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
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"sort"
	"strconv"
	"time"

	"parcial1/internal/agregado"
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
	TamBuffer     int            // bytes del buffer de escritura (default 1 MiB; SinBuffer = cada línea directo)
}

// SinBuffer como Opciones.TamBuffer escribe cada línea con una llamada a Write
// del destino. Existe para medir cuánto aporta el buffer de escritura.
const SinBuffer = -1

// tamBufferPorDefecto: con 1 MiB una línea de ~180 B se agrupa con otras ~5800
// antes de llegar al sistema operativo (ver BenchmarkEscrituraBuffer).
const tamBufferPorDefecto = 1 << 20

// Par es una clave con su conteo, para los rankings de la verificación. Es el
// mismo tipo que usa el analizador, así los rankings se comparan directamente.
type Par = agregado.Par[string]

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
	TopIPsActivas      []Par      `json:"top_ips_activas"` // entre las IPs seguidas: pool activo + anomalías
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

// Categorías de endpoint: determinan las distribuciones de status, bytes y latencia.
const (
	catPagina   = "pagina"
	catAdmin    = "admin"
	catAPI      = "api"
	catEstatico = "estatico"
	catSalud    = "salud"
)

// Métodos y protocolos que aparecen en más de un lugar del generador.
const (
	metodoGET  = "GET"
	metodoPOST = "POST"
	http10     = "HTTP/1.0"
	http11     = "HTTP/1.1"
)

// defEndpoint asocia una ruta con su peso relativo y su categoría (que determina
// las distribuciones de status, bytes y latencia).
type defEndpoint struct {
	ruta      string
	peso      float64
	categoria string
}

var endpoints = []defEndpoint{
	{"/", 120, catPagina},
	{"/usr/login", 55, catPagina},
	{"/usr/register", 20, catPagina},
	{"/usr", 30, catPagina},
	{"/usr/admin", 8, catAdmin},
	{"/usr/admin/developer", 3, catAdmin},
	{"/logout", 12, catPagina},
	{"/about", 8, catPagina},
	{"/contact", 5, catPagina},
	{"/api/products", 70, catAPI},
	{"/api/products/detalle", 45, catAPI},
	{"/api/cart", 38, catAPI},
	{"/api/checkout", 12, catAPI},
	{"/api/orders", 18, catAPI},
	{"/api/search", 40, catAPI},
	{"/api/user/profile", 22, catAPI},
	{"/api/recommendations", 15, catAPI},
	{"/api/reviews", 10, catAPI},
	{"/static/css/main.css", 60, catEstatico},
	{"/static/js/app.js", 58, catEstatico},
	{"/static/js/vendor.js", 40, catEstatico},
	{"/images/logo.png", 35, catEstatico},
	{"/images/banner.jpg", 25, catEstatico},
	{"/favicon.ico", 48, catEstatico},
	{"/robots.txt", 6, catEstatico},
	{"/health", 70, catSalud},
	{"/metrics", 30, catSalud},
}

// pesoStr es un valor de string con su peso, para los muestreadores.
type pesoStr struct {
	valor string
	peso  float64
}

