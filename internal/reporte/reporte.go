// Package reporte presenta los resultados del análisis: el resumen de
// rendimiento, los rankings, el histograma horario, la validación contra el
// ground truth, los hallazgos de anomalías y la fila de medición en CSV.
//
// Todo escribe sobre un io.Writer, así la presentación se testea sin ejecutar
// el comando y sin capturar la salida estándar.
package reporte

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"parcial1/internal/agregado"
	"parcial1/internal/anomalias"
	"parcial1/internal/cache"
	"parcial1/internal/loggen"
	"parcial1/internal/medicion"
)

// Corrida describe una ejecución del análisis, para el encabezado del reporte.
type Corrida struct {
	Medicion   medicion.Medicion
	Errores    int  // líneas no reconocidas
	DesdeCache bool // los agregados salieron de la memoización
}

// nuevaTabla devuelve un tabwriter con el formato común a todas las tablas.
func nuevaTabla(w io.Writer, separacion int) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, separacion, ' ', 0)
}

// Resumen escribe el reporte principal: rendimiento, los cuatro rankings y la
// distribución por hora.
func Resumen(w io.Writer, q *cache.Consultas, c Corrida, n int) {
	cont := q.Cont
	fmt.Fprintf(w, "Analizadas %d líneas", cont.Total)
	if c.Errores > 0 {
		fmt.Fprintf(w, " (%d no reconocidas)", c.Errores)
	}
	if c.DesdeCache {
		fmt.Fprint(w, "  [agregados memoizados: no se re-parseó el archivo]")
	}
	fmt.Fprintln(w)

	m := c.Medicion
	tw := nuevaTabla(w, 2)
	fmt.Fprintf(tw, "  tiempo\t%s  (%.0f líneas/s)\n",
		m.Transcurrido.Round(time.Millisecond), float64(cont.Total)/m.Transcurrido.Seconds())
	fmt.Fprintf(tw, "  heap retenido\t%s\n", medicion.Bytes(m.HeapFinal))
	fmt.Fprintf(tw, "  total asignado\t%s  en %.1f M asignaciones, %d ciclos de GC\n",
		medicion.Bytes(m.TotalAsign), float64(m.Asignaciones)/1e6, m.CiclosGC)
	tw.Flush()

	ranking(w, "Errores y estados más frecuentes", q.Estados(n), cont.Total)
	ranking(w, "Endpoints más solicitados", q.Endpoints(n), cont.Total)
	ranking(w, "IPs más activas", q.IPs(n), cont.Total)
	ranking(w, "Usuarios más activos", q.Usuarios(n), cont.Total)
	histogramaHorario(w, cont)
}

func ranking(w io.Writer, titulo string, pares []agregado.Par[string], total int) {
	fmt.Fprintf(w, "\n%s\n", titulo)
	tw := nuevaTabla(w, 2)
	for _, p := range pares {
		fmt.Fprintf(tw, "  %s\t%d\t%.1f%%\n", p.Clave, p.Conteo, 100*float64(p.Conteo)/float64(total))
	}
	tw.Flush()
}

func histogramaHorario(w io.Writer, c *agregado.Contadores) {
	pico := c.HoraPico()
	fmt.Fprintf(w, "\nSolicitudes por hora  (pico: %02d:00 h)\n", pico)
	tw := nuevaTabla(w, 1)
	for h, cnt := range c.PorHora {
		fmt.Fprintf(tw, "  %02d:00\t%d\t%s\n", h, cnt, Barra(cnt, c.PorHora[pico], 40))
	}
	tw.Flush()
}

// Barra dibuja v como una barra de '#' proporcional a maximo, de a lo sumo
// ancho caracteres.
func Barra(v, maximo, ancho int) string {
	if maximo <= 0 {
		return ""
	}
	return strings.Repeat("#", v*ancho/maximo)
}

// LeerVerificacion carga el ground-truth.json que escribe el generador.
func LeerVerificacion(ruta string) (loggen.Verificacion, error) {
	var ver loggen.Verificacion
	datos, err := os.ReadFile(ruta)
	if err != nil {
		return ver, fmt.Errorf("no se pudo leer %q: %w", ruta, err)
	}
	if err := json.Unmarshal(datos, &ver); err != nil {
		return ver, fmt.Errorf("ground truth inválido: %w", err)
	}
	return ver, nil
}

