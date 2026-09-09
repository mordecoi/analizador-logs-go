// Comando analizar: procesa un archivo de log en formato Apache Combined y
// reporta errores más frecuentes, endpoints y usuarios más activos, IPs más
// activas y distribución horaria.
//
// Lectura secuencial línea a línea. El parser (regex | manual) y la estrategia
// de Top-N (sort | heap) se eligen por flag para comparar versiones.
//
//	go run ./cmd/analizar -archivo logfiles.log
//	go run ./cmd/analizar -archivo logfiles.log -verificacion ground-truth.json
//	go run ./cmd/analizar -archivo logfiles.log -parser regex -cpuprofile cpu.pprof
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"strconv"
	"text/tabwriter"
	"time"

	"parcial1/internal/agregado"
	"parcial1/internal/anomalias"
	"parcial1/internal/cache"
	"parcial1/internal/loggen"
	"parcial1/internal/parse"
	"parcial1/internal/pipeline"
)

func main() {
	archivo := flag.String("archivo", "logfiles.log", "archivo de log a analizar")
	topn := flag.Int("topn", 10, "cantidad de resultados en cada ranking")
	metStr := flag.String("topn-metodo", "heap", "estrategia de Top-N: sort | heap")
	parserStr := flag.String("parser", "manual", "parser de líneas: regex | manual")
	modo := flag.String("modo", "conc", "procesamiento: seq | conc | conc-mutex")
	workers := flag.Int("workers", 0, "cantidad de workers para el modo concurrente (0 = GOMAXPROCS)")
	usarCache := flag.Bool("cache", false, "memoizar los agregados a disco y reusarlos si el log no cambió")
	anom := flag.Bool("anomalias", false, "detectar patrones anómalos (pasada secuencial extra)")
	verif := flag.String("verificacion", "", "ground-truth.json para validar los resultados (opcional)")
	csv := flag.String("csv", "", "archivo CSV donde agregar la fila de medición (opcional)")
	version := flag.String("version", "v3-conc", "etiqueta de versión para el CSV")
	cpuprofile := flag.String("cpuprofile", "", "escribir perfil de CPU a este archivo")
	memprofile := flag.String("memprofile", "", "escribir perfil de heap a este archivo")
	traza := flag.String("trace", "", "escribir traza de ejecución (go tool trace) a este archivo")
	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer pprof.StopCPUProfile()
	}
	if *traza != "" {
		f, err := os.Create(*traza)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
		if err := trace.Start(f); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer trace.Stop()
	}

	met := agregado.MetodoHeap
	if *metStr == "sort" {
		met = agregado.MetodoSort
	}
	parsear := pipeline.Parser(parse.ParsearManual)
	if *parserStr == "regex" {
		parsear = parse.Parsear
	}
	procesar := func(ruta string) (*agregado.Contadores, int, error) {
		switch *modo {
		case "seq":
			return pipeline.Secuencial(ruta, parsear)
		case "conc-mutex":
			return pipeline.ConcurrenteMutex(ruta, *workers, parsear)
		default:
			return pipeline.Concurrente(ruta, *workers, parsear)
		}
	}

	var cont *agregado.Contadores
	var med medicion
	var err error
	desdeCache := false

	if *usarCache {
		cont, med, desdeCache = cargarMemoizado(*archivo)
	}
	if cont == nil {
		cont, med, err = analizar(*archivo, procesar)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if *usarCache {
			if err := cache.Guardar(*archivo, cont); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}
	}

	q := cache.NuevasConsultas(cont, met)

	reportar(os.Stdout, q, med, *topn, desdeCache)

	if *verif != "" {
		ok, err := validar(os.Stdout, q, *verif, *topn)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if !ok {
			os.Exit(2)
		}
	}
	fmt.Printf("\nCache de consultas Top-N: %d hits, %d misses\n", q.Hits, q.Misses)

	if *anom {
		if err := detectarAnomalias(os.Stdout, *archivo, parsear, *verif); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	if *csv != "" {
		if err := agregarCSV(*csv, *version, *archivo, cont, med); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
		runtime.GC()
		if err := pprof.WriteHeapProfile(f); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		// sin esto, el compilador da por muertos los contadores antes del GC de
		// arriba y el perfil "inuse" sale vacío: hay que mantenerlos vivos hasta
		// después de escribir el perfil.
		runtime.KeepAlive(cont)
	}
}

