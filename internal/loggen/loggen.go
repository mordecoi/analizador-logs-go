// Package loggen genera archivos de log sintéticos en formato Apache Combined
// con señal analítica realista y anomalías inyectadas.
//
// A diferencia de un generador ingenuo (choice uniforme sobre listas chicas), acá:
//
//   - Los timestamps salen de un proceso de Poisson no homogéneo: el reloj avanza
//     con intervalos exponenciales cuya tasa se modula con una curva horaria y un
//     factor de fin de semana. La salida queda ordenada cronológicamente, como un
//     log real, y "horas con más tráfico" tiene una respuesta no trivial.
//   - Endpoints, IPs y usuarios se muestrean con pesos sesgados (tipo Zipf), así el
//     Top-N tiene una cabeza y una cola de verdad.
//   - El status, los bytes y la latencia dependen de la categoría del endpoint.
//   - Se inyectan tres anomalías (ráfaga, pico de 5xx, escaneo) cuyo detalle exacto
//     se devuelve en Verificacion para poder validar los detectores después.
package loggen

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"time"
)

// layoutApache es el formato de fecha del log (idéntico al de Apache).
const layoutApache = "02/Jan/2006:15:04:05 -0700"

// Opciones controla la generación. Los ceros se completan con valores por defecto.
type Opciones struct {
	Lineas        int            // cantidad de líneas a emitir (obligatorio, > 0)
	Semilla       uint64         // 0 → semilla fija reproducible
	Desde         time.Time      // instante de la primera solicitud
	TasaPorSeg    float64        // solicitudes por segundo promedio (default 1.65)
	FraccionHeavy float64        // fracción de solicitudes desde el pool de IPs activas (default 0.6)
	CantHeavyIPs  int            // tamaño del pool de IPs activas (default 200)
	Zona          *time.Location // zona horaria del log (default -0300)
}

// Par es una clave con su conteo, para los rankings de la verificación.
type Par struct {
	Clave  string `json:"clave"`
	Conteo int    `json:"conteo"`
}

// Anomalia describe un patrón anómalo inyectado, con su ventana temporal exacta.
type Anomalia struct {
	Tipo     string    `json:"tipo"` // "rafaga" | "pico_5xx" | "escaneo"
	IP       string    `json:"ip,omitempty"`
	Endpoint string    `json:"endpoint,omitempty"`
	Desde    time.Time `json:"desde"`
	Hasta    time.Time `json:"hasta"`
	Cantidad int       `json:"cantidad"`
	Detalle  string    `json:"detalle"`
}

// Verificacion es el "ground truth": las anomalías inyectadas y los agregados
// exactos calculados mientras se generaba. Sirve para validar que el analizador
// devuelve lo correcto.
type Verificacion struct {
	Semilla            uint64     `json:"semilla"`
	Lineas             int        `json:"lineas"`
	Desde              time.Time  `json:"desde"`
	Hasta              time.Time  `json:"hasta"`
	Anomalias          []Anomalia `json:"anomalias"`
	TopEndpoints       []Par      `json:"top_endpoints"`
	TopEstados         []Par      `json:"top_estados"`
	TopIPsActivas      []Par      `json:"top_ips_activas"`
	SolicitudesPorHora [24]int    `json:"solicitudes_por_hora"`
	HoraPico           int        `json:"hora_pico"`
}

// perfilHorario es el multiplicador de tasa para cada hora del día (0..23).
// Modela un servidor con pico de media mañana y de media tarde.
var perfilHorario = [24]float64{
	0.30, 0.20, 0.15, 0.12, 0.12, 0.18,
	0.35, 0.60, 0.90, 1.30, 1.60, 1.70,
	1.50, 1.45, 1.70, 1.70, 1.60, 1.40,
	1.20, 1.00, 0.85, 0.75, 0.55, 0.40,
}

// defEndpoint asocia una ruta con su peso relativo y su categoría (que determina
// las distribuciones de status, bytes y latencia).
type defEndpoint struct {
	ruta      string
	peso      float64
	categoria string
}

