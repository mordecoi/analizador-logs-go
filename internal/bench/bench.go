// Package bench agrupa las utilidades comunes a los ejercicios del práctico:
// generación de datos, medición de tiempos y formateo de resultados.
package bench

import (
	"fmt"
	"math"
	"math/rand/v2"
	"runtime"
	"testing"
	"time"
)

// CantObjetivos es potencia de dos, así se puede indexar con máscara
// en vez de módulo (una operación menos dentro del bucle medido).
const CantObjetivos = 1024

// MascaraObjetivos se usa como: objetivos[i & bench.MascaraObjetivos]
const MascaraObjetivos = CantObjetivos - 1

// NuevoRand devuelve una fuente con semilla fija, para que las
// corridas sean reproducibles.
func NuevoRand() *rand.Rand {
	return rand.New(rand.NewPCG(1, 2))
}

// GenerarDatos devuelve una permutación aleatoria de 0..n-1.
func GenerarDatos(r *rand.Rand, n int) []int {
	return r.Perm(n)
}

// GenerarObjetivos arma un conjunto de valores a buscar dentro del rango,
// para medir el caso promedio y no una única posición arbitraria.
func GenerarObjetivos(r *rand.Rand, n int) []int {
	obj := make([]int, CantObjetivos)
	for i := range obj {
		obj[i] = r.IntN(n)
	}
	return obj
}

// Medir corre fn con el motor de benchmarks de Go y devuelve los
// nanosegundos promedio por operación. El parámetro i es el número
// de iteración, útil para variar el dato de entrada en cada vuelta.
func Medir(fn func(i int)) float64 {
	runtime.GC()
	res := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			fn(i)
		}
	})
	return float64(res.NsPerOp())
}

// Formatear muestra los nanosegundos en la unidad más legible.
func Formatear(ns float64) string {
	return time.Duration(ns).String()
}

// Comparar arma una línea con la diferencia entre dos mediciones,
// en valor absoluto y en porcentaje relativo a la primera.
func Comparar(etiqueta string, manual, nativa float64) string {
	dif := nativa - manual
	var pct float64
	if manual != 0 {
		pct = dif / manual * 100
	}
	signo := "+"
	if dif < 0 {
		signo = "-"
	}
	return fmt.Sprintf("%s: manual %s vs nativa %s  →  %s%s (%s%.2f%%)",
		etiqueta,
		Formatear(manual),
		Formatear(nativa),
		signo, Formatear(math.Abs(dif)),
		signo, math.Abs(pct))
}

// GenerarConRepetidos devuelve n valores en el rango [0, maximo).
// Si maximo < n, las repeticiones aparecen por fuerza.
func GenerarConRepetidos(r *rand.Rand, n, maximo int) []int {
	s := make([]int, n)
	for i := range s {
		s[i] = r.IntN(maximo)
	}
	return s
}

// MedirOrdenamiento mide una función que ordena en el lugar. Restaura
// los datos antes de cada iteración con el cronómetro pausado, para que
// el costo de copiar no se cuente y para que el algoritmo no reciba un
// arreglo ya ordenado a partir de la segunda vuelta.
func MedirOrdenamiento(fn func([]int), datos []int) float64 {
	buffer := make([]int, len(datos))
	runtime.GC()
	res := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			copy(buffer, datos)
			b.StartTimer()
			fn(buffer)
		}
	})
	return float64(res.NsPerOp())
}

// GenerarCasiOrdenado devuelve 0..n-1 ordenado, con algunos pares
// intercambiados al azar. Sirve para exponer el mejor caso de los
// algoritmos adaptativos.
func GenerarCasiOrdenado(r *rand.Rand, n, desordenes int) []int {
	s := make([]int, n)
	for i := range s {
		s[i] = i
	}
	for k := 0; k < desordenes; k++ {
		i, j := r.IntN(n), r.IntN(n)
		s[i], s[j] = s[j], s[i]
	}
	return s
}

// GenerarInverso devuelve n-1..0, el peor caso de los algoritmos
// cuadráticos: cada elemento tiene que recorrer todo el arreglo.
func GenerarInverso(n int) []int {
	s := make([]int, n)
	for i := range s {
		s[i] = n - 1 - i
	}
	return s
}

// MedirSimple adapta Medir a funciones que no dependen del número de iteración,
// como la construcción de una estructura auxiliar.
func MedirSimple(fn func()) float64 {
	return Medir(func(int) { fn() })
}

// MedirMemoria informa cuánta memoria queda retenida por la estructura que
// devuelve construir. runtime.GC descarta lo temporal antes de cada lectura y
// KeepAlive impide que el recolector libere el objeto antes de la segunda. Solo
// es confiable para estructuras grandes: por debajo del megabyte el ruido del
// heap supera a lo que se quiere medir, y conviene calcular el tamaño estructural
// exacto en su lugar.
func MedirMemoria(construir func() any) uint64 {
	runtime.GC()
	var antes, despues runtime.MemStats
	runtime.ReadMemStats(&antes)

	obj := construir()

	runtime.GC()
	runtime.ReadMemStats(&despues)
	runtime.KeepAlive(obj)

	if despues.HeapAlloc < antes.HeapAlloc {
		return 0
	}
	return despues.HeapAlloc - antes.HeapAlloc
}

// MedirFijo corre fn exactamente iteraciones veces y devuelve el promedio en
// nanosegundos y la cantidad de asignaciones de heap por iteración. Para
// workloads que no se pueden dejar crecer libremente —una cola que acumula, una
// estructura que se vacía—, donde el b.N que elegiría testing.Benchmark sería
// impredecible y distorsionaría la medición.
func MedirFijo(iteraciones int, fn func(i int)) (nsOp, allocsOp float64) {
	runtime.GC()
	var antes, despues runtime.MemStats
	runtime.ReadMemStats(&antes)

	inicio := time.Now()
	for i := 0; i < iteraciones; i++ {
		fn(i)
	}
	transcurrido := time.Since(inicio)

	runtime.ReadMemStats(&despues)
	ns := float64(transcurrido) / float64(iteraciones)
	allocs := float64(despues.Mallocs-antes.Mallocs) / float64(iteraciones)
	return ns, allocs
}

// FormatearBytes muestra un tamaño en memoria en la unidad más legible.
func FormatearBytes(bytes uint64) string {
	switch {
	case bytes < 1<<10:
		return fmt.Sprintf("%d B", bytes)
	case bytes < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(bytes)/(1<<10))
	default:
		return fmt.Sprintf("%.2f MB", float64(bytes)/(1<<20))
	}
}
