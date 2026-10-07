package cli

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"

	"parcial1/internal/agregado"
	"parcial1/internal/anomalias"
	"parcial1/internal/cache"
	"parcial1/internal/medicion"
	"parcial1/internal/parse"
	"parcial1/internal/pipeline"
	"parcial1/internal/reporte"
)

var tracer = otel.Tracer("parcial1/internal/cli")

// opcionesAnalisis son los flags del subcomando analizar.
type opcionesAnalisis struct {
	archivo      string
	topn         int
	topnMetodo   string
	parser       string
	modo         string
	workers      int
	cache        bool
	anomalias    bool
	verificacion string
	csv          string
	version      string
	cpuprofile   string
	memprofile   string
	traza        string
}

// Valores admitidos por los flags de elección.
var (
	metodosTopN = []string{"heap", "sort"}
	parsers     = []string{"manual", "regex"}
	modos       = []string{"conc", "seq", "conc-mutex"}
)

func comandoAnalizar() *cobra.Command {
	var o opcionesAnalisis
	cmd := &cobra.Command{
		Use:   "analizar",
		Short: "Analiza un log y reporta rankings, horas pico y anomalías",
		Long: `Analiza un log en formato Apache Combined.

La entrada puede ser un archivo (se parte en bloques que se procesan en
paralelo), un archivo .gz o "-" para leer de la entrada estándar (en estos dos
casos un lector corta lotes y varios workers los parsean).`,
		Example: `  analizador analizar -a logfiles.log
  analizador analizar -a logfiles.log --verificacion ground-truth.json --anomalias
  analizador analizar -a logfiles.log --parser regex --modo seq --topn-metodo sort
  zcat logs.gz | analizador analizar -a -`,
		Args: cobra.NoArgs,
		PreRunE: func(*cobra.Command, []string) error {
			return o.validar()
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return analizar(cmd.Context(), cmd.OutOrStdout(), cmd.InOrStdin(), o)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.archivo, "archivo", "a", "logfiles.log", `archivo de log a analizar (.gz admitido; "-" = stdin)`)
	f.IntVarP(&o.topn, "topn", "n", 10, "cantidad de resultados en cada ranking")
	f.StringVar(&o.topnMetodo, "topn-metodo", "heap", "estrategia de Top-N: "+strings.Join(metodosTopN, " | "))
	f.StringVar(&o.parser, "parser", "manual", "parser de líneas: "+strings.Join(parsers, " | "))
	f.StringVar(&o.modo, "modo", "conc", "procesamiento: "+strings.Join(modos, " | "))
	f.IntVarP(&o.workers, "workers", "w", 0, "workers del modo concurrente (0 = GOMAXPROCS)")
	f.BoolVar(&o.cache, "cache", false, "memoizar los agregados a disco y reusarlos si el log no cambió")
	f.BoolVar(&o.anomalias, "anomalias", false, "detectar patrones anómalos (pasada secuencial extra)")
	f.StringVar(&o.verificacion, "verificacion", "", "ground-truth.json para validar los resultados")
	f.StringVar(&o.csv, "csv", "", "CSV donde agregar la fila de medición")
	f.StringVar(&o.version, "version-etiqueta", "final", "etiqueta de la versión en el CSV")
	f.StringVar(&o.cpuprofile, "cpuprofile", "", "escribir perfil de CPU (pprof) a este archivo")
	f.StringVar(&o.memprofile, "memprofile", "", "escribir perfil de heap (pprof) a este archivo")
	f.StringVar(&o.traza, "trace", "", "escribir traza de ejecución (go tool trace) a este archivo")
	return cmd
}

// validar rechaza valores fuera de los admitidos. En el primer parcial un
// valor mal escrito caía en silencio en la opción por defecto.
func (o opcionesAnalisis) validar() error {
	elegir := func(flag, valor string, admitidos []string) error {
		if !slices.Contains(admitidos, valor) {
			return fmt.Errorf("--%s %q inválido: debe ser %s", flag, valor, strings.Join(admitidos, " | "))
		}
		return nil
	}
	if err := elegir("topn-metodo", o.topnMetodo, metodosTopN); err != nil {
		return err
	}
	if err := elegir("parser", o.parser, parsers); err != nil {
		return err
	}
	if err := elegir("modo", o.modo, modos); err != nil {
		return err
	}
	if o.archivo == "-" && (o.cache || o.anomalias) {
		return fmt.Errorf("--cache y --anomalias necesitan releer la entrada: no se pueden usar con stdin")
	}
	return nil
}

func (o opcionesAnalisis) parsear() pipeline.Parser {
	if o.parser == "regex" {
		return parse.Parsear
	}
	return parse.ParsearManual
}

func (o opcionesAnalisis) metodoTopN() agregado.Metodo {
	if o.topnMetodo == "sort" {
		return agregado.MetodoSort
	}
	return agregado.MetodoHeap
}

func analizar(ctx context.Context, w io.Writer, stdin io.Reader, o opcionesAnalisis) (err error) {
	detener, err := iniciarPerfiles(o.cpuprofile, o.traza)
	if err != nil {
		return err
	}
	defer detener()

	ctx, span := tracer.Start(ctx, "analizar", oteltrace.WithAttributes(
		attribute.String("archivo", o.archivo), attribute.String("modo", o.modo),
		attribute.String("parser", o.parser), attribute.Int("workers", o.workers)))
	defer span.End()

	cont, corrida, err := obtenerContadores(ctx, w, stdin, o)
	if err != nil {
		return err
	}
	span.SetAttributes(attribute.Int("lineas", cont.Total))

	q := cache.NuevasConsultas(cont, o.metodoTopN())
	reporte.Resumen(w, q, corrida, o.topn)

	validado := true
	if o.verificacion != "" {
		ver, err := reporte.LeerVerificacion(o.verificacion)
		if err != nil {
			return err
		}
		validado = reporte.Validar(w, q, ver, o.topn)
	}
	fmt.Fprintf(w, "\nCache de consultas Top-N: %d hits, %d misses\n", q.Hits, q.Misses)

	if o.anomalias {
		if err := detectarAnomalias(ctx, w, o); err != nil {
			return err
		}
	}
	if o.csv != "" {
		if err := reporte.AgregarCSV(o.csv, o.version, o.archivo, cont.Total, corrida.Medicion); err != nil {
			return err
		}
	}
	if o.memprofile != "" {
		if err := escribirPerfilMemoria(o.memprofile); err != nil {
			return err
		}
		// sin esto, el compilador da por muertos los contadores antes del GC
		// de escribirPerfilMemoria y el perfil "inuse" sale vacío.
		runtime.KeepAlive(cont)
	}
	if !validado {
		return ErrValidacion
	}
	return nil
}

// obtenerContadores trae los agregados del cache (si --cache y el log no
// cambió) o procesa la entrada, midiendo el costo en ambos casos.
func obtenerContadores(ctx context.Context, w io.Writer, stdin io.Reader, o opcionesAnalisis) (*agregado.Contadores, reporte.Corrida, error) {
	var cont *agregado.Contadores
	if o.cache {
		var motivo string
		med, _ := medicion.Medir(func() error {
			cont, motivo = cache.Cargar(o.archivo)
			return nil
		})
		if cont != nil {
			return cont, reporte.Corrida{Medicion: med, DesdeCache: true}, nil
		}
		fmt.Fprintf(w, "Cache de agregados: miss (%s); se procesa el archivo.\n", motivo)
	}

	var errores int
	med, err := medicion.Medir(func() error {
		var err error
		cont, errores, err = procesar(ctx, stdin, o)
		return err
	})
	if err != nil {
		return nil, reporte.Corrida{}, err
	}
	if o.cache {
		if err := cache.Guardar(o.archivo, cont); err != nil {
			fmt.Fprintln(w, err) // no poder memoizar no invalida el análisis
		}
	}
	return cont, reporte.Corrida{Medicion: med, Errores: errores}, nil
}

// procesar elige la estrategia según la entrada y el modo: los archivos
// planos se parten por offsets; stdin y .gz pasan por el productor-consumidor.
func procesar(ctx context.Context, stdin io.Reader, o opcionesAnalisis) (*agregado.Contadores, int, error) {
	ctx, span := tracer.Start(ctx, "procesar")
	defer span.End()

	if o.archivo == "-" || esGzip(o.archivo) {
		r, cerrar, err := abrirSecuencial(o.archivo, stdin)
		if err != nil {
			return nil, 0, err
		}
		defer cerrar()
		return pipeline.DesdeLector(ctx, r, o.workers, o.parsear())
	}
	switch o.modo {
	case "seq":
		return pipeline.Secuencial(ctx, o.archivo, o.parsear())
	case "conc-mutex":
		return pipeline.ConcurrenteMutex(ctx, o.archivo, o.workers, o.parsear())
	default:
		return pipeline.Concurrente(ctx, o.archivo, o.workers, o.parsear())
	}
}

func esGzip(ruta string) bool { return strings.HasSuffix(ruta, ".gz") }

// abrirSecuencial abre la entrada como un io.Reader de lectura secuencial,
// descomprimiendo si es un .gz.
func abrirSecuencial(ruta string, stdin io.Reader) (io.Reader, func(), error) {
	if ruta == "-" {
		return stdin, func() {}, nil
	}
	f, err := os.Open(ruta)
	if err != nil {
		return nil, nil, err
	}
	if !esGzip(ruta) {
		return f, func() { f.Close() }, nil
	}
	zr, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("%s: %w", ruta, err)
	}
	return zr, func() { zr.Close(); f.Close() }, nil
}