var endpoints = []defEndpoint{
	{"/", 120, "pagina"},
	{"/usr/login", 55, "pagina"},
	{"/usr/register", 20, "pagina"},
	{"/usr", 30, "pagina"},
	{"/usr/admin", 8, "admin"},
	{"/usr/admin/developer", 3, "admin"},
	{"/logout", 12, "pagina"},
	{"/about", 8, "pagina"},
	{"/contact", 5, "pagina"},
	{"/api/products", 70, "api"},
	{"/api/products/detalle", 45, "api"},
	{"/api/cart", 38, "api"},
	{"/api/checkout", 12, "api"},
	{"/api/orders", 18, "api"},
	{"/api/search", 40, "api"},
	{"/api/user/profile", 22, "api"},
	{"/api/recommendations", 15, "api"},
	{"/api/reviews", 10, "api"},
	{"/static/css/main.css", 60, "estatico"},
	{"/static/js/app.js", 58, "estatico"},
	{"/static/js/vendor.js", 40, "estatico"},
	{"/images/logo.png", 35, "estatico"},
	{"/images/banner.jpg", 25, "estatico"},
	{"/favicon.ico", 48, "estatico"},
	{"/robots.txt", 6, "estatico"},
	{"/health", 70, "salud"},
	{"/metrics", 30, "salud"},
}

// pesoStr es un valor de string con su peso, para los muestreadores.
type pesoStr struct {
	valor string
	peso  float64
}

// estadosPorCategoria: distribución de status según el tipo de endpoint.
var estadosPorCategoria = map[string][]pesoStr{
	"estatico": {{"200", 78}, {"304", 18}, {"404", 4}},
	"pagina":   {{"200", 88}, {"301", 3}, {"302", 3}, {"404", 4}, {"500", 2}},
	"api":      {{"200", 86}, {"400", 3}, {"401", 2}, {"403", 1}, {"404", 3}, {"500", 3}, {"502", 2}},
	"admin":    {{"200", 55}, {"403", 38}, {"500", 7}},
	"salud":    {{"200", 992}, {"503", 8}},
}

var uaNavegadores = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:84.0) Gecko/20100101 Firefox/84.0",
	"Mozilla/5.0 (Android 10; Mobile; rv:84.0) Gecko/84.0 Firefox/84.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.141 Safari/537.36",
	"Mozilla/5.0 (Linux; Android 10; ONEPLUS A6000) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.141 Mobile Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/89.0.4380.0 Safari/537.36 Edg/89.0.759.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.88 Safari/537.36 OPR/73.0.3856.329",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_9_3) AppleWebKit/537.75.14 (KHTML, like Gecko) Version/7.0.3 Safari/7046A194A",
	"Mozilla/5.0 (iPhone; CPU iPhone OS 12_4_9 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/12.1.2 Mobile/15E148 Safari/604.1",
}

var uaBots = []string{
	"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
	"Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
	"curl/7.81.0",
	"python-requests/2.31.0",
	"Go-http-client/2.0",
}

// referrers con "-" repetido para que la mayoría de las solicitudes no tengan origen.
var referrers = []string{
	"-", "-", "-", "-", "-",
	"https://www.google.com/", "https://www.bing.com/",
	"https://duckduckgo.com/", "https://t.co/", "https://news.ycombinator.com/",
}

var nombres = []string{
	"james", "adam", "eve", "alex", "smith", "isabella", "david", "angela", "donald", "hilary",
	"laura", "mateo", "sofia", "bruno", "carla", "diego", "elena", "fabian", "gimena", "hernan",
	"ivana", "julian", "karen", "lucas", "marina", "nacho", "olivia", "pablo", "rocio", "tomas",
}

// rutasSondeo son rutas típicas de un escáner de vulnerabilidades (siempre 404).
var rutasSondeo = []string{
	"/wp-login.php", "/xmlrpc.php", "/.env", "/.git/config", "/config.php",
	"/admin.php", "/phpmyadmin/", "/backup.sql", "/.aws/credentials", "/server-status",
	"/vendor/phpunit/phpunit/phpunit.php", "/shell.php", "/wp-admin/", "/api/v1/keys",
}

// muestreador elige un string con probabilidad proporcional a un peso, en O(log n)
// por sorteo mediante búsqueda sobre la suma acumulada normalizada.
type muestreador struct {
	valores []string
	acum    []float64
}

func nuevoMuestreador(valores []string, pesos []float64) *muestreador {
	if len(valores) != len(pesos) || len(valores) == 0 {
		panic("loggen: muestreador mal construido")
	}
	acum := make([]float64, len(pesos))
	var s float64
	for i, p := range pesos {
		s += p
		acum[i] = s
	}
	for i := range acum {
		acum[i] /= s
	}
	return &muestreador{valores: slices.Clone(valores), acum: acum}
}

