package pipeline_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"parcial1/internal/loggen"
	"parcial1/internal/parse"
	"parcial1/internal/pipeline"
)

var ctx = context.Background()

// archivoDePrueba genera un log reproducible en un archivo temporal.
func archivoDePrueba(t *testing.T, lineas int) string {
	t.Helper()
	ruta := filepath.Join(t.TempDir(), "log.txt")
	f, err := os.Create(ruta)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := loggen.Generar(f, loggen.Opciones{Lineas: lineas, Semilla: 21}); err != nil {
		t.Fatal(err)
	}
	return ruta
}

// TestConcurrenteIgualQueSecuencial: el map-reduce tiene que dar exactamente los
// mismos contadores que la pasada secuencial, para cualquier cantidad de workers.
// Los workers ≫ líneas fuerzan bloques minúsculos y ejercitan la alineación de
// límites a comienzos de línea.
func TestConcurrenteIgualQueSecuencial(t *testing.T) {
	ruta := archivoDePrueba(t, 20_000)
	seq, erroresSeq, err := pipeline.Secuencial(ctx, ruta, parse.ParsearManual)
	if err != nil {
		t.Fatal(err)
	}
	if erroresSeq != 0 {
		t.Fatalf("errores en secuencial: %d", erroresSeq)
	}

	for _, w := range []int{2, 3, 7, 8, 16, 61} {
		conc, errores, err := pipeline.Concurrente(ctx, ruta, w, parse.ParsearManual)
		if err != nil {
			t.Fatalf("workers=%d: %v", w, err)
		}
		if errores != 0 {
			t.Fatalf("workers=%d: %d errores", w, errores)
		}
		if !seq.Igual(conc) {
			t.Fatalf("workers=%d: contadores distintos (total %d vs %d)", w, seq.Total, conc.Total)
		}
	}
}

func TestConcurrenteMutexIgualQueSecuencial(t *testing.T) {
	ruta := archivoDePrueba(t, 5_000)
	seq, _, err := pipeline.Secuencial(ctx, ruta, parse.ParsearManual)
	if err != nil {
		t.Fatal(err)
	}
	conc, _, err := pipeline.ConcurrenteMutex(ctx, ruta, 8, parse.ParsearManual)
	if err != nil {
		t.Fatal(err)
	}
	if !seq.Igual(conc) {
		t.Fatalf("contadores distintos (total %d vs %d)", seq.Total, conc.Total)
	}
}

// TestArchivoMinusculo: más workers que líneas y archivos borde.
func TestArchivoMinusculo(t *testing.T) {
	ruta := archivoDePrueba(t, 3)
	seq, _, err := pipeline.Secuencial(ctx, ruta, parse.ParsearManual)
	if err != nil {
		t.Fatal(err)
	}
	conc, _, err := pipeline.Concurrente(ctx, ruta, 12, parse.ParsearManual)
	if err != nil {
		t.Fatal(err)
	}
	if !seq.Igual(conc) {
		t.Fatal("contadores distintos con más workers que líneas")
	}
	if conc.Total != 3 {
		t.Fatalf("Total: %d", conc.Total)
	}
}

func TestArchivoVacio(t *testing.T) {
	ruta := filepath.Join(t.TempDir(), "vacio.log")
	if err := os.WriteFile(ruta, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	conc, errores, err := pipeline.Concurrente(ctx, ruta, 4, parse.ParsearManual)
	if err != nil {
		t.Fatal(err)
	}
	if conc.Total != 0 || errores != 0 {
		t.Fatalf("total=%d errores=%d", conc.Total, errores)
	}
}

// TestLineasMalformadasSeCuentan: los errores de parseo se suman entre workers.
func TestLineasMalformadasSeCuentan(t *testing.T) {
	ruta := filepath.Join(t.TempDir(), "mixto.log")
	contenido := `10.0.0.1 - - [01/Jan/2024:00:00:02 -0300] "GET / HTTP/1.1" 200 10 "-" "ua" 1
basura que no parsea
10.0.0.2 - - [01/Jan/2024:00:00:03 -0300] "GET / HTTP/1.1" 200 10 "-" "ua" 1
mas basura
`
	if err := os.WriteFile(ruta, []byte(contenido), 0o644); err != nil {
		t.Fatal(err)
	}
	cont, errores, err := pipeline.Concurrente(ctx, ruta, 3, parse.ParsearManual)
	if err != nil {
		t.Fatal(err)
	}
	if cont.Total != 2 || errores != 2 {
		t.Fatalf("total=%d errores=%d, esperaba 2 y 2", cont.Total, errores)
	}
}
