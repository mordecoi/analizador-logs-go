// Package anomalias detecta patrones anómalos en una pasada secuencial sobre el
// log en orden cronológico:
//
//   - Ráfaga: una IP concentra demasiadas solicitudes en una ventana corta
//     (ventana deslizante de timestamps por IP, O(1) amortizado por evento).
//   - Pico de 5xx: una franja horaria con errores de servidor muy por encima de
//     lo típico para esa hora del día (conteo por hora + umbral μ + k·σ contra
//     las franjas de la misma hora, al final). Las franjas anómalas seguidas se
//     unen en un episodio.
//   - Escaneo: una IP acumula muchas rutas distintas con 404 en pocos minutos
//     (ventana deslizante de rutas 404 por IP). Un usuario intenso legítimo
//     también termina tocando todas las rutas, pero a lo largo de días y sin
//     404 masivos; el escáner lo hace en minutos.
//
// Los detectores necesitan las entradas en orden temporal: por eso corren en la
// pasada secuencial y no dentro del map-reduce por bloques, que rompería las
// ventanas en los límites de bloque.
package anomalias

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"parcial1/internal/parse"
)

// Hallazgo es una anomalía detectada, con su ventana temporal.
type Hallazgo struct {
	Tipo     string // "rafaga" | "pico_5xx" | "escaneo"
	IP       string
	Endpoint string
	Desde    time.Time
	Hasta    time.Time
	Cantidad int
	Detalle  string
}

// Config son los umbrales de los detectores.
type Config struct {
	VentanaRafaga time.Duration // largo de la ventana por IP
	UmbralRafaga  int           // eventos en la ventana que abren un episodio

	VentanaEscaneo time.Duration // largo de la ventana de 404s por IP
	UmbralEscaneo  int           // rutas distintas con 404 que abren un episodio

	FactorPico  float64 // cuántos desvíos sobre la media marca una franja anómala
	VecesTipico float64 // además, cuántas veces lo típico de su hora tiene que superar
	PisoPico    int     // mínimo absoluto de 5xx para considerar una franja
}

// ConfigPorDefecto devuelve los umbrales calibrados con margen contra el tráfico normal medido
// (la IP más activa ronda 15 solicitudes/min en hora pico y junta ~5 rutas
// distintas con 404 por ventana de 10 min). El umbral de ráfaga además queda
// por encima del ritmo de un escáner (~66/min), para que cada ataque lo
// reporte un solo detector.
func ConfigPorDefecto() Config {
	return Config{
		VentanaRafaga:  time.Minute,
		UmbralRafaga:   100,
		VentanaEscaneo: 10 * time.Minute,
		UmbralEscaneo:  15,
		FactorPico:     3,
		VecesTipico:    2,
		PisoPico:       50,
	}
}

// ventanaIP es la ventana deslizante de timestamps de una IP.
type ventanaIP struct {
	ip       string  // copia propia de la IP (la del evento es una vista temporal)
	ts       []int64 // cola FIFO de timestamps (unix) dentro de la ventana
	enCurso  bool
	desde    int64
	hasta    int64
	cantidad int
	maximo   int
}

// ventana404 es la ventana deslizante de rutas con 404 de una IP.
type ventana404 struct {
	ip       string // copia propia de la IP
	eventos  []evento404
	enVent   map[string]int // ruta → apariciones dentro de la ventana
	enCurso  bool
	desde    int64
	hasta    int64
	rutasEp  map[string]struct{} // rutas distintas acumuladas en el episodio
	cantidad int
}

type evento404 struct {
	t    int64
	ruta string
}

// franja acumula los 5xx de una hora calendario.
type franja struct {
	total   int
	porRuta map[string]int
}

// Detector acumula estado por IP y por franja; Procesar es O(1) amortizado.
type Detector struct {
	cfg       Config
	rafagas   map[string]*ventanaIP
	escaneos  map[string]*ventana404
	franjas   map[int64]*franja // hora unix (truncada) → 5xx
	rutas     map[string]string // internado de rutas: vista → copia propia
	hallazgos []Hallazgo
	zona      *time.Location // para reportar los picos en la zona del log
}

// NuevoDetector devuelve un detector vacío con los umbrales de cfg.
func NuevoDetector(cfg Config) *Detector {
	return &Detector{
		cfg:      cfg,
		rafagas:  make(map[string]*ventanaIP),
		escaneos: make(map[string]*ventana404),
		franjas:  make(map[int64]*franja),
		rutas:    make(map[string]string),
	}
}

// ruta devuelve una copia propia de r, compartida por todas sus apariciones.
// Las entradas que llegan a Procesar pueden ser vistas sobre el buffer del
// lector (que se reutiliza), así que nada de ellas se guarda sin copiar. Hay
// pocas rutas distintas, así que internarlas cuesta una búsqueda por evento y
// una copia por ruta, en vez de una copia por evento.
func (d *Detector) ruta(r string) string {
	if propia, ok := d.rutas[r]; ok {
		return propia
	}
	propia := strings.Clone(r)
	d.rutas[propia] = propia
	return propia
}