// nuevoMuestreadorPares es el mismo constructor a partir de una lista de pesoStr.
func nuevoMuestreadorPares(pares []pesoStr) *muestreador {
	valores := make([]string, len(pares))
	pesos := make([]float64, len(pares))
	for i, p := range pares {
		valores[i], pesos[i] = p.valor, p.peso
	}
	return nuevoMuestreador(valores, pesos)
}

func (m *muestreador) elegir(r *rand.Rand) string {
	u := r.Float64()
	i := sort.Search(len(m.acum), func(i int) bool { return m.acum[i] >= u })
	if i >= len(m.valores) {
		i = len(m.valores) - 1
	}
	return m.valores[i]
}

// evento es una solicitud ya materializada, antes de formatearla.
type evento struct {
	t        time.Time
	ip       string
	usuario  string
	metodo   string
	ruta     string
	proto    string
	estado   string
	bytes    int
	referrer string
	ua       string
	respMs   int
}

func (e *evento) escribir(bw *bufio.Writer) {
	// Fprintf por línea no es lo más rápido, pero esto es un generador, no el
	// código que vamos a perfilar: prima la legibilidad.
	fmt.Fprintf(bw, "%s - %s [%s] \"%s %s %s\" %s %d %q %q %d\n",
		e.ip, e.usuario, e.t.Format(layoutApache),
		e.metodo, e.ruta, e.proto,
		e.estado, e.bytes, e.referrer, e.ua, e.respMs)
}

func factorTemporal(t time.Time) float64 {
	f := perfilHorario[t.Hour()]
	if wd := t.Weekday(); wd == time.Saturday || wd == time.Sunday {
		f *= 0.6
	}
	return f
}

func ipAleatoria(r *rand.Rand) string {
	return fmt.Sprintf("%d.%d.%d.%d", 1+r.IntN(223), r.IntN(256), r.IntN(256), 1+r.IntN(254))
}

func metodoDe(r *rand.Rand, categoria string) string {
	if categoria == "estatico" || categoria == "salud" {
		return "GET"
	}
	switch u := r.Float64(); {
	case u < 0.72:
		return "GET"
	case u < 0.90:
		return "POST"
	case u < 0.96:
		return "PUT"
	default:
		return "DELETE"
	}
}

func protoDe(r *rand.Rand) string {
	if r.Float64() < 0.08 {
		return "HTTP/1.0"
	}
	return "HTTP/1.1"
}

func bytesDe(r *rand.Rand, categoria, estado string) int {
	switch {
	case estado == "304":
		return 0
	case estado[0] == '4' || estado[0] == '5':
		return 120 + r.IntN(500)
	}
	var base, sigma float64
	switch categoria {
	case "estatico":
		base, sigma = 60000, 0.5
	case "pagina":
		base, sigma = 12000, 0.5
	case "api":
		base, sigma = 1500, 0.6
	case "admin":
		base, sigma = 1800, 0.4
	default: // salud
		base, sigma = 15, 0.3
	}
	b := int(base * math.Exp(r.NormFloat64()*sigma))
	if b < 1 {
		b = 1
	}
	return b
}

func latenciaMs(r *rand.Rand, categoria, ruta, estado string) int {
	if estado[0] == '5' {
		if r.Float64() < 0.5 {
			return 1 + r.IntN(4) // falla rápido
		}
		return 1000 + r.IntN(4000) // timeout
	}
	var lo, hi int
	switch categoria {
	case "salud":
		lo, hi = 1, 6
	case "estatico":
		lo, hi = 2, 25
	case "pagina":
		lo, hi = 20, 140
	case "api":
		lo, hi = 25, 160
	case "admin":
		lo, hi = 12, 70
	default:
		lo, hi = 10, 80
	}
	ms := lo + r.IntN(hi-lo)
	switch ruta {
	case "/api/search", "/api/checkout", "/api/recommendations":
		ms += r.IntN(300)
		if r.Float64() < 0.1 {
			ms += r.IntN(1500) // cola pesada de los endpoints lentos
		}
	}
	return ms
}

func uaDe(r *rand.Rand, esHeavy bool) string {
	p := 0.04
	if esHeavy {
		p = 0.55
	}
	if r.Float64() < p {
		return uaBots[r.IntN(len(uaBots))]
	}
	return uaNavegadores[r.IntN(len(uaNavegadores))]
}

