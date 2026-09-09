// Comando generar: produce un archivo de log sintético en formato Apache Combined
// y un ground-truth.json con las anomalías inyectadas y los agregados exactos.
//
//	go run ./cmd/generar -lineas 1000000
//	go run ./cmd/generar -lineas 20000000 -salida grande.log -tasa 3.0
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"parcial1/internal/loggen"
)

func main() {
	lineas := flag.Int("lineas", 1_000_000, "cantidad de líneas a generar")
	semilla := flag.Uint64("semilla", 0, "semilla del generador (0 = fija reproducible)")
	salida := flag.String("salida", "logfiles.log", "archivo de log de salida")
	verif := flag.String("verificacion", "ground-truth.json", "archivo JSON con el ground truth")
	desde := flag.String("desde", "2024-01-01", "fecha de la primera solicitud (YYYY-MM-DD)")
	tasa := flag.Float64("tasa", 1.65, "solicitudes por segundo promedio")
	heavy := flag.Int("heavy", 200, "tamaño del pool de IPs de alta actividad")
	flag.Parse()

	zona := time.FixedZone("-0300", -3*3600)
	d, err := time.ParseInLocation("2006-01-02", *desde, zona)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fecha inválida %q: %v\n", *desde, err)
		os.Exit(1)
	}

	f, err := os.Create(*salida)
	if err != nil {
		fmt.Fprintf(os.Stderr, "no se pudo crear %q: %v\n", *salida, err)
		os.Exit(1)
	}
	defer f.Close()

	opts := loggen.Opciones{
		Lineas:       *lineas,
		Semilla:      *semilla,
		Desde:        d,
		TasaPorSeg:   *tasa,
		CantHeavyIPs: *heavy,
		Zona:         zona,
	}

	t0 := time.Now()
	ver, err := loggen.Generar(f, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error generando: %v\n", err)
		os.Exit(1)
	}
	transcurrido := time.Since(t0)

	if err := f.Sync(); err != nil {
		fmt.Fprintf(os.Stderr, "error al sincronizar %q: %v\n", *salida, err)
		os.Exit(1)
	}

	vf, err := os.Create(*verif)
	if err != nil {
		fmt.Fprintf(os.Stderr, "no se pudo crear %q: %v\n", *verif, err)
		os.Exit(1)
	}
	defer vf.Close()
	enc := json.NewEncoder(vf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(ver); err != nil {
		fmt.Fprintf(os.Stderr, "error escribiendo verificación: %v\n", err)
		os.Exit(1)
	}

	resumen(os.Stdout, *salida, transcurrido, ver)
}

func resumen(w *os.File, ruta string, transcurrido time.Duration, ver loggen.Verificacion) {
	info, _ := os.Stat(ruta)
	var bytes int64
	if info != nil {
		bytes = info.Size()
	}
	span := ver.Hasta.Sub(ver.Desde)
	lineasPorSeg := float64(ver.Lineas) / transcurrido.Seconds()
	mbPorSeg := float64(bytes) / (1 << 20) / transcurrido.Seconds()

	fmt.Fprintf(w, "\nGenerado %q\n", ruta)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  líneas\t%d\n", ver.Lineas)
	fmt.Fprintf(tw, "  tamaño\t%s\n", formatearBytes(bytes))
	fmt.Fprintf(tw, "  tiempo\t%s  (%.0f líneas/s, %.1f MB/s)\n", transcurrido.Round(time.Millisecond), lineasPorSeg, mbPorSeg)
	fmt.Fprintf(tw, "  período\t%s → %s  (%s)\n", ver.Desde.Format("2006-01-02"), ver.Hasta.Format("2006-01-02"), span.Round(time.Hour))
	fmt.Fprintf(tw, "  hora pico\t%02d:00 h  (%d solicitudes)\n", ver.HoraPico, ver.SolicitudesPorHora[ver.HoraPico])
	tw.Flush()

	fmt.Fprintln(w, "\n  Top 5 endpoints:")
	for _, p := range primeros(ver.TopEndpoints, 5) {
		fmt.Fprintf(w, "    %-24s %d\n", p.Clave, p.Conteo)
	}
	fmt.Fprintln(w, "  Top 5 status:")
	for _, p := range primeros(ver.TopEstados, 5) {
		fmt.Fprintf(w, "    %-24s %d\n", p.Clave, p.Conteo)
	}
	fmt.Fprintln(w, "  Anomalías inyectadas:")
	for _, a := range ver.Anomalias {
		fmt.Fprintf(w, "    [%s] %s\n", a.Tipo, a.Detalle)
		fmt.Fprintf(w, "      %s → %s", a.Desde.Format("2006-01-02 15:04:05"), a.Hasta.Format("15:04:05"))
		if a.IP != "" {
			fmt.Fprintf(w, "  ip=%s", a.IP)
		}
		if a.Endpoint != "" {
			fmt.Fprintf(w, "  endpoint=%s", a.Endpoint)
		}
		fmt.Fprintf(w, "  n=%d\n", a.Cantidad)
	}
}

func primeros(p []loggen.Par, n int) []loggen.Par {
	if len(p) > n {
		return p[:n]
	}
	return p
}

func formatearBytes(b int64) string {
	switch {
	case b < 1<<10:
		return fmt.Sprintf("%d B", b)
	case b < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	case b < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%.2f GB", float64(b)/(1<<30))
	}
}