// Validar compara los resultados contra el ground truth del generador y
// escribe una línea por chequeo. Devuelve si todos coinciden.
func Validar(w io.Writer, q *cache.Consultas, ver loggen.Verificacion, n int) bool {
	c := q.Cont
	fmt.Fprintln(w, "\nValidación contra el ground truth")
	tw := nuevaTabla(w, 2)
	ok := true
	chequear := func(nombre string, cond bool, detalle string) {
		estado := "OK"
		if !cond {
			estado, ok = "DIFIERE", false
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", nombre, estado, detalle)
	}
	rankings := func(nombre string, nuestro, gt []agregado.Par[string]) {
		i := PrimeraDiferencia(nuestro, gt)
		chequear(nombre, i < 0, detalleRanking(nuestro, gt, i))
	}

	chequear("total de líneas", c.Total == ver.Lineas, fmt.Sprintf("%d vs %d", c.Total, ver.Lineas))
	chequear("hora pico", c.HoraPico() == ver.HoraPico,
		fmt.Sprintf("%02d:00 vs %02d:00", c.HoraPico(), ver.HoraPico))
	rankings("top endpoints", q.Endpoints(n), ver.TopEndpoints)
	rankings("top estados", q.Estados(n), ver.TopEstados)
	rankings("top IPs", q.IPs(n), ver.TopIPsActivas)
	tw.Flush()
	return ok
}

// PrimeraDiferencia devuelve la primera posición en que nuestro ranking difiere
// del ground truth (misma clave, mismo conteo, mismo orden), o -1 si coinciden.
// Solo se compara hasta el largo del ground truth.
func PrimeraDiferencia(nuestro, gt []agregado.Par[string]) int {
	for i, g := range gt {
		if i >= len(nuestro) || nuestro[i] != g {
			return i
		}
	}
	return -1
}

func detalleRanking(nuestro, gt []agregado.Par[string], dif int) string {
	switch {
	case len(gt) == 0:
		return "(ground truth vacío)"
	case dif < 0:
		return fmt.Sprintf("%d entradas idénticas", len(gt))
	case dif >= len(nuestro):
		return fmt.Sprintf("pos %d: ∅ vs esperado %s=%d", dif, gt[dif].Clave, gt[dif].Conteo)
	default:
		return fmt.Sprintf("pos %d: %s=%d vs esperado %s=%d", dif,
			nuestro[dif].Clave, nuestro[dif].Conteo, gt[dif].Clave, gt[dif].Conteo)
	}
}

// Anomalias escribe los hallazgos del detector y cuánto tardó la pasada.
func Anomalias(w io.Writer, hs []anomalias.Hallazgo, duracion time.Duration) {
	fmt.Fprintf(w, "\nDetección de anomalías  (pasada secuencial extra: %s)\n", duracion.Round(time.Millisecond))
	if len(hs) == 0 {
		fmt.Fprintln(w, "  sin hallazgos")
	}
	tw := nuevaTabla(w, 2)
	for i, h := range hs {
		quien := h.IP
		if h.Endpoint != "" {
			quien = h.Endpoint
		}
		fmt.Fprintf(tw, "  %d.\t[%s]\t%s\t%s → %s\t%s\n",
			i+1, h.Tipo, quien, h.Desde.Format("02/01 15:04:05"), h.Hasta.Format("15:04:05"), h.Detalle)
	}
	tw.Flush()
}

// Evaluacion resume cuántas anomalías inyectadas se detectaron.
type Evaluacion struct {
	Detectadas, Inyectadas int
	Hallazgos              int
	FalsosPositivos        int
}

// Precision es la fracción de hallazgos que corresponden a una anomalía real.
func (e Evaluacion) Precision() float64 {
	if e.Hallazgos == 0 {
		return 1
	}
	return float64(e.Hallazgos-e.FalsosPositivos) / float64(e.Hallazgos)
}

// EvaluarAnomalias empareja cada anomalía inyectada con a lo sumo un hallazgo
// que coincida, escribe el resultado y devuelve precisión y recall.
func EvaluarAnomalias(w io.Writer, hs []anomalias.Hallazgo, ver loggen.Verificacion) Evaluacion {
	fmt.Fprintln(w, "\n  Contra el ground truth:")
	tw := nuevaTabla(w, 2)
	usados := make([]bool, len(hs))
	ev := Evaluacion{Inyectadas: len(ver.Anomalias), Hallazgos: len(hs)}
	for _, a := range ver.Anomalias {
		estado := "NO DETECTADA"
		for i, h := range hs {
			if !usados[i] && coincideAnomalia(h, a) {
				estado = "DETECTADA"
				usados[i] = true
				ev.Detectadas++
				break
			}
		}
		fmt.Fprintf(tw, "    [%s]\t%s%s\t%s\n", a.Tipo, a.IP, a.Endpoint, estado)
	}
	tw.Flush()
	ev.FalsosPositivos = ev.Hallazgos - ev.Detectadas
	fmt.Fprintf(w, "    recall %d/%d, precisión %.0f%%  (%d hallazgos, %d falsos positivos)\n",
		ev.Detectadas, ev.Inyectadas, 100*ev.Precision(), ev.Hallazgos, ev.FalsosPositivos)
	return ev
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

// encabezadoCSV son las columnas de la tabla comparativa de mediciones.
const encabezadoCSV = "version,archivo,lineas,ms,lineas_por_seg,heap_mb,total_alloc_mb,allocs_millones,num_gc"

// AgregarCSV agrega una fila de medición al CSV en ruta (creándolo con
// encabezado si no existe).
func AgregarCSV(ruta, version, archivo string, lineas int, m medicion.Medicion) error {
	if dir := filepath.Dir(ruta); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}
	info, err := os.Stat(ruta)
	nuevo := err != nil || info.Size() == 0

	f, err := os.OpenFile(ruta, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if nuevo {
		fmt.Fprintln(f, encabezadoCSV)
	}
	fmt.Fprintf(f, "%s,%s,%d,%.1f,%.0f,%.1f,%.1f,%.2f,%d\n",
		version, filepath.Base(archivo), lineas,
		float64(m.Transcurrido.Nanoseconds())/1e6,
		float64(lineas)/m.Transcurrido.Seconds(),
		medicion.MB(m.HeapFinal), medicion.MB(m.TotalAsign),
		float64(m.Asignaciones)/1e6, m.CiclosGC)
	return f.Close()
}