// Generar escribe opts.Lineas líneas de log en w y devuelve la verificación.
// No cierra w; sí vacía su propio buffer antes de volver.
func Generar(w io.Writer, opts Opciones) (Verificacion, error) {
	if opts.Lineas <= 0 {
		return Verificacion{}, fmt.Errorf("loggen: Lineas debe ser > 0, es %d", opts.Lineas)
	}
	if opts.TasaPorSeg <= 0 {
		opts.TasaPorSeg = 1.65
	}
	if opts.FraccionHeavy <= 0 {
		opts.FraccionHeavy = 0.6
	}
	if opts.CantHeavyIPs <= 0 {
		opts.CantHeavyIPs = 200
	}
	if opts.Zona == nil {
		opts.Zona = time.FixedZone("-0300", -3*3600)
	}
	if opts.Desde.IsZero() {
		opts.Desde = time.Date(2024, 1, 1, 0, 0, 0, 0, opts.Zona)
	} else {
		opts.Desde = opts.Desde.In(opts.Zona)
	}

	semUsada := opts.Semilla
	var r *rand.Rand
	if opts.Semilla == 0 {
		r = rand.New(rand.NewPCG(1, 2))
	} else {
		r = rand.New(rand.NewPCG(opts.Semilla, opts.Semilla^0x9e3779b97f4a7c15))
	}

	// --- Muestreadores -------------------------------------------------------
	rutas := make([]string, len(endpoints))
	pesosEp := make([]float64, len(endpoints))
	rutaACategoria := make(map[string]string, len(endpoints))
	for i, d := range endpoints {
		rutas[i], pesosEp[i] = d.ruta, d.peso
		rutaACategoria[d.ruta] = d.categoria
	}
	endpointMues := nuevoMuestreador(rutas, pesosEp)

	estadoMues := make(map[string]*muestreador, len(estadosPorCategoria))
	for cat, pares := range estadosPorCategoria {
		estadoMues[cat] = nuevoMuestreadorPares(pares)
	}

	heavyIPs := make([]string, opts.CantHeavyIPs)
	pesosHeavy := make([]float64, opts.CantHeavyIPs)
	for i := range heavyIPs {
		heavyIPs[i] = ipAleatoria(r)
		pesosHeavy[i] = 1.0 / math.Pow(float64(i+1), 0.9) // Zipf
	}
	heavyMues := nuevoMuestreador(heavyIPs, pesosHeavy)
	esHeavyIP := make(map[string]bool, opts.CantHeavyIPs)
	for _, ip := range heavyIPs {
		esHeavyIP[ip] = true
	}

	pesosNombre := make([]float64, len(nombres))
	for i := range pesosNombre {
		pesosNombre[i] = 1.0 / math.Pow(float64(i+1), 0.7)
	}
	usuarioMues := nuevoMuestreador(nombres, pesosNombre)

	// tasaBase se calibra para que la tasa media emitida ≈ opts.TasaPorSeg
	// una vez aplicada la modulación horaria.
	var sumaPerfil float64
	for _, v := range perfilHorario {
		sumaPerfil += v
	}
	tasaBase := opts.TasaPorSeg / (sumaPerfil / 24)

	spanSeg := float64(opts.Lineas) / opts.TasaPorSeg

	// --- Anomalías (se construyen antes de la corriente base) ---------------
	rafagaBuf, rafagaAnom := construirRafaga(r, opts, spanSeg)
	picoBuf, picoAnom := construirPico5xx(r, opts, spanSeg, heavyMues, usuarioMues)
	escaneoBuf, escaneoAnom := construirEscaneo(r, opts, spanSeg, rutaACategoria)

	type fuenteAnom struct {
		buf []evento
		i   int
	}
	anoms := []*fuenteAnom{{buf: rafagaBuf}, {buf: picoBuf}, {buf: escaneoBuf}}

	// --- Corriente base ----------------------------------------------------
	reloj := opts.Desde
	generarBase := func() evento {
		lambda := tasaBase * factorTemporal(reloj)
		reloj = reloj.Add(time.Duration(r.ExpFloat64() / lambda * float64(time.Second)))

		ruta := endpointMues.elegir(r)
		cat := rutaACategoria[ruta]
		estado := estadoMues[cat].elegir(r)

		var ip string
		esHeavy := false
		if r.Float64() < opts.FraccionHeavy {
			ip, esHeavy = heavyMues.elegir(r), true
		} else {
			ip = ipAleatoria(r)
		}

		usuario := "-"
		if (cat == "api" || cat == "admin") && r.Float64() < 0.6 {
			usuario = usuarioMues.elegir(r)
		}

		return evento{
			t: reloj, ip: ip, usuario: usuario,
			metodo: metodoDe(r, cat), ruta: ruta, proto: protoDe(r),
			estado: estado, bytes: bytesDe(r, cat, estado),
			referrer: referrers[r.IntN(len(referrers))], ua: uaDe(r, esHeavy),
			respMs: latenciaMs(r, cat, ruta, estado),
		}
	}

	// --- Merge cronológico y conteo --------------------------------------
	bw := bufio.NewWriterSize(w, 1<<20)

	contEndpoint := make(map[string]int, len(endpoints)+len(rutasSondeo))
	contEstado := make(map[string]int, 16)
	contHeavy := make(map[string]int, opts.CantHeavyIPs)
	var porHora [24]int
	var ultima time.Time

	escribir := func(e evento) {
		e.escribir(bw)
		contEndpoint[e.ruta]++
		contEstado[e.estado]++
		if esHeavyIP[e.ip] {
			contHeavy[e.ip]++
		}
		porHora[e.t.Hour()]++
		ultima = e.t
	}

	baseSig := generarBase()
	for escritas := 0; escritas < opts.Lineas; escritas++ {
		mejorT := baseSig.t
		mejorIdx := -1 // -1 = corriente base
		for k, a := range anoms {
			if a.i < len(a.buf) && a.buf[a.i].t.Before(mejorT) {
				mejorT, mejorIdx = a.buf[a.i].t, k
			}
		}
		if mejorIdx == -1 {
			escribir(baseSig)
			baseSig = generarBase()
		} else {
			escribir(anoms[mejorIdx].buf[anoms[mejorIdx].i])
			anoms[mejorIdx].i++
		}
	}

	if err := bw.Flush(); err != nil {
		return Verificacion{}, fmt.Errorf("loggen: al escribir: %w", err)
	}

	horaPico := 0
	for h := 1; h < 24; h++ {
		if porHora[h] > porHora[horaPico] {
			horaPico = h
		}
	}

	return Verificacion{
		Semilla:            semUsada,
		Lineas:             opts.Lineas,
		Desde:              opts.Desde,
		Hasta:              ultima,
		Anomalias:          []Anomalia{rafagaAnom, picoAnom, escaneoAnom},
		TopEndpoints:       topN(contEndpoint, 10),
		TopEstados:         topN(contEstado, 10),
		TopIPsActivas:      topN(contHeavy, 10),
		SolicitudesPorHora: porHora,
		HoraPico:           horaPico,
	}, nil
}

