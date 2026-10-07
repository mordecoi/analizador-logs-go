package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"parcial1/internal/loggen"
	"parcial1/internal/medicion"
)

// opcionesGenerar son los flags del subcomando generar.
type opcionesGenerar struct {
	lineas       int
	semilla      uint64
	salida       string
	verificacion string
	desde        string
	tasa         float64
	heavy        int
	buffer       int
}

func comandoGenerar() *cobra.Command {
	var o opcionesGenerar
	cmd := &cobra.Command{
		Use:   "generar",
		Short: "Genera un log sintético y su ground truth",
		Long: `Genera un log sintético en formato Apache Combined con tráfico realista
(Poisson no homogéneo, popularidad tipo Zipf) y tres anomalías inyectadas, más
un JSON con los agregados exactos para validar el análisis.`,
		Example: `  analizador generar --lineas 1000000
  analizador generar -n 20000000 -o grande.log --tasa 3`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return generar(cmd.OutOrStdout(), o)
		},
	}
	f := cmd.Flags()
	f.IntVarP(&o.lineas, "lineas", "n", 1_000_000, "cantidad de líneas a generar")
	f.Uint64Var(&o.semilla, "semilla", 0, "semilla del generador (0 = fija reproducible)")
	f.StringVarP(&o.salida, "salida", "o", "logfiles.log", "archivo de log de salida")
	f.StringVar(&o.verificacion, "verificacion", "ground-truth.json", "archivo JSON con el ground truth")
	f.StringVar(&o.desde, "desde", "2024-01-01", "fecha de la primera solicitud (AAAA-MM-DD)")
	f.Float64Var(&o.tasa, "tasa", 1.65, "solicitudes por segundo promedio")
	f.IntVar(&o.heavy, "heavy", 200, "tamaño del pool de IPs de alta actividad")
	f.IntVar(&o.buffer, "buffer", 1<<20, "bytes del buffer de escritura (-1 = sin buffer)")
	return cmd
}

func generar(w io.Writer, o opcionesGenerar) error {
	zona := time.FixedZone("-0300", -3*3600)
	desde, err := time.ParseInLocation(time.DateOnly, o.desde, zona)
	if err != nil {
		return fmt.Errorf("fecha inválida %q: %w", o.desde, err)
	}

	f, err := os.Create(o.salida)
	if err != nil {
		return err
	}
	defer f.Close()

	t0 := time.Now()
	ver, err := loggen.Generar(f, loggen.Opciones{
		Lineas:       o.lineas,
		Semilla:      o.semilla,
		Desde:        desde,
		TasaPorSeg:   o.tasa,
		CantHeavyIPs: o.heavy,
		Zona:         zona,
		TamBuffer:    o.buffer,
	})
	if err != nil {
		return err
	}
	transcurrido := time.Since(t0)
	if err := f.Close(); err != nil {
		return fmt.Errorf("al cerrar %q: %w", o.salida, err)
	}

	if err := escribirJSON(o.verificacion, ver); err != nil {
		return err
	}
	resumenGeneracion(w, o.salida, transcurrido, ver)
	return nil
}

func escribirJSON(ruta string, v any) error {
	f, err := os.Create(ruta)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("al escribir %q: %w", ruta, err)
	}
	return f.Close()
}

func resumenGeneracion(w io.Writer, ruta string, transcurrido time.Duration, ver loggen.Verificacion) {
	var tam int64
	if info, err := os.Stat(ruta); err == nil {
		tam = info.Size()
	}
	seg := transcurrido.Seconds()

	fmt.Fprintf(w, "\nGenerado %q\n", ruta)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  líneas\t%d\n", ver.Lineas)
	fmt.Fprintf(tw, "  tamaño\t%s\n", medicion.Bytes(tam))
	fmt.Fprintf(tw, "  tiempo\t%s  (%.0f líneas/s, %.1f MB/s)\n",
		transcurrido.Round(time.Millisecond), float64(ver.Lineas)/seg, float64(tam)/(1<<20)/seg)
	fmt.Fprintf(tw, "  período\t%s → %s  (%s)\n", ver.Desde.Format(time.DateOnly),
		ver.Hasta.Format(time.DateOnly), ver.Hasta.Sub(ver.Desde).Round(time.Hour))
	fmt.Fprintf(tw, "  hora pico\t%02d:00 h  (%d solicitudes)\n", ver.HoraPico, ver.SolicitudesPorHora[ver.HoraPico])
	tw.Flush()

	fmt.Fprintln(w, "\n  Top 5 endpoints:")
	for _, p := range ver.TopEndpoints[:min(5, len(ver.TopEndpoints))] {
		fmt.Fprintf(w, "    %-24s %d\n", p.Clave, p.Conteo)
	}
	fmt.Fprintln(w, "  Top 5 status:")
	for _, p := range ver.TopEstados[:min(5, len(ver.TopEstados))] {
		fmt.Fprintf(w, "    %-24s %d\n", p.Clave, p.Conteo)
	}
	fmt.Fprintln(w, "  Anomalías inyectadas:")
	for _, a := range ver.Anomalias {
		fmt.Fprintf(w, "    [%s] %s\n", a.Tipo, a.Detalle)
		fmt.Fprintf(w, "      %s → %s", a.Desde.Format(time.DateTime), a.Hasta.Format(time.TimeOnly))
		if a.IP != "" {
			fmt.Fprintf(w, "  ip=%s", a.IP)
		}
		if a.Endpoint != "" {
			fmt.Fprintf(w, "  endpoint=%s", a.Endpoint)
		}
		fmt.Fprintf(w, "  n=%d\n", a.Cantidad)
	}
}
