// Package pipeline implementa las formas de recorrer el archivo completo:
//
//   - Secuencial: una sola pasada.
//   - Concurrente: map-reduce — el archivo se parte en bloques alineados a '\n',
//     cada worker parsea y cuenta su bloque en contadores propios (sin ningún
//     estado compartido) y al final los parciales se fusionan.
//   - ConcurrenteMutex: variante deliberadamente mala con un único contador
//     compartido protegido por mutex, para medir cuánto cuesta la contención.
//   - DesdeLector: productor-consumidor para entradas que no se pueden partir
//     por offsets (stdin, un .gz): un lector corta lotes y N workers los parsean.
//
// Todas leen por lotes (lotes.go): bloques de TamLotePorDefecto bytes en un
// buffer reutilizado, con cada línea entregada como vista sin copia. Contrato:
// los strings de una parse.Entrada solo son válidos durante la llamada al
// callback que la recibe; quien los retenga debe clonarlos.
//
// Cada recorrido abre spans de OpenTelemetry (uno por bloque y uno por la
// fusión). Sin un TracerProvider configurado son no-ops de costo despreciable.
package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"parcial1/internal/agregado"
	"parcial1/internal/parse"
)

// Parser convierte una línea de log en una Entrada.
type Parser func(string) (parse.Entrada, error)

var tracer = otel.Tracer("parcial1/internal/pipeline")

// Recorrer procesa el archivo entero secuencialmente entregando cada entrada a
// fn, en orden. Es la base de Secuencial y de las pasadas que dependen del
// orden temporal (ventanas deslizantes de anomalías). Devuelve la cantidad de
// líneas no reconocidas.
func Recorrer(ctx context.Context, ruta string, parsear Parser, fn func(parse.Entrada)) (int, error) {
	tam, err := tamanoDe(ruta)
	if err != nil {
		return 0, err
	}
	return procesarBloque(ctx, ruta, 0, tam, parsear, fn, TamLotePorDefecto)
}

// Secuencial procesa el archivo entero en una sola pasada.
func Secuencial(ctx context.Context, ruta string, parsear Parser) (*agregado.Contadores, int, error) {
	cont := agregado.NuevoContadores()
	errores, err := Recorrer(ctx, ruta, parsear, cont.Agregar)
	if err != nil {
		return nil, 0, err
	}
	return cont, errores, nil
}

// Concurrente parte el archivo en `workers` bloques y los procesa en paralelo
// (workers <= 0 usa GOMAXPROCS).
//
// Por qué los bloques son independientes: cada línea pertenece exactamente a un
// bloque (el que contiene su primer byte), y cada worker acumula en contadores
// propios, así que no hay ningún acceso compartido durante el procesamiento.
// La única sincronización es esperar con WaitGroup y fusionar al final:
// O(n·L/P) de trabajo paralelo + O(u·P) de fusión secuencial.
func Concurrente(ctx context.Context, ruta string, workers int, parsear Parser) (*agregado.Contadores, int, error) {
	workers = normalizarWorkers(workers)
	if workers == 1 {
		return Secuencial(ctx, ruta, parsear)
	}

	// Cada goroutine escribe solo en su propio índice: no hay carrera.
	parciales := make([]*agregado.Contadores, workers)
	for w := range parciales {
		parciales[w] = agregado.NuevoContadoresParticionados(workers)
	}
	errores, err := enBloques(ctx, ruta, workers, parsear, func(w int) func(parse.Entrada) {
		return parciales[w].Agregar
	})
	if err != nil {
		return nil, 0, err
	}
	return fusionar(ctx, parciales), errores, nil
}

// ConcurrenteMutex procesa en paralelo pero acumula en un único contador
// compartido, tomando un mutex por línea. Existe solo para medir la contención:
// serializa el trabajo útil y agrega el costo de adquirir el lock ~n veces.
func ConcurrenteMutex(ctx context.Context, ruta string, workers int, parsear Parser) (*agregado.Contadores, int, error) {
	workers = normalizarWorkers(workers)
	var mu sync.Mutex
	compartido := agregado.NuevoContadores()
	agregar := func(e parse.Entrada) {
		mu.Lock()
		compartido.Agregar(e)
		mu.Unlock()
	}
	errores, err := enBloques(ctx, ruta, workers, parsear, func(int) func(parse.Entrada) {
		return agregar
	})
	if err != nil {
		return nil, 0, err
	}
	return compartido, errores, nil
}