// medicion junta lo que se reporta de rendimiento de una corrida.
type medicion struct {
	Transcurrido time.Duration
	HeapFinal    uint64 // memoria retenida por los contadores tras procesar
	TotalAsign   uint64 // memoria total asignada durante el proceso (presión de GC)
	Asignaciones uint64 // cantidad de asignaciones de heap
	CiclosGC     uint32
	Errores      int
}

func analizar(ruta string, procesar func(string) (*agregado.Contadores, int, error)) (*agregado.Contadores, medicion, error) {
	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	t0 := time.Now()

	cont, errores, err := procesar(ruta)
	if err != nil {
		return nil, medicion{}, err
	}

	transcurrido := time.Since(t0)
	runtime.GC()
	runtime.ReadMemStats(&m1)

	return cont, medicion{
		Transcurrido: transcurrido,
		HeapFinal:    m1.HeapAlloc,
		TotalAsign:   m1.TotalAlloc - m0.TotalAlloc,
		Asignaciones: m1.Mallocs - m0.Mallocs,
		CiclosGC:     m1.NumGC - m0.NumGC,
		Errores:      errores,
	}, nil
}

// detectarAnomalias hace la pasada secuencial extra con los detectores de
// ventana (necesitan las entradas en orden temporal, por eso no va dentro del
// map-reduce) y, si hay ground truth, calcula precisión y recall.
func detectarAnomalias(w *os.File, ruta string, parsear pipeline.Parser, verif string) error {
	d := anomalias.NuevoDetector(anomalias.ConfigPorDefecto())
	t0 := time.Now()
	if _, err := pipeline.Recorrer(ruta, parsear, d.Procesar); err != nil {
		return err
	}
	hs := d.Hallazgos()
	fmt.Fprintf(w, "\nDetección de anomalías  (pasada secuencial extra: %s)\n",
		time.Since(t0).Round(time.Millisecond))
	if len(hs) == 0 {
		fmt.Fprintln(w, "  sin hallazgos")
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for i, h := range hs {
		quien := h.IP
		if h.Endpoint != "" {
			quien = h.Endpoint
		}
		fmt.Fprintf(tw, "  %d.\t[%s]\t%s\t%s → %s\t%s\n",
			i+1, h.Tipo, quien,
			h.Desde.Format("02/01 15:04:05"), h.Hasta.Format("15:04:05"), h.Detalle)
	}
	tw.Flush()

	if verif == "" {
		return nil
	}
	datos, err := os.ReadFile(verif)
	if err != nil {
		return err
	}
	var ver loggen.Verificacion
	if err := json.Unmarshal(datos, &ver); err != nil {
		return err
	}

	fmt.Fprintln(w, "\n  Contra el ground truth:")
	twv := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	detectadas := 0
	usados := make(map[int]bool)
	for _, a := range ver.Anomalias {
		estado := "NO DETECTADA"
		for i, h := range hs {
			if !usados[i] && coincideAnomalia(h, a) {
				estado = "DETECTADA"
				usados[i] = true
				detectadas++
				break
			}
		}
		fmt.Fprintf(twv, "    [%s]\t%s%s\t%s\n", a.Tipo, a.IP, a.Endpoint, estado)
	}
	twv.Flush()
	precision := 1.0
	if len(hs) > 0 {
		precision = float64(len(usados)) / float64(len(hs))
	}
	fmt.Fprintf(w, "    recall %d/%d, precisión %.0f%%  (%d hallazgos, %d falsos positivos)\n",
		detectadas, len(ver.Anomalias), 100*precision, len(hs), len(hs)-len(usados))
	return nil
}

// coincideAnomalia: mismo tipo, misma IP/endpoint (si el ground truth los
// declara) y ventanas temporales que se solapan.
func coincideAnomalia(h anomalias.Hallazgo, a loggen.Anomalia) bool {
	if h.Tipo != a.Tipo {
		return false
	}
	if a.IP != "" && h.IP != a.IP {
		return false
	}
	if a.Endpoint != "" && h.Endpoint != a.Endpoint {
		return false
	}
	return h.Desde.Before(a.Hasta) && a.Desde.Before(h.Hasta)
}

// cargarMemoizado intenta traer los agregados desde el cache en disco, midiendo
// cuánto cuesta la deserialización. Devuelve nil si fue miss (e informa por qué).
func cargarMemoizado(ruta string) (*agregado.Contadores, medicion, bool) {
	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	t0 := time.Now()

	cont, motivo := cache.Cargar(ruta)

	transcurrido := time.Since(t0)
	runtime.GC()
	runtime.ReadMemStats(&m1)

	if cont == nil {
		fmt.Printf("Cache de agregados: miss (%s); se procesa el archivo.\n", motivo)
		return nil, medicion{}, false
	}
	return cont, medicion{
		Transcurrido: transcurrido,
		HeapFinal:    m1.HeapAlloc,
		TotalAsign:   m1.TotalAlloc - m0.TotalAlloc,
		Asignaciones: m1.Mallocs - m0.Mallocs,
		CiclosGC:     m1.NumGC - m0.NumGC,
	}, true
}

func reportar(w *os.File, q *cache.Consultas, m medicion, n int, desdeCache bool) {
	c := q.Cont
	fmt.Fprintf(w, "Analizadas %d líneas", c.Total)
	if m.Errores > 0 {
		fmt.Fprintf(w, " (%d no reconocidas)", m.Errores)
	}
	if desdeCache {
		fmt.Fprintf(w, "  [agregados memoizados: no se re-parseó el archivo]")
	}
	fmt.Fprintln(w)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  tiempo\t%s  (%.0f líneas/s)\n",
		m.Transcurrido.Round(time.Millisecond),
		float64(c.Total)/m.Transcurrido.Seconds())
	fmt.Fprintf(tw, "  heap retenido\t%s\n", mb(m.HeapFinal))
	fmt.Fprintf(tw, "  total asignado\t%s  en %.1f M asignaciones, %d ciclos de GC\n",
		mb(m.TotalAsign), float64(m.Asignaciones)/1e6, m.CiclosGC)
	tw.Flush()

	rankStr(w, "Errores y estados más frecuentes", topEstados(q, n), c.Total)
	rankStr(w, "Endpoints más solicitados", q.Endpoints(n), c.Total)
	rankStr(w, "IPs más activas", q.IPs(n), c.Total)
	rankStr(w, "Usuarios más activos", q.Usuarios(n), c.Total)

	fmt.Fprintf(w, "\nSolicitudes por hora  (pico: %02d:00 h)\n", c.HoraPico())
	twh := tabwriter.NewWriter(w, 0, 0, 1, ' ', 0)
	maxH := c.PorHora[c.HoraPico()]
	for h, cnt := range c.PorHora {
		fmt.Fprintf(twh, "  %02d:00\t%d\t%s\n", h, cnt, barra(cnt, maxH, 40))
	}
	twh.Flush()
}