// detectarAnomalias hace la pasada secuencial extra con los detectores de
// ventana (necesitan las entradas en orden temporal, por eso no va dentro del
// map-reduce) y, si hay ground truth, calcula precisión y recall.
func detectarAnomalias(ctx context.Context, w io.Writer, o opcionesAnalisis) error {
	ctx, span := tracer.Start(ctx, "anomalias")
	defer span.End()

	r, cerrar, err := abrirSecuencial(o.archivo, nil)
	if err != nil {
		return err
	}
	defer cerrar()

	d := anomalias.NuevoDetector(anomalias.ConfigPorDefecto())
	t0 := time.Now()
	if _, err := pipeline.RecorrerLector(ctx, r, o.parsear(), d.Procesar); err != nil {
		return err
	}
	hs := d.Hallazgos()
	span.SetAttributes(attribute.Int("hallazgos", len(hs)))
	reporte.Anomalias(w, hs, time.Since(t0))

	if o.verificacion == "" {
		return nil
	}
	ver, err := reporte.LeerVerificacion(o.verificacion)
	if err != nil {
		return err
	}
	reporte.EvaluarAnomalias(w, hs, ver)
	return nil
}

// iniciarPerfiles arranca el perfil de CPU y la traza de ejecución pedidos y
// devuelve la función que los detiene y cierra sus archivos.
func iniciarPerfiles(cpuprofile, traza string) (func(), error) {
	var detener []func()
	parar := func() {
		for _, d := range slices.Backward(detener) {
			d()
		}
	}
	arrancar := func(ruta string, iniciar func(io.Writer) error, finalizar func()) error {
		if ruta == "" {
			return nil
		}
		f, err := os.Create(ruta)
		if err != nil {
			return err
		}
		if err := iniciar(f); err != nil {
			f.Close()
			return err
		}
		detener = append(detener, func() { finalizar(); f.Close() })
		return nil
	}
	if err := arrancar(cpuprofile, pprof.StartCPUProfile, pprof.StopCPUProfile); err != nil {
		parar()
		return nil, err
	}
	if err := arrancar(traza, trace.Start, trace.Stop); err != nil {
		parar()
		return nil, err
	}
	return parar, nil
}

func escribirPerfilMemoria(ruta string) error {
	f, err := os.Create(ruta)
	if err != nil {
		return err
	}
	defer f.Close()
	runtime.GC()
	if err := pprof.WriteHeapProfile(f); err != nil {
		return err
	}
	return f.Close()
}
