package agregado_test

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"parcial1/internal/agregado"
)

// mapaSesgado arma un mapa de u claves con conteos tipo Zipf (pocas claves con
// conteo alto, cola larga cerca de 1) — la forma real de los datos del log.
func mapaSesgado(u int) map[int]int {
	r := rand.New(rand.NewPCG(1, 2)) // semilla fija: corridas reproducibles
	m := make(map[int]int, u)
	for i := 0; i < u; i++ {
		m[i] = 1 + int(1e6/float64(i+1)) + r.IntN(3)
	}
	return m
}

var casosTopN = []struct{ u, k int }{
	{15, 10},         // status: u chico
	{40, 10},         // endpoints: u chico
	{100_000, 10},    // IPs, escala intermedia
	{1_200_000, 10},  // IPs, escala ~1 M de líneas
	{1_200_000, 100}, // mismo u, k más grande
	{1_200_000, 1000},
	{20_000, 10_000}, // k ≈ u/2 → debería ganar sort
	{20_000, 18_000}, // k ≈ u → gana sort
}

func BenchmarkTopN(b *testing.B) {
	impls := []struct {
		nombre string
		fn     func(agregado.Fuente[int], int) []agregado.Par[int]
	}{
		{"sort", agregado.TopNSort[int]},
		{"heap", agregado.TopNHeap[int]},
	}
	for _, c := range casosTopN {
		m := mapaSesgado(c.u)
		for _, impl := range impls {
			b.Run(fmt.Sprintf("%s/u=%d/k=%d", impl.nombre, c.u, c.k), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					_ = impl.fn(agregado.Mapa[int](m), c.k)
				}
			})
		}
	}
}
