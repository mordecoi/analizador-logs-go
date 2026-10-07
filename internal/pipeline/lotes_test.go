package pipeline

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"parcial1/internal/agregado"
	"parcial1/internal/anomalias"
	"parcial1/internal/loggen"
	"parcial1/internal/parse"
)

// recolectarLineas es un Parser de prueba que guarda una copia de cada línea.
func recolectarLineas(dst *[]string) Parser {
	return func(s string) (parse.Entrada, error) {
		*dst = append(*dst, strings.Clone(s))
		return parse.Entrada{}, nil
	}
}

// TestLectorLotesBordes recorre contenidos borde con lotes minúsculos, para
// forzar líneas partidas entre lotes y líneas más largas que el buffer.
func TestLectorLotesBordes(t *testing.T) {
	casos := []struct {
		nombre    string
		contenido string
		esperadas []string
	}{
		{"vacío", "", nil},
		{"una sin salto", "abc", []string{"abc"}},
		{"una con salto", "abc\n", []string{"abc"}},
		{"crlf", "ab\r\ncd\r\n", []string{"ab", "cd"}},
		{"líneas vacías", "a\n\nb\n", []string{"a", "", "b"}},
		{"más larga que el buffer", "0123456789abcdefghij\nxy\n", []string{"0123456789abcdefghij", "xy"}},
		{"sin salto final", "uno\ndos\ntres", []string{"uno", "dos", "tres"}},
	}
	for _, c := range casos {
		for _, tam := range []int{1, 3, 7, 64} {
			t.Run(fmt.Sprintf("%s/lote=%d", c.nombre, tam), func(t *testing.T) {
				var got []string
				l := nuevoLectorLotes(strings.NewReader(c.contenido), tam)
				for {
					lt, err := l.siguiente()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					procesarLote(lt, 0, int64(len(c.contenido))+1, recolectarLineas(&got), func(parse.Entrada) {})
				}
				if !slices.Equal(got, c.esperadas) {
					t.Fatalf("obtuve %q, esperaba %q", got, c.esperadas)
				}
			})
		}
	}
}

// archivoGenerado escribe un log reproducible y devuelve su ruta.
func archivoGenerado(t testing.TB, lineas int) string {
	t.Helper()
	ruta := filepath.Join(t.TempDir(), "log.txt")
	f, err := os.Create(ruta)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := loggen.Generar(f, loggen.Opciones{Lineas: lineas, Semilla: 5}); err != nil {
		t.Fatal(err)
	}
	return ruta
}

// TestBloquesConLotesChicos: con lotes de pocos bytes, cada bloque cruza
// cientos de límites de lote y de bloque; el resultado tiene que ser idéntico.
func TestBloquesConLotesChicos(t *testing.T) {
	ruta := archivoGenerado(t, 3000)
	ctx := context.Background()
	ref, _, err := Secuencial(ctx, ruta, parse.ParsearManual)
	if err != nil {
		t.Fatal(err)
	}
	tam, _ := tamanoDe(ruta)
	for _, workers := range []int{1, 3, 8} {
		for _, tamLote := range []int{50, 333, 4096} {
			total := agregado.NuevoContadores()
			limites := limitesDeBloques(tam, workers)
			for w := range workers {
				parcial := agregado.NuevoContadores()
				if _, err := procesarBloque(ctx, ruta, limites[w], limites[w+1], parse.ParsearManual, parcial.Agregar, tamLote); err != nil {
					t.Fatal(err)
				}
				total.Fusionar(parcial)
			}
			if !ref.Igual(total) {
				t.Fatalf("workers=%d lote=%d: total %d vs %d", workers, tamLote, total.Total, ref.Total)
			}
		}
	}
}