// topEstados adapta el ranking de estados (clave int) a claves string.
func topEstados(q *cache.Consultas, n int) []agregado.Par[string] {
	crudo := q.Estados(n)
	res := make([]agregado.Par[string], len(crudo))
	for i, p := range crudo {
		res[i] = agregado.Par[string]{Clave: strconv.Itoa(p.Clave), Conteo: p.Conteo}
	}
	return res
}

func rankStr(w *os.File, titulo string, pares []agregado.Par[string], total int) {
	fmt.Fprintf(w, "\n%s\n", titulo)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, p := range pares {
		fmt.Fprintf(tw, "  %s\t%d\t%.1f%%\n", p.Clave, p.Conteo, 100*float64(p.Conteo)/float64(total))
	}
	tw.Flush()
}

// validar compara los resultados contra el ground-truth.json del generador.
func validar(w *os.File, q *cache.Consultas, ruta string, n int) (bool, error) {
	c := q.Cont
	datos, err := os.ReadFile(ruta)
	if err != nil {
		return false, fmt.Errorf("no se pudo leer %q: %w", ruta, err)
	}
	var ver loggen.Verificacion
	if err := json.Unmarshal(datos, &ver); err != nil {
		return false, fmt.Errorf("ground truth inválido: %w", err)
	}

	fmt.Fprintln(w, "\nValidación contra el ground truth")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	ok := true

	chequear := func(nombre string, cond bool, detalle string) {
		estado := "OK"
		if !cond {
			estado, ok = "DIFIERE", false
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", nombre, estado, detalle)
	}

	rankearYchequear := func(nombre string, nuestro []agregado.Par[string], gt []loggen.Par) {
		chequear(nombre, coincideRanking(nuestro, gt), resumenRanking(nuestro, gt))
	}

	chequear("total de líneas", c.Total == ver.Lineas,
		fmt.Sprintf("%d vs %d", c.Total, ver.Lineas))
	chequear("hora pico", c.HoraPico() == ver.HoraPico,
		fmt.Sprintf("%02d:00 vs %02d:00", c.HoraPico(), ver.HoraPico))
	rankearYchequear("top endpoints", q.Endpoints(n), ver.TopEndpoints)
	rankearYchequear("top estados", topEstados(q, n), ver.TopEstados)
	rankearYchequear("top IPs", q.IPs(n), ver.TopIPsActivas)
	tw.Flush()
	return ok, nil
}

// coincideRanking exige mismas claves, mismo orden y mismos conteos que el
// ground truth (comparando solo hasta el largo del ground truth).
func coincideRanking(nuestro []agregado.Par[string], gt []loggen.Par) bool {
	if len(nuestro) < len(gt) {
		return false
	}
	for i, g := range gt {
		if nuestro[i].Clave != g.Clave || nuestro[i].Conteo != g.Conteo {
			return false
		}
	}
	return true
}

func resumenRanking(nuestro []agregado.Par[string], gt []loggen.Par) string {
	if len(gt) == 0 {
		return "(ground truth vacío)"
	}
	if coincideRanking(nuestro, gt) {
		return fmt.Sprintf("%d entradas idénticas", len(gt))
	}
	primeraDif := "?"
	for i, g := range gt {
		if i >= len(nuestro) || nuestro[i].Clave != g.Clave || nuestro[i].Conteo != g.Conteo {
			nc := "∅"
			if i < len(nuestro) {
				nc = fmt.Sprintf("%s=%d", nuestro[i].Clave, nuestro[i].Conteo)
			}
			primeraDif = fmt.Sprintf("pos %d: %s vs esperado %s=%d", i, nc, g.Clave, g.Conteo)
			break
		}
	}
	return primeraDif
}

func agregarCSV(ruta, version, archivo string, c *agregado.Contadores, m medicion) error {
	if dir := filepath.Dir(ruta); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	nuevo := false
	if info, err := os.Stat(ruta); err != nil || info.Size() == 0 {
		nuevo = true
	}
	f, err := os.OpenFile(ruta, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	if nuevo {
		fmt.Fprintln(f, "version,archivo,lineas,ms,lineas_por_seg,heap_mb,total_alloc_mb,allocs_millones,num_gc")
	}
	fmt.Fprintf(f, "%s,%s,%d,%.1f,%.0f,%.1f,%.1f,%.2f,%d\n",
		version, filepath.Base(archivo), c.Total,
		float64(m.Transcurrido.Nanoseconds())/1e6,
		float64(c.Total)/m.Transcurrido.Seconds(),
		float64(m.HeapFinal)/(1<<20),
		float64(m.TotalAsign)/(1<<20),
		float64(m.Asignaciones)/1e6,
		m.CiclosGC)
	return nil
}

func mb(b uint64) string {
	return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
}

func barra(v, max, ancho int) string {
	if max == 0 {
		return ""
	}
	n := v * ancho / max
	s := make([]byte, n)
	for i := range s {
		s[i] = '#'
	}
	return string(s)
}