// estadosPorCategoria: distribución de status según el tipo de endpoint.
var estadosPorCategoria = map[string][]pesoStr{
	catEstatico: {{"200", 78}, {"304", 18}, {"404", 4}},
	catPagina:   {{"200", 88}, {"301", 3}, {"302", 3}, {"404", 4}, {"500", 2}},
	catAPI:      {{"200", 86}, {"400", 3}, {"401", 2}, {"403", 1}, {"404", 3}, {"500", 3}, {"502", 2}},
	catAdmin:    {{"200", 55}, {"403", 38}, {"500", 7}},
	catSalud:    {{"200", 992}, {"503", 8}},
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

// agregarLinea agrega la línea formateada al final de b y lo devuelve, como las
// funciones Append* de strconv. Reemplaza a un fmt.Fprintf por línea: produce
// exactamente los mismos bytes (%q equivale a strconv.Quote) pero sin el costo
// de interpretar el formato ni empaquetar cada argumento en una interfaz, y
// reutilizando el mismo buffer para todas las líneas.
func (e *evento) agregarLinea(b []byte) []byte {
	b = append(b, e.ip...)
	b = append(b, " - "...)
	b = append(b, e.usuario...)
	b = append(b, " ["...)
	b = e.t.AppendFormat(b, layoutApache)
	b = append(b, "] \""...)
	b = append(b, e.metodo...)
	b = append(b, ' ')
	b = append(b, e.ruta...)
	b = append(b, ' ')
	b = append(b, e.proto...)
	b = append(b, "\" "...)
	b = append(b, e.estado...)
	b = append(b, ' ')
	b = strconv.AppendInt(b, int64(e.bytes), 10)
	b = append(b, ' ')
	b = strconv.AppendQuote(b, e.referrer)
	b = append(b, ' ')
	b = strconv.AppendQuote(b, e.ua)
	b = append(b, ' ')
	b = strconv.AppendInt(b, int64(e.respMs), 10)
	return append(b, '\n')
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
	if categoria == catEstatico || categoria == catSalud {
		return metodoGET
	}
	switch u := r.Float64(); {
	case u < 0.72:
		return metodoGET
	case u < 0.90:
		return metodoPOST
	case u < 0.96:
		return "PUT"
	default:
		return "DELETE"
	}
}

func protoDe(r *rand.Rand) string {
	if r.Float64() < 0.08 {
		return http10
	}
	return http11
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
	case catEstatico:
		base, sigma = 60000, 0.5
	case catPagina:
		base, sigma = 12000, 0.5
	case catAPI:
		base, sigma = 1500, 0.6
	case catAdmin:
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
	case catSalud:
		lo, hi = 1, 6
	case catEstatico:
		lo, hi = 2, 25
	case catPagina:
		lo, hi = 20, 140
	case catAPI:
		lo, hi = 25, 160
	case catAdmin:
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
//
// El log se arma como un merge cronológico de cuatro corrientes ya ordenadas:
// la corriente base (Poisson no homogéneo) y las tres anomalías, que se
// construyen enteras antes de empezar porque son chicas.
func Generar(w io.Writer, opts Opciones) (Verificacion, error) {
	if opts.Lineas <= 0 {
		return Verificacion{}, fmt.Errorf("loggen: Lineas debe ser > 0, es %d", opts.Lineas)
	}
	opts = completarOpciones(opts)
	r := nuevoRand(opts.Semilla)
	m := nuevosMuestreadores(r, opts)
	spanSeg := float64(opts.Lineas) / opts.TasaPorSeg

	rafagaBuf, rafagaAnom := construirRafaga(r, opts, spanSeg)
	picoBuf, picoAnom := construirPico5xx(r, opts, spanSeg, m.heavy, m.usuario)
	escaneoBuf, escaneoAnom := construirEscaneo(r, opts, spanSeg, m.rutaACategoria)
	anoms := [][]evento{rafagaBuf, picoBuf, escaneoBuf}

	base := nuevaCorrienteBase(r, opts, m)
	esc := nuevoEscritor(w, opts.TamBuffer)
	// Se siguen las IPs que pueden llegar al top: el pool activo y las de las
	// anomalías (una ráfaga de 500 solicitudes entra al top-10 en logs chicos).
	// Las IPs aleatorias aparecen ~1 vez cada una y no hace falta contarlas.
	seguidas := maps.Clone(m.esHeavyIP)
	seguidas[rafagaAnom.IP] = true
	seguidas[escaneoAnom.IP] = true
	acu := nuevoAcumulador(seguidas)

	sig := base.siguiente()
	for escritas := 0; escritas < opts.Lineas; escritas++ {
		e := &sig
		elegida := -1 // -1 = corriente base
		for k, a := range anoms {
			if len(a) > 0 && a[0].t.Before(e.t) {
				e, elegida = &anoms[k][0], k
			}
		}
		if err := esc.escribir(e); err != nil {
			return Verificacion{}, err
		}
		acu.registrar(e)
		if elegida == -1 {
			sig = base.siguiente()
		} else {
			anoms[elegida] = anoms[elegida][1:]
		}
	}
	if err := esc.vaciar(); err != nil {
		return Verificacion{}, err
	}

	ver := acu.verificacion()
	ver.Semilla = opts.Semilla
	ver.Lineas = opts.Lineas
	ver.Desde = opts.Desde
	ver.Anomalias = []Anomalia{rafagaAnom, picoAnom, escaneoAnom}
	return ver, nil
}

// completarOpciones reemplaza los ceros de opts por los valores por defecto.
func completarOpciones(opts Opciones) Opciones {
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
	if opts.TamBuffer == 0 {
		opts.TamBuffer = tamBufferPorDefecto
	}
	return opts
}

// nuevoRand devuelve la fuente de números aleatorios; la semilla 0 es una
// semilla fija, para que la corrida por defecto sea reproducible.
func nuevoRand(semilla uint64) *rand.Rand {
	if semilla == 0 {
		return rand.New(rand.NewPCG(1, 2))
	}
	return rand.New(rand.NewPCG(semilla, semilla^0x9e3779b97f4a7c15))
}

// muestreadores agrupa las distribuciones de las que sale cada campo.
type muestreadores struct {
	endpoint       *muestreador
	estado         map[string]*muestreador // categoría → distribución de status
	heavy          *muestreador            // pool de IPs de alta actividad (Zipf)
	usuario        *muestreador
	esHeavyIP      map[string]bool
	rutaACategoria map[string]string
}

func nuevosMuestreadores(r *rand.Rand, opts Opciones) muestreadores {
	m := muestreadores{
		estado:         make(map[string]*muestreador, len(estadosPorCategoria)),
		esHeavyIP:      make(map[string]bool, opts.CantHeavyIPs),
		rutaACategoria: make(map[string]string, len(endpoints)),
	}

	rutas := make([]string, len(endpoints))
	pesosEp := make([]float64, len(endpoints))
	for i, d := range endpoints {
		rutas[i], pesosEp[i] = d.ruta, d.peso
		m.rutaACategoria[d.ruta] = d.categoria
	}
	m.endpoint = nuevoMuestreador(rutas, pesosEp)

	for cat, pares := range estadosPorCategoria {
		m.estado[cat] = nuevoMuestreadorPares(pares)
	}

	heavyIPs := make([]string, opts.CantHeavyIPs)
	pesosHeavy := make([]float64, opts.CantHeavyIPs)
	for i := range heavyIPs {
		heavyIPs[i] = ipAleatoria(r)
		pesosHeavy[i] = 1.0 / math.Pow(float64(i+1), 0.9) // Zipf
		m.esHeavyIP[heavyIPs[i]] = true
	}
	m.heavy = nuevoMuestreador(heavyIPs, pesosHeavy)

	pesosNombre := make([]float64, len(nombres))
	for i := range pesosNombre {
		pesosNombre[i] = 1.0 / math.Pow(float64(i+1), 0.7)
	}
	m.usuario = nuevoMuestreador(nombres, pesosNombre)
	return m
}

// corrienteBase produce el tráfico normal: cada llamada a siguiente avanza el
// reloj un intervalo exponencial cuya tasa sigue la curva horaria.
type corrienteBase struct {
	r        *rand.Rand
	opts     Opciones
	m        muestreadores
	reloj    time.Time
	tasaBase float64
}

func nuevaCorrienteBase(r *rand.Rand, opts Opciones, m muestreadores) *corrienteBase {
	// tasaBase se calibra para que la tasa media emitida ≈ opts.TasaPorSeg
	// una vez aplicada la modulación horaria.
	var sumaPerfil float64
	for _, v := range perfilHorario {
		sumaPerfil += v
	}
	return &corrienteBase{
		r: r, opts: opts, m: m,
		reloj:    opts.Desde,
		tasaBase: opts.TasaPorSeg / (sumaPerfil / 24),
	}
}

func (c *corrienteBase) siguiente() evento {
	r := c.r
	lambda := c.tasaBase * factorTemporal(c.reloj)
	c.reloj = c.reloj.Add(time.Duration(r.ExpFloat64() / lambda * float64(time.Second)))

	ruta := c.m.endpoint.elegir(r)
	cat := c.m.rutaACategoria[ruta]
	estado := c.m.estado[cat].elegir(r)

	var ip string
	esHeavy := false
	if r.Float64() < c.opts.FraccionHeavy {
		ip, esHeavy = c.m.heavy.elegir(r), true
	} else {
		ip = ipAleatoria(r)
	}

	usuario := "-"
	if (cat == catAPI || cat == catAdmin) && r.Float64() < 0.6 {
		usuario = c.m.usuario.elegir(r)
	}

	return evento{
		t: c.reloj, ip: ip, usuario: usuario,
		metodo: metodoDe(r, cat), ruta: ruta, proto: protoDe(r),
		estado: estado, bytes: bytesDe(r, cat, estado),
		referrer: referrers[r.IntN(len(referrers))], ua: uaDe(r, esHeavy),
		respMs: latenciaMs(r, cat, ruta, estado),
	}
}

// escritor formatea cada evento en un buffer de línea reutilizado y lo manda
// al destino, a través de un bufio.Writer salvo con SinBuffer.
type escritor struct {
	destino io.Writer
	bw      *bufio.Writer // nil con SinBuffer
	linea   []byte
}

func nuevoEscritor(w io.Writer, tamBuffer int) *escritor {
	e := &escritor{destino: w, linea: make([]byte, 0, 512)}
	if tamBuffer != SinBuffer {
		e.bw = bufio.NewWriterSize(w, tamBuffer)
		e.destino = e.bw
	}
	return e
}

func (e *escritor) escribir(ev *evento) error {
	e.linea = ev.agregarLinea(e.linea[:0])
	if _, err := e.destino.Write(e.linea); err != nil {
		return fmt.Errorf("loggen: al escribir: %w", err)
	}
	return nil
}

func (e *escritor) vaciar() error {
	if e.bw == nil {
		return nil
	}
	if err := e.bw.Flush(); err != nil {
		return fmt.Errorf("loggen: al escribir: %w", err)
	}
	return nil
}

// acumulador cuenta, mientras se escribe, los agregados exactos del ground truth.
type acumulador struct {
	endpoints map[string]int
	estados   map[string]int
	ips       map[string]int // solo las IPs seguidas: las únicas que llegan al top
	seguidas  map[string]bool
	porHora   [24]int
	ultima    time.Time
}

func nuevoAcumulador(seguidas map[string]bool) *acumulador {
	return &acumulador{
		endpoints: make(map[string]int, len(endpoints)+len(rutasSondeo)),
		estados:   make(map[string]int, 16),
		ips:       make(map[string]int, len(seguidas)),
		seguidas:  seguidas,
	}
}

func (a *acumulador) registrar(e *evento) {
	a.endpoints[e.ruta]++
	a.estados[e.estado]++
	if a.seguidas[e.ip] {
		a.ips[e.ip]++
	}
	a.porHora[e.t.Hour()]++
	a.ultima = e.t
}

func (a *acumulador) verificacion() Verificacion {
	return Verificacion{
		Hasta:              a.ultima,
		TopEndpoints:       agregado.TopNSort(agregado.Mapa[string](a.endpoints), 10),
		TopEstados:         agregado.TopNSort(agregado.Mapa[string](a.estados), 10),
		TopIPsActivas:      agregado.TopNSort(agregado.Mapa[string](a.ips), 10),
		SolicitudesPorHora: a.porHora,
		HoraPico:           agregado.HoraPico(a.porHora),
	}
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
			ip: ip, usuario: "-", metodo: metodoPOST, ruta: "/usr/login", proto: http11,
			estado: estado, bytes: bytesDe(r, catPagina, estado),
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
			ip: ip, usuario: usuario, metodo: metodoPOST, ruta: endpoint, proto: http11,
			estado: est, bytes: bytesDe(r, catAPI, est),
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
				ip: ip, usuario: "-", metodo: metodoGET, ruta: ruta, proto: http11,
				estado: estado, bytes: bytesDe(r, catPagina, estado),
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