// TestDesdeLectorGzip: el productor-consumidor sobre un .gz da lo mismo que el
// recorrido secuencial del archivo sin comprimir.
func TestDesdeLectorGzip(t *testing.T) {
	ruta := archivoGenerado(t, 5000)
	ctx := context.Background()
	ref, _, err := Secuencial(ctx, ruta, parse.ParsearManual)
	if err != nil {
		t.Fatal(err)
	}
	datos, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatal(err)
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write(datos)
	zw.Close()

	for _, tamLote := range []int{100, 64 << 10} {
		zr, err := gzip.NewReader(bytes.NewReader(gz.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		got, errores, err := desdeLector(ctx, zr, 4, parse.ParsearManual, tamLote)
		if err != nil {
			t.Fatal(err)
		}
		if errores != 0 || !ref.Igual(got) {
			t.Fatalf("lote=%d: total %d vs %d, %d errores", tamLote, got.Total, ref.Total, errores)
		}
	}
}

// TestAnomaliasSobreLotes es la prueba de integración del contrato de vistas:
// el detector retiene IPs y rutas por mucho tiempo. Si alguna quedara apuntando
// al buffer reutilizado del lector, sus hallazgos saldrían con IPs corruptas y
// distintos de los obtenidos parseando strings independientes.
func TestAnomaliasSobreLotes(t *testing.T) {
	ruta := archivoGenerado(t, 60_000)

	ref := anomalias.NuevoDetector(anomalias.ConfigPorDefecto())
	f, err := os.Open(ruta)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if e, err := parse.ParsearManual(sc.Text()); err == nil {
			ref.Procesar(e)
		}
	}

	d := anomalias.NuevoDetector(anomalias.ConfigPorDefecto())
	if _, err := procesarBloque(context.Background(), ruta, 0, 1<<62, parse.ParsearManual, d.Procesar, 4096); err != nil {
		t.Fatal(err)
	}

	esperados, obtenidos := ref.Hallazgos(), d.Hallazgos()
	if len(esperados) == 0 {
		t.Fatal("el log de prueba no tiene anomalías detectables")
	}
	if !slices.EqualFunc(esperados, obtenidos, func(a, b anomalias.Hallazgo) bool {
		return a.Tipo == b.Tipo && a.IP == b.IP && a.Endpoint == b.Endpoint &&
			a.Desde.Equal(b.Desde) && a.Cantidad == b.Cantidad
	}) {
		t.Fatalf("hallazgos distintos:\n esperados %+v\n obtenidos %+v", esperados, obtenidos)
	}
}

// procesarConReadString es la lectura del primer parcial (un string nuevo por
// línea con bufio.Reader.ReadString), conservada solo como referencia para
// BenchmarkLectura.
func procesarConReadString(ruta string, parsear Parser, agregar func(parse.Entrada)) error {
	f, err := os.Open(ruta)
	if err != nil {
		return err
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 1<<20)
	for {
		linea, err := br.ReadString('\n')
		if len(linea) > 0 {
			if e, perr := parsear(strings.TrimRight(linea, "\r\n")); perr == nil {
				agregar(e)
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// BenchmarkLectura compara la lectura línea a línea con ReadString contra la
// lectura por lotes con vistas sin copia, sobre el mismo archivo y el mismo
// trabajo de parseo y conteo.
//
//	go test ./internal/pipeline -run '^$' -bench Lectura -benchmem
func BenchmarkLectura(b *testing.B) {
	ruta := archivoGenerado(b, 200_000)
	info, _ := os.Stat(ruta)
	b.Run("readstring", func(b *testing.B) {
		b.SetBytes(info.Size())
		b.ReportAllocs()
		for b.Loop() {
			c := agregado.NuevoContadores()
			if err := procesarConReadString(ruta, parse.ParsearManual, c.Agregar); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("lotes", func(b *testing.B) {
		b.SetBytes(info.Size())
		b.ReportAllocs()
		for b.Loop() {
			c := agregado.NuevoContadores()
			if _, err := procesarBloque(context.Background(), ruta, 0, info.Size(), parse.ParsearManual, c.Agregar, TamLotePorDefecto); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkTamLote mide la lectura secuencial (sin parseo) con distintos
// tamaños de lote: cuántas llamadas al sistema hacen falta para leer el archivo.
func BenchmarkTamLote(b *testing.B) {
	ruta := archivoGenerado(b, 200_000)
	info, _ := os.Stat(ruta)
	nada := func(string) (parse.Entrada, error) { return parse.Entrada{}, nil }
	for _, tam := range []int{512, 4 << 10, 64 << 10, 1 << 20, 8 << 20} {
		nombre := fmt.Sprintf("%dKiB", tam>>10)
		if tam < 1<<10 {
			nombre = fmt.Sprintf("%dB", tam)
		}
		b.Run(nombre, func(b *testing.B) {
			b.SetBytes(info.Size())
			for b.Loop() {
				if _, err := procesarBloque(context.Background(), ruta, 0, info.Size(), nada, func(parse.Entrada) {}, tam); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
