package parse_test

import (
	"bufio"
	"bytes"
	"testing"

	"parcial1/internal/loggen"
	"parcial1/internal/parse"
)

// lineasDePrueba genera un log y devuelve sus líneas como slice, para medir el
// parseo sobre datos representativos (todas las categorías, con y sin usuario).
func lineasDePrueba(tb testing.TB, n int) []string {
	tb.Helper()
	var buf bytes.Buffer
	if _, err := loggen.Generar(&buf, loggen.Opciones{Lineas: n, Semilla: 1}); err != nil {
		tb.Fatal(err)
	}
	var ls []string
	sc := bufio.NewScanner(&buf)
	for sc.Scan() {
		ls = append(ls, sc.Text())
	}
	return ls
}

func BenchmarkParsear(b *testing.B) {
	lineas := lineasDePrueba(b, 5000)
	impls := []struct {
		nombre string
		fn     func(string) (parse.Entrada, error)
	}{
		{"regex", parse.Parsear},
		{"manual", parse.ParsearManual},
	}
	for _, impl := range impls {
		b.Run(impl.nombre, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(lineas[0]))) // referencia aproximada
			for i := 0; i < b.N; i++ {
				e, err := impl.fn(lineas[i%len(lineas)])
				if err != nil {
					b.Fatal(err)
				}
				_ = e
			}
		})
	}
}