// Procesar incorpora una entrada (deben llegar en orden cronológico).
func (d *Detector) Procesar(e parse.Entrada) {
	if d.zona == nil {
		d.zona = e.Fecha.Location()
	}
	t := e.Fecha.Unix()
	d.procesarRafaga(e.IP, t)
	if e.Estado >= 500 {
		d.procesarPico(d.ruta(e.Ruta), t)
	}
	if e.Estado == 404 {
		d.procesarEscaneo(e.IP, d.ruta(e.Ruta), t)
	}
}

func (d *Detector) procesarRafaga(ip string, t int64) {
	v := d.rafagas[ip]
	if v == nil {
		v = &ventanaIP{ip: strings.Clone(ip)}
		d.rafagas[v.ip] = v
	}
	corte := t - int64(d.cfg.VentanaRafaga.Seconds())
	for len(v.ts) > 0 && v.ts[0] <= corte {
		v.ts = v.ts[1:] // no retiene de más: la ventana es corta (lección P4 ej. 4)
	}
	v.ts = append(v.ts, t)

	switch {
	case len(v.ts) >= d.cfg.UmbralRafaga && !v.enCurso:
		v.enCurso = true
		v.desde = v.ts[0]
		v.hasta = t
		v.cantidad = len(v.ts)
		v.maximo = len(v.ts)
	case v.enCurso && len(v.ts) >= d.cfg.UmbralRafaga/2:
		// sigue el episodio (histéresis: no se cierra por una pausa breve)
		v.hasta = t
		v.cantidad++
		if len(v.ts) > v.maximo {
			v.maximo = len(v.ts)
		}
	case v.enCurso:
		d.cerrarRafaga(v)
	}
}

func (d *Detector) cerrarRafaga(v *ventanaIP) {
	d.hallazgos = append(d.hallazgos, Hallazgo{
		Tipo: "rafaga", IP: v.ip,
		Desde: d.instante(v.desde), Hasta: d.instante(v.hasta),
		Cantidad: v.cantidad,
		Detalle: fmt.Sprintf("%d solicitudes; hasta %d dentro de una ventana de %s",
			v.cantidad, v.maximo, d.cfg.VentanaRafaga),
	})
	v.enCurso = false
}

func (d *Detector) procesarEscaneo(ip, ruta string, t int64) {
	v := d.escaneos[ip]
	if v == nil {
		v = &ventana404{ip: strings.Clone(ip), enVent: make(map[string]int)}
		d.escaneos[v.ip] = v
	}
	corte := t - int64(d.cfg.VentanaEscaneo.Seconds())
	for len(v.eventos) > 0 && v.eventos[0].t <= corte {
		viejo := v.eventos[0]
		v.eventos = v.eventos[1:]
		if v.enVent[viejo.ruta]--; v.enVent[viejo.ruta] == 0 {
			delete(v.enVent, viejo.ruta)
		}
	}
	v.eventos = append(v.eventos, evento404{t, ruta})
	v.enVent[ruta]++

	switch {
	case len(v.enVent) >= d.cfg.UmbralEscaneo && !v.enCurso:
		v.enCurso = true
		v.desde = v.eventos[0].t
		v.hasta = t
		v.rutasEp = make(map[string]struct{}, len(v.enVent))
		for r := range v.enVent {
			v.rutasEp[r] = struct{}{}
		}
		v.cantidad = len(v.eventos)
	case v.enCurso && len(v.enVent) >= d.cfg.UmbralEscaneo/2:
		v.hasta = t
		v.rutasEp[ruta] = struct{}{}
		v.cantidad++
	case v.enCurso:
		d.cerrarEscaneo(v)
	}
}

func (d *Detector) cerrarEscaneo(v *ventana404) {
	d.hallazgos = append(d.hallazgos, Hallazgo{
		Tipo: "escaneo", IP: v.ip,
		Desde: d.instante(v.desde), Hasta: d.instante(v.hasta),
		Cantidad: v.cantidad,
		Detalle: fmt.Sprintf("%d respuestas 404 sobre %d rutas distintas en la ventana",
			v.cantidad, len(v.rutasEp)),
	})
	v.enCurso = false
}

func (d *Detector) procesarPico(ruta string, t int64) {
	hora := t - t%3600
	f := d.franjas[hora]
	if f == nil {
		f = &franja{porRuta: make(map[string]int)}
		d.franjas[hora] = f
	}
	f.total++
	f.porRuta[ruta]++
}

// Hallazgos cierra los episodios abiertos, analiza las franjas de 5xx y
// devuelve todo ordenado cronológicamente.
func (d *Detector) Hallazgos() []Hallazgo {
	for _, v := range d.rafagas {
		if v.enCurso {
			d.cerrarRafaga(v)
		}
	}
	for _, v := range d.escaneos {
		if v.enCurso {
			d.cerrarEscaneo(v)
		}
	}
	d.analizarPicos()

	slices.SortFunc(d.hallazgos, func(a, b Hallazgo) int { return a.Desde.Compare(b.Desde) })
	return d.hallazgos
}

