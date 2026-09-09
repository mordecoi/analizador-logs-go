// Package anomalias detecta patrones anómalos en una pasada secuencial sobre el
// log en orden cronológico:
//
//   - Ráfaga: una IP concentra demasiadas solicitudes en una ventana corta
//     (ventana deslizante de timestamps por IP, O(1) amortizado por evento).
//   - Pico de 5xx: una franja horaria con errores de servidor muy por encima de
//     lo típico (conteo por hora + umbral estadístico μ + k·σ al final).
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

	FactorPico float64 // cuántos desvíos sobre la media marca una franja anómala
	PisoPico   int     // mínimo absoluto de 5xx para considerar una franja
}

// ConfigPorDefecto: calibrada con margen contra el tráfico normal medido
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
		PisoPico:       50,
	}
}

// ventanaIP es la ventana deslizante de timestamps de una IP.
type ventanaIP struct {
	ts       []int64 // cola FIFO de timestamps (unix) dentro de la ventana
	enCurso  bool
	desde    int64
	hasta    int64
	cantidad int
	maximo   int
}

// ventana404 es la ventana deslizante de rutas con 404 de una IP.
type ventana404 struct {
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
	hallazgos []Hallazgo
	zona      *time.Location // para reportar los picos en la zona del log
}

func NuevoDetector(cfg Config) *Detector {
	return &Detector{
		cfg:      cfg,
		rafagas:  make(map[string]*ventanaIP),
		escaneos: make(map[string]*ventana404),
		franjas:  make(map[int64]*franja),
	}
}

// Procesar incorpora una entrada (deben llegar en orden cronológico).
func (d *Detector) Procesar(e parse.Entrada) {
	if d.zona == nil {
		d.zona = e.Fecha.Location()
	}
	t := e.Fecha.Unix()
	d.procesarRafaga(e.IP, t)
	if e.Estado >= 500 {
		d.procesarPico(e.Ruta, t)
	}
	if e.Estado == 404 {
		d.procesarEscaneo(e.IP, e.Ruta, t)
	}
}

func (d *Detector) procesarRafaga(ip string, t int64) {
	v := d.rafagas[ip]
	if v == nil {
		v = &ventanaIP{}
		d.rafagas[ip] = v
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
		d.cerrarRafaga(ip, v)
	}
}

func (d *Detector) cerrarRafaga(ip string, v *ventanaIP) {
	d.hallazgos = append(d.hallazgos, Hallazgo{
		Tipo: "rafaga", IP: ip,
		Desde: time.Unix(v.desde, 0).In(d.zonaODefault()), Hasta: time.Unix(v.hasta, 0).In(d.zonaODefault()),
		Cantidad: v.cantidad,
		Detalle: fmt.Sprintf("%d solicitudes; hasta %d dentro de una ventana de %s",
			v.cantidad, v.maximo, d.cfg.VentanaRafaga),
	})
	v.enCurso = false
}

func (d *Detector) procesarEscaneo(ip, ruta string, t int64) {
	v := d.escaneos[ip]
	if v == nil {
		v = &ventana404{enVent: make(map[string]int)}
		d.escaneos[ip] = v
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
		d.cerrarEscaneo(ip, v)
	}
}

func (d *Detector) cerrarEscaneo(ip string, v *ventana404) {
	d.hallazgos = append(d.hallazgos, Hallazgo{
		Tipo: "escaneo", IP: ip,
		Desde: time.Unix(v.desde, 0).In(d.zonaODefault()), Hasta: time.Unix(v.hasta, 0).In(d.zonaODefault()),
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
	for ip, v := range d.rafagas {
		if v.enCurso {
			d.cerrarRafaga(ip, v)
		}
	}
	for ip, v := range d.escaneos {
		if v.enCurso {
			d.cerrarEscaneo(ip, v)
		}
	}
	d.analizarPicos()

	slices.SortFunc(d.hallazgos, func(a, b Hallazgo) int { return a.Desde.Compare(b.Desde) })
	return d.hallazgos
}

// analizarPicos marca las franjas cuyo total de 5xx supera μ + k·σ, calculando
// μ y σ **sin la franja candidata** (leave-one-out). Con pocas franjas, un
// outlier grande infla tanto el desvío global que queda por debajo de su propio
// umbral y se esconde a sí mismo; sacándolo del cálculo, el umbral refleja lo
// "típico" de verdad. El piso absoluto evita marcar franjas chicas cuando σ≈0.
func (d *Detector) analizarPicos() {
	if len(d.franjas) < 3 {
		return // sin suficientes franjas no hay "típico" contra el cual comparar
	}
	var suma, suma2 float64
	for _, f := range d.franjas {
		suma += float64(f.total)
		suma2 += float64(f.total) * float64(f.total)
	}
	n := float64(len(d.franjas))

	for hora, f := range d.franjas {
		x := float64(f.total)
		media := (suma - x) / (n - 1)
		varianza := (suma2-x*x)/(n-1) - media*media
		desvio := math.Sqrt(math.Max(0, varianza))
		umbral := media + d.cfg.FactorPico*desvio
		if f.total < d.cfg.PisoPico || x <= umbral {
			continue
		}
		// atribuir el pico a la ruta dominante de la franja
		ruta, maxi := "", 0
		for r, c := range f.porRuta {
			if c > maxi || (c == maxi && r < ruta) {
				ruta, maxi = r, c
			}
		}
		d.hallazgos = append(d.hallazgos, Hallazgo{
			Tipo: "pico_5xx", Endpoint: ruta,
			Desde:    time.Unix(hora, 0).In(d.zonaODefault()),
			Hasta:    time.Unix(hora+3600, 0).In(d.zonaODefault()),
			Cantidad: f.total,
			Detalle: fmt.Sprintf("%d respuestas 5xx en la franja (típico %.0f ± %.0f); %d en %s",
				f.total, media, desvio, maxi, ruta),
		})
	}
}

func (d *Detector) zonaODefault() *time.Location {
	if d.zona != nil {
		return d.zona
	}
	return time.UTC
}
