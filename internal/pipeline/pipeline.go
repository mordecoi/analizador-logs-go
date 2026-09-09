// Package pipeline implementa las formas de recorrer el archivo completo:
//
//   - Secuencial: una sola pasada.
//   - Concurrente: map-reduce — el archivo se parte en bloques alineados a '\n',
//     cada worker parsea y cuenta su bloque en contadores propios (sin ningún
//     estado compartido) y al final los parciales se fusionan.
//   - ConcurrenteMutex: variante deliberadamente mala con un único contador
//     compartido protegido por mutex, para medir cuánto cuesta la contención.
package pipeline

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"

	"parcial1/internal/agregado"
	"parcial1/internal/parse"
)

// Parser convierte una línea de log en una Entrada.
type Parser func(string) (parse.Entrada, error)

// Recorrer procesa el archivo entero secuencialmente entregando cada entrada a
// fn, en orden. Es la base de Secuencial y de las pasadas que dependen del
// orden temporal (ventanas deslizantes de anomalías).
func Recorrer(ruta string, parsear Parser, fn func(parse.Entrada)) (int, error) {
	tam, err := tamanoDe(ruta)
	if err != nil {
		return 0, err
	}
	return procesarBloque(ruta, 0, tam, parsear, fn)
}

// Secuencial procesa el archivo entero en una sola pasada.
func Secuencial(ruta string, parsear Parser) (*agregado.Contadores, int, error) {
	cont := agregado.NuevoContadores()
	errores, err := Recorrer(ruta, parsear, cont.Agregar)
	if err != nil {
		return nil, 0, err
	}
	return cont, errores, nil
}

// Concurrente parte el archivo en `workers` bloques y los procesa en paralelo.
//
// Por qué los bloques son independientes: cada línea pertenece exactamente a un
// bloque (el que contiene su primer byte), y cada worker acumula en contadores
// propios, así que no hay ningún acceso compartido durante el procesamiento.
// La única sincronización es esperar con WaitGroup y fusionar al final:
// O(n·L/P) de trabajo paralelo + O(u·P) de fusión secuencial.
func Concurrente(ruta string, workers int, parsear Parser) (*agregado.Contadores, int, error) {
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	tam, err := tamanoDe(ruta)
	if err != nil {
		return nil, 0, err
	}
	if workers == 1 || tam == 0 {
		return Secuencial(ruta, parsear)
	}

	limites := limitesDeBloques(tam, workers)

	// Cada goroutine escribe solo en su propio índice: no hay carrera.
	parciales := make([]*agregado.Contadores, workers)
	erroresPar := make([]int, workers)
	errsPar := make([]error, workers)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			cont := agregado.NuevoContadores()
			erroresPar[w], errsPar[w] = procesarBloque(ruta, limites[w], limites[w+1], parsear, cont.Agregar)
			parciales[w] = cont
		}(w)
	}
	wg.Wait()

	total := agregado.NuevoContadores()
	errores := 0
	for w := range parciales {
		if errsPar[w] != nil {
			return nil, 0, errsPar[w]
		}
		total.Fusionar(parciales[w])
		errores += erroresPar[w]
	}
	return total, errores, nil
}

// ConcurrenteMutex procesa en paralelo pero acumula en un único contador
// compartido, tomando un mutex por línea. Existe solo para medir la contención:
// serializa el trabajo útil y agrega el costo de adquirir el lock ~n veces.
func ConcurrenteMutex(ruta string, workers int, parsear Parser) (*agregado.Contadores, int, error) {
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	tam, err := tamanoDe(ruta)
	if err != nil {
		return nil, 0, err
	}
	limites := limitesDeBloques(tam, workers)

	var mu sync.Mutex
	compartido := agregado.NuevoContadores()
	agregar := func(e parse.Entrada) {
		mu.Lock()
		compartido.Agregar(e)
		mu.Unlock()
	}

	erroresPar := make([]int, workers)
	errsPar := make([]error, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			erroresPar[w], errsPar[w] = procesarBloque(ruta, limites[w], limites[w+1], parsear, agregar)
		}(w)
	}
	wg.Wait()

	errores := 0
	for w := range erroresPar {
		if errsPar[w] != nil {
			return nil, 0, errsPar[w]
		}
		errores += erroresPar[w]
	}
	return compartido, errores, nil
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

// procesarBloque procesa las líneas cuyo primer byte cae en [desde, hasta).
//
// Alineación: si desde > 0, se arranca un byte antes y se descarta hasta el
// primer '\n' inclusive. Si desde cae justo en un comienzo de línea, ese byte
// anterior ES el '\n' y no se descarta nada; si cae a mitad de una línea, la
// cola descartada la procesa el bloque anterior, que siempre termina completa
// la línea que cruza su límite. Así ninguna línea se pierde ni se cuenta doble.
func procesarBloque(ruta string, desde, hasta int64, parsear Parser, agregar func(parse.Entrada)) (int, error) {
	f, err := os.Open(ruta)
	if err != nil {
		return 0, fmt.Errorf("no se pudo abrir %q: %w", ruta, err)
	}
	defer f.Close()

	pos := desde
	if desde > 0 {
		if _, err := f.Seek(desde-1, io.SeekStart); err != nil {
			return 0, err
		}
	}
	br := bufio.NewReaderSize(f, 1<<20)
	if desde > 0 {
		basura, err := br.ReadString('\n')
		if err == io.EOF {
			return 0, nil // el bloque no contiene ningún comienzo de línea
		}
		if err != nil {
			return 0, err
		}
		pos = desde - 1 + int64(len(basura))
	}

	errores := 0
	for pos < hasta {
		linea, err := br.ReadString('\n')
		if len(linea) > 0 {
			pos += int64(len(linea))
			recorte := strings.TrimRight(linea, "\r\n")
			if e, perr := parsear(recorte); perr != nil {
				errores++
			} else {
				agregar(e)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return errores, err
		}
	}
	return errores, nil
}

func tamanoDe(ruta string) (int64, error) {
	info, err := os.Stat(ruta)
	if err != nil {
		return 0, fmt.Errorf("no se pudo leer %q: %w", ruta, err)
	}
	return info.Size(), nil
}