func topN(m map[string]int, n int) []Par {
	pares := make([]Par, 0, len(m))
	for k, v := range m {
		pares = append(pares, Par{Clave: k, Conteo: v})
	}
	slices.SortFunc(pares, func(a, b Par) int {
		if a.Conteo != b.Conteo {
			return b.Conteo - a.Conteo // descendente por conteo
		}
		return strings.Compare(a.Clave, b.Clave) // desempate estable
	})
	if len(pares) > n {
		pares = pares[:n]
	}
	return pares
}

func ordenarPorTiempo(buf []evento) {
	slices.SortFunc(buf, func(a, b evento) int { return a.t.Compare(b.t) })
}

// ventana devuelve el intervalo [primero, último] de un buffer ya ordenado,
// alineado a segundos enteros (inicio hacia abajo, fin hacia arriba). El log
// solo tiene precisión de segundos, así que la ventana declarada tiene que
// contener a todos los eventos una vez truncados al segundo.
func ventana(buf []evento) (desde, hasta time.Time) {
	desde = buf[0].t.Truncate(time.Second)
	hasta = buf[len(buf)-1].t.Truncate(time.Second).Add(time.Second)
	return
}

// construirRafaga: una IP nueva (sin historial) hace muchos POST a /usr/login en
// pocos segundos — el patrón de un ataque de fuerza bruta / credential stuffing.
func construirRafaga(r *rand.Rand, opts Opciones, spanSeg float64) ([]evento, Anomalia) {
	ip := ipAleatoria(r)
	ini := opts.Desde.Add(time.Duration(0.18 * spanSeg * float64(time.Second)))
	dur := 30 * time.Second
	const cant = 500

	buf := make([]evento, cant)
	for i := range buf {
		estado := "200"
		if r.Float64() < 0.25 {
			estado = "403"
		}
		buf[i] = evento{
			t:  ini.Add(time.Duration(r.Float64() * float64(dur))),
			ip: ip, usuario: "-", metodo: "POST", ruta: "/usr/login", proto: "HTTP/1.1",
			estado: estado, bytes: bytesDe(r, "pagina", estado),
			referrer: "-", ua: "python-requests/2.31.0", respMs: 30 + r.IntN(90),
		}
	}
	ordenarPorTiempo(buf)
	desde, hasta := ventana(buf)
	return buf, Anomalia{
		Tipo: "rafaga", IP: ip, Desde: desde, Hasta: hasta, Cantidad: cant,
		Detalle: fmt.Sprintf("%d solicitudes POST a /usr/login en ~%s desde una sola IP", cant, dur),
	}
}

