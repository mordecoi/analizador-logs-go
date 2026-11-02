// Package medicion mide tiempo y memoria de una operación y formatea tamaños.
//
// Reúne lo que en el primer parcial estaba repetido: el bloque de
// runtime.ReadMemStats antes y después (copiado en el análisis y en la carga
// del cache) y tres funciones distintas para mostrar bytes (una por comando).
package medicion

import (
	"fmt"
	"runtime"
	"time"
)

// Medicion junta lo que se reporta de rendimiento de una operación.
type Medicion struct {
	Transcurrido time.Duration
	HeapFinal    uint64 // memoria retenida tras la operación (lo que quedó vivo)
	TotalAsign   uint64 // memoria total asignada durante la operación (presión de GC)
	Asignaciones uint64 // cantidad de asignaciones de heap
	CiclosGC     uint32
}

// Medir ejecuta fn y devuelve su costo. Fuerza un GC antes y después para que
// HeapFinal refleje solo lo que fn dejó vivo, no basura pendiente de recolectar.
func Medir(fn func() error) (Medicion, error) {
	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	t0 := time.Now()

	err := fn()

	transcurrido := time.Since(t0)
	runtime.GC()
	runtime.ReadMemStats(&m1)
	return Medicion{
		Transcurrido: transcurrido,
		HeapFinal:    m1.HeapAlloc,
		TotalAsign:   m1.TotalAlloc - m0.TotalAlloc,
		Asignaciones: m1.Mallocs - m0.Mallocs,
		CiclosGC:     m1.NumGC - m0.NumGC,
	}, err
}

// Bytes muestra un tamaño en la unidad más legible (B, KB, MB o GB).
func Bytes[T int64 | uint64 | int](b T) string {
	v := float64(b)
	switch {
	case v < 1<<10:
		return fmt.Sprintf("%d B", int64(b))
	case v < 1<<20:
		return fmt.Sprintf("%.1f KB", v/(1<<10))
	case v < 1<<30:
		return fmt.Sprintf("%.1f MB", v/(1<<20))
	default:
		return fmt.Sprintf("%.2f GB", v/(1<<30))
	}
}

// MB expresa b en mebibytes, para columnas numéricas (CSV).
func MB(b uint64) float64 { return float64(b) / (1 << 20) }

func sinUso() {}