// estadistica acumula suma y suma de cuadrados para calcular media y desvío
// dejando afuera un valor (leave-one-out) en O(1).
type estadistica struct {
	n           int
	suma, suma2 float64
}

func (e *estadistica) agregar(x float64) {
	e.n++
	e.suma += x
	e.suma2 += x * x
}

// sin devuelve media y desvío de los valores acumulados sin contar x. El
// desvío nunca baja de √media: los conteos son de tipo Poisson, y con pocas
// muestras (6 en un log de una semana) el desvío estimado puede salir
// artificialmente chico y dejar el umbral demasiado cerca de la media.
func (e estadistica) sin(x float64) (media, desvio float64) {
	n := float64(e.n - 1)
	media = (e.suma - x) / n
	varianza := (e.suma2-x*x)/n - media*media
	return media, math.Max(math.Sqrt(math.Max(0, varianza)), math.Sqrt(math.Max(0, media)))
}

// minFranjasPorHora es cuántas franjas de una misma hora del día hacen falta
// para usarlas como referencia. Con menos (logs de un par de días) se compara
// contra todas las franjas, como en el primer parcial.
const minFranjasPorHora = 4

// analizarPicos marca las franjas cuyo total de 5xx supera μ + k·σ.
//
// Dos decisiones, cada una motivada por un caso que fallaba:
//
//   - μ y σ se calculan **sin la franja candidata** (leave-one-out). Con pocas
//     franjas, un outlier grande infla tanto el desvío global que queda por
//     debajo de su propio umbral y se esconde a sí mismo.
//   - La referencia son las franjas de **la misma hora del día** (estacionalidad):
//     a las 3 AM lo típico son ~17 errores y a las 13 h ~190, así que un pico
//     nocturno de 300 queda por debajo del umbral global (~360) pero es 10×
//     lo típico de su hora. Lo mostró el log de 20 M líneas, donde el pico
//     inyectado cruza el límite entre dos horas y cada mitad (~320) pasaba
//     inadvertida.
//
// Además del umbral estadístico se exige un tamaño de efecto: la franja tiene
// que superar VecesTipico veces la media de referencia. Con cientos de franjas
// evaluadas a 3σ, alguna lo supera por azar (comparaciones múltiples): en un
// log de una semana, una medianoche con 53 errores contra un típico de 35 daba
// z ≈ 3,1, estadísticamente significativo pero irrelevante en la práctica.
//
// Las franjas anómalas consecutivas se informan como un solo episodio: un pico
// que cruza una hora en punto es una anomalía, no dos. El piso absoluto evita
// marcar franjas chicas cuando σ ≈ 0.
func (d *Detector) analizarPicos() {
	if len(d.franjas) < 3 {
		return // sin suficientes franjas no hay "típico" contra el cual comparar
	}
	var global estadistica
	var porHora [24]estadistica
	for hora, f := range d.franjas {
		global.agregar(float64(f.total))
		porHora[d.instante(hora).Hour()].agregar(float64(f.total))
	}

	var anomalas []int64
	for hora, f := range d.franjas {
		ref := porHora[d.instante(hora).Hour()]
		if ref.n < minFranjasPorHora {
			ref = global
		}
		x := float64(f.total)
		media, desvio := ref.sin(x)
		if f.total >= d.cfg.PisoPico && x > media+d.cfg.FactorPico*desvio && x > d.cfg.VecesTipico*media {
			anomalas = append(anomalas, hora)
		}
	}
	slices.Sort(anomalas)

	for i := 0; i < len(anomalas); {
		j := i + 1
		for j < len(anomalas) && anomalas[j] == anomalas[j-1]+3600 {
			j++
		}
		d.reportarPico(anomalas[i:j], global)
		i = j
	}
}

// reportarPico agrega un hallazgo por un episodio de franjas consecutivas,
// atribuido a la ruta con más errores en el episodio.
func (d *Detector) reportarPico(horas []int64, global estadistica) {
	total := 0
	porRuta := make(map[string]int)
	for _, h := range horas {
		f := d.franjas[h]
		total += f.total
		for r, c := range f.porRuta {
			porRuta[r] += c
		}
	}
	ruta, maxi := "", 0
	for r, c := range porRuta {
		if c > maxi || (c == maxi && r < ruta) {
			ruta, maxi = r, c
		}
	}
	media := global.suma / float64(global.n)
	d.hallazgos = append(d.hallazgos, Hallazgo{
		Tipo: "pico_5xx", Endpoint: ruta,
		Desde:    d.instante(horas[0]),
		Hasta:    d.instante(horas[len(horas)-1] + 3600),
		Cantidad: total,
		Detalle: fmt.Sprintf("%d respuestas 5xx en %d h (típico %.0f/h); %d en %s",
			total, len(horas), media, maxi, ruta),
	})
}

// instante convierte segundos unix a un time.Time en la zona del log.
func (d *Detector) instante(unix int64) time.Time {
	zona := d.zona
	if zona == nil {
		zona = time.UTC
	}
	return time.Unix(unix, 0).In(zona)
}