// enBloques es el fan-out común a Concurrente y ConcurrenteMutex: parte el
// archivo en workers bloques y procesa cada uno en su goroutine, entregando las
// entradas del bloque w a agregarDe(w). Devuelve el total de líneas no
// reconocidas, o el primer error de E/S.
func enBloques(ctx context.Context, ruta string, workers int, parsear Parser,
	agregarDe func(w int) func(parse.Entrada)) (int, error) {
	tam, err := tamanoDe(ruta)
	if err != nil {
		return 0, err
	}
	limites := limitesDeBloques(tam, workers)

	erroresPar := make([]int, workers)
	errsPar := make([]error, workers)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			erroresPar[w], errsPar[w] = procesarBloque(ctx, ruta, limites[w], limites[w+1],
				parsear, agregarDe(w), TamLotePorDefecto)
		})
	}
	wg.Wait()

	errores := 0
	for w := range workers {
		if errsPar[w] != nil {
			return 0, errsPar[w]
		}
		errores += erroresPar[w]
	}
	return errores, nil
}

// fusionar es la fase reduce: suma todos los parciales, también en paralelo
// (una goroutine por partición de claves, ver agregado.FusionarParalelo).
func fusionar(ctx context.Context, parciales []*agregado.Contadores) *agregado.Contadores {
	_, span := tracer.Start(ctx, "fusionar", trace.WithAttributes(attribute.Int("parciales", len(parciales))))
	defer span.End()
	return agregado.FusionarParalelo(parciales, len(parciales))
}

// DesdeLector procesa una entrada no posicionable (stdin, un archivo
// comprimido) con un productor-consumidor: un único lector corta la entrada en
// lotes de líneas completas y workers goroutines los parsean, cada una en sus
// propios contadores. Los buffers circulan por un pool (el canal libres), así
// que la memoria queda acotada a 2·workers lotes sin importar el tamaño de la
// entrada.
//
// A diferencia de Concurrente, la lectura es secuencial: conviene cuando leer
// es barato frente a parsear o cuando no hay otra opción (descomprimir gzip es
// inherentemente secuencial, pero el parseo sí se reparte).
func DesdeLector(ctx context.Context, r io.Reader, workers int, parsear Parser) (*agregado.Contadores, int, error) {
	return desdeLector(ctx, r, normalizarWorkers(workers), parsear, TamLotePorDefecto)
}

// lote es un trozo de líneas completas y el buffer que lo contiene, para
// devolverlo al pool cuando el worker termina.
type lote struct {
	datos []byte
	buf   []byte
}

func desdeLector(ctx context.Context, r io.Reader, workers int, parsear Parser, tamLote int) (*agregado.Contadores, int, error) {
	libres := make(chan []byte, 2*workers)
	for range 2 * workers {
		libres <- make([]byte, tamLote)
	}
	lotes := make(chan lote, workers)

	parciales := make([]*agregado.Contadores, workers)
	erroresPar := make([]int, workers)
	var wg sync.WaitGroup
	for w := range workers {
		parciales[w] = agregado.NuevoContadoresParticionados(workers)
		wg.Go(func() {
			for l := range lotes {
				_, e, _ := procesarLote(l.datos, 0, int64(len(l.datos)), parsear, parciales[w].Agregar)
				erroresPar[w] += e
				libres <- l.buf
			}
		})
	}

	errLectura := producirLotes(ctx, r, libres, lotes)
	close(lotes)
	wg.Wait()
	if errLectura != nil {
		return nil, 0, errLectura
	}

	errores := 0
	for _, e := range erroresPar {
		errores += e
	}
	return fusionar(ctx, parciales), errores, nil
}

// producirLotes es el lado productor de DesdeLector. La cola incompleta de cada
// lote se copia al buffer siguiente antes de enviar el actual: el worker solo
// lee la parte de líneas completas, así que no hay carrera sobre la cola.
func producirLotes(ctx context.Context, r io.Reader, libres chan []byte, lotes chan<- lote) error {
	_, span := tracer.Start(ctx, "leer-lotes")
	defer span.End()

	var cola []byte
	cantidad := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		buf := <-libres
		n := copy(buf, cola)
		datos, c, buf, err := leerLote(r, buf, n)
		cola = c
		if len(datos) > 0 {
			lotes <- lote{datos: datos, buf: buf}
			cantidad++
		} else {
			libres <- buf
		}
		if err == io.EOF {
			span.SetAttributes(attribute.Int("lotes", cantidad))
			return nil
		}
		if err != nil {
			return fmt.Errorf("al leer: %w", err)
		}
	}
}