// construirPico5xx: hora con una tasa anormal de errores 5xx en /api/checkout.
func construirPico5xx(r *rand.Rand, opts Opciones, spanSeg float64, heavyMues, usuarioMues *muestreador) ([]evento, Anomalia) {
	const endpoint = "/api/checkout"
	ini := opts.Desde.Add(time.Duration(0.50 * spanSeg * float64(time.Second)))
	dur := time.Hour
	const cant = 600
	estados := nuevoMuestreador([]string{"500", "502", "503"}, []float64{6, 3, 2})

	buf := make([]evento, cant)
	for i := range buf {
		var ip string
		esHeavy := false
		if r.Float64() < opts.FraccionHeavy {
			ip, esHeavy = heavyMues.elegir(r), true
		} else {
			ip = ipAleatoria(r)
		}
		usuario := "-"
		if r.Float64() < 0.6 {
			usuario = usuarioMues.elegir(r)
		}
		est := estados.elegir(r)
		buf[i] = evento{
			t:  ini.Add(time.Duration(r.Float64() * float64(dur))),
			ip: ip, usuario: usuario, metodo: "POST", ruta: endpoint, proto: "HTTP/1.1",
			estado: est, bytes: bytesDe(r, "api", est),
			referrer: "-", ua: uaDe(r, esHeavy), respMs: 1000 + r.IntN(9000),
		}
	}
	ordenarPorTiempo(buf)
	desde, hasta := ventana(buf)
	return buf, Anomalia{
		Tipo: "pico_5xx", Endpoint: endpoint, Desde: desde, Hasta: hasta, Cantidad: cant,
		Detalle: fmt.Sprintf("~%d respuestas 5xx en %s concentradas en 1 h", cant, endpoint),
	}
}

// construirEscaneo: una IP nueva recorre todas las rutas conocidas y rutas de
// sondeo típicas de un escáner, en pocos minutos, casi todo 404.
func construirEscaneo(r *rand.Rand, opts Opciones, spanSeg float64, rutaACategoria map[string]string) ([]evento, Anomalia) {
	ip := ipAleatoria(r)
	ini := opts.Desde.Add(time.Duration(0.75 * spanSeg * float64(time.Second)))
	dur := 120 * time.Second
	const rondas = 3

	objetivos := make([]string, 0, len(endpoints)+len(rutasSondeo))
	for _, d := range endpoints {
		objetivos = append(objetivos, d.ruta)
	}
	objetivos = append(objetivos, rutasSondeo...)

	var buf []evento
	for ronda := 0; ronda < rondas; ronda++ {
		for _, ruta := range objetivos {
			estado := "404"
			if _, conocida := rutaACategoria[ruta]; conocida && r.Float64() < 0.5 {
				estado = "200"
			}
			buf = append(buf, evento{
				t:  ini.Add(time.Duration(r.Float64() * float64(dur))),
				ip: ip, usuario: "-", metodo: "GET", ruta: ruta, proto: "HTTP/1.1",
				estado: estado, bytes: bytesDe(r, "pagina", estado),
				referrer: "-", ua: "curl/7.81.0", respMs: 5 + r.IntN(40),
			})
		}
	}
	ordenarPorTiempo(buf)
	desde, hasta := ventana(buf)
	return buf, Anomalia{
		Tipo: "escaneo", IP: ip, Desde: desde, Hasta: hasta, Cantidad: len(buf),
		Detalle: fmt.Sprintf("una IP toca %d rutas distintas (incluye rutas de sondeo) en ~%s", len(objetivos), dur),
	}
}
