package loggen

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// lineasBench es lo que escribe cada iteración de BenchmarkEscrituraBuffer:
// ~9 MB, suficiente para que el costo de las llamadas al sistema domine.
const lineasBench = 50_000

// BenchmarkEscrituraBuffer mide la escritura de un log a un archivo real con
// distintos tamaños de buffer. Sin buffer cada línea es una llamada a Write del
// sistema operativo; con buffer, una llamada cada tamBuffer/180 líneas.
//
//	go test ./internal/loggen -run '^$' -bench EscrituraBuffer -benchmem
func BenchmarkEscrituraBuffer(b *testing.B) {
	casos := []struct {
		nombre string
		tam    int
	}{
		{"sin-buffer", SinBuffer},
		{"4KiB", 4 << 10},
		{"64KiB", 64 << 10},
		{"1MiB", 1 << 20},
		{"8MiB", 8 << 20},
	}
	for _, t := range casos {
		b.Run(t.nombre, func(b *testing.B) {
			ruta := filepath.Join(b.TempDir(), "bench.log")
			for b.Loop() {
				f, err := os.Create(ruta)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := Generar(f, Opciones{Lineas: lineasBench, TamBuffer: t.tam}); err != nil {
					b.Fatal(err)
				}
				if err := f.Close(); err != nil {
					b.Fatal(err)
				}
			}
			info, _ := os.Stat(ruta)
			b.SetBytes(info.Size())
		})
	}
}

// escribirConFprintf es la versión original del formateo de líneas, que se
// conserva solo como referencia para el benchmark.
func (e *evento) escribirConFprintf(w io.Writer) {
	fmt.Fprintf(w, "%s - %s [%s] \"%s %s %s\" %s %d %q %q %d\n",
		e.ip, e.usuario, e.t.Format(layoutApache),
		e.metodo, e.ruta, e.proto,
		e.estado, e.bytes, e.referrer, e.ua, e.respMs)
}

// BenchmarkFormatoLinea compara el formateo de una línea con fmt.Fprintf
// contra agregarLinea (strconv.Append* sobre un buffer reutilizado).
func BenchmarkFormatoLinea(b *testing.B) {
	opts := completarOpciones(Opciones{Lineas: 1})
	r := nuevoRand(0)
	base := nuevaCorrienteBase(r, opts, nuevosMuestreadores(r, opts))
	eventos := make([]evento, 1024)
	for i := range eventos {
		eventos[i] = base.siguiente()
	}
	bw := bufio.NewWriterSize(io.Discard, 1<<20)

	b.Run("fprintf", func(b *testing.B) {
		b.ReportAllocs()
		i := 0
		for b.Loop() {
			eventos[i%len(eventos)].escribirConFprintf(bw)
			i++
		}
	})
	b.Run("append", func(b *testing.B) {
		b.ReportAllocs()
		linea := make([]byte, 0, 512)
		i := 0
		for b.Loop() {
			linea = eventos[i%len(eventos)].agregarLinea(linea[:0])
			bw.Write(linea)
			i++
		}
	})
}

// TestAgregarLineaIgualQueFprintf garantiza que el formateo optimizado produce
// los mismos bytes que el original, para todos los tipos de evento.
func TestAgregarLineaIgualQueFprintf(t *testing.T) {
	opts := completarOpciones(Opciones{Lineas: 1})
	r := nuevoRand(3)
	m := nuevosMuestreadores(r, opts)
	base := nuevaCorrienteBase(r, opts, m)
	eventos := make([]evento, 0, 5000)
	for range 5000 {
		eventos = append(eventos, base.siguiente())
	}
	esc, _ := construirEscaneo(r, opts, 1e6, m.rutaACategoria)
	eventos = append(eventos, esc...)

	for i, e := range eventos {
		var ref bufferString
		e.escribirConFprintf(&ref)
		if got := string(e.agregarLinea(nil)); got != ref.s {
			t.Fatalf("evento %d:\n append  %q\n fprintf %q", i, got, ref.s)
		}
	}
}

type bufferString struct{ s string }

func (b *bufferString) Write(p []byte) (int, error) { b.s += string(p); return len(p), nil }