func normalizarWorkers(workers int) int {
	if workers <= 0 {
		return runtime.GOMAXPROCS(0)
	}
	return workers
}

// limitesDeBloques devuelve workers+1 offsets que parten [0, tam) en bloques
// de tamaño casi igual. Los límites caen en bytes arbitrarios; procesarBloque
// se encarga de alinearlos a comienzos de línea.
func limitesDeBloques(tam int64, workers int) []int64 {
	limites := make([]int64, workers+1)
	for i := 1; i < workers; i++ {
		limites[i] = tam * int64(i) / int64(workers)
	}
	limites[workers] = tam
	return limites
}

// procesarBloque procesa las líneas cuyo primer byte cae en [desde, hasta),
// leyendo por lotes de tamLote bytes.
//
// Alineación: si desde > 0, se arranca un byte antes y se descarta hasta el
// primer '\n' inclusive. Si desde cae justo en un comienzo de línea, ese byte
// anterior ES el '\n' y no se descarta nada; si cae a mitad de una línea, la
// cola descartada la procesa el bloque anterior, que siempre termina completa
// la línea que cruza su límite. Así ninguna línea se pierde ni se cuenta doble.
func procesarBloque(ctx context.Context, ruta string, desde, hasta int64, parsear Parser,
	agregar func(parse.Entrada), tamLote int) (int, error) {
	_, span := tracer.Start(ctx, "bloque", trace.WithAttributes(
		attribute.Int64("desde", desde), attribute.Int64("hasta", hasta)))
	defer span.End()

	f, err := os.Open(ruta)
	if err != nil {
		return 0, fmt.Errorf("no se pudo abrir %q: %w", ruta, err)
	}
	defer f.Close()

	offset := desde // offset en el archivo del primer byte del próximo lote
	if desde > 0 {
		offset = desde - 1
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return 0, err
		}
	}
	lineas, errores, err := recorrer(f, offset, hasta, desde > 0, parsear, agregar, tamLote)
	span.SetAttributes(attribute.Int("lineas", lineas), attribute.Int("errores", errores))
	return errores, err
}

// RecorrerLector es Recorrer sobre cualquier io.Reader (stdin, un .gz): una
// pasada secuencial que entrega cada entrada a fn, en orden.
func RecorrerLector(ctx context.Context, r io.Reader, parsear Parser, fn func(parse.Entrada)) (int, error) {
	_, span := tracer.Start(ctx, "recorrer")
	defer span.End()
	_, errores, err := recorrer(r, 0, math.MaxInt64, false, parsear, fn, TamLotePorDefecto)
	return errores, err
}

// recorrer lee r por lotes y procesa las líneas que empiezan antes de hasta.
// offset es la posición en el archivo del primer byte de r; con alinear se
// descarta todo hasta el primer '\n' inclusive (ver procesarBloque).
func recorrer(r io.Reader, offset, hasta int64, alinear bool, parsear Parser,
	agregar func(parse.Entrada), tamLote int) (lineas, errores int, err error) {
	lector := nuevoLectorLotes(r, tamLote)
	for {
		lt, err := lector.siguiente()
		if err == io.EOF {
			return lineas, errores, nil
		}
		if err != nil {
			return lineas, errores, err
		}
		if alinear {
			alinear = false
			i := bytes.IndexByte(lt, '\n')
			if i < 0 {
				return 0, 0, nil // el bloque no contiene ningún comienzo de línea
			}
			offset += int64(i + 1)
			lt = lt[i+1:]
		}
		l, e, terminado := procesarLote(lt, offset, hasta, parsear, agregar)
		lineas += l
		errores += e
		if terminado {
			return lineas, errores, nil
		}
		offset += int64(len(lt))
	}
}

func tamanoDe(ruta string) (int64, error) {
	info, err := os.Stat(ruta)
	if err != nil {
		return 0, fmt.Errorf("no se pudo leer %q: %w", ruta, err)
	}
	return info.Size(), nil
}
