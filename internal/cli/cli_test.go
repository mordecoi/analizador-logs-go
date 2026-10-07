package cli

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"parcial1/internal/loggen"
)

// ejecutar corre la CLI con args y devuelve lo que escribió en la salida.
func ejecutar(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := NuevoComando()
	var salida bytes.Buffer
	cmd.SetOut(&salida)
	cmd.SetErr(&salida)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return salida.String(), err
}

// logGenerado corre "generar" en un directorio temporal y devuelve las rutas
// del log y del ground truth.
func logGenerado(t *testing.T, lineas string) (log, gt string) {
	t.Helper()
	dir := t.TempDir()
	log, gt = filepath.Join(dir, "log.txt"), filepath.Join(dir, "gt.json")
	out, err := ejecutar(t, "", "generar", "-n", lineas, "-o", log, "--verificacion", gt)
	if err != nil {
		t.Fatalf("generar: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Anomalías inyectadas") {
		t.Fatalf("resumen de generación incompleto:\n%s", out)
	}
	return log, gt
}

func TestAnalizarValidaYDetecta(t *testing.T) {
	log, gt := logGenerado(t, "40000")
	out, err := ejecutar(t, "", "analizar", "-a", log, "--verificacion", gt, "--anomalias")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, esperado := range []string{
		"Analizadas 40000 líneas",
		"top endpoints    OK",
		"top IPs          OK",
		"recall 3/3, precisión 100%",
	} {
		if !strings.Contains(out, esperado) {
			t.Errorf("falta %q en la salida:\n%s", esperado, out)
		}
	}
}

// TestModosDanLoMismo: todas las combinaciones de parser, modo y Top-N tienen
// que validar contra el mismo ground truth.
func TestModosDanLoMismo(t *testing.T) {
	log, gt := logGenerado(t, "15000")
	for _, parser := range parsers {
		for _, modo := range modos {
			for _, met := range metodosTopN {
				out, err := ejecutar(t, "", "analizar", "-a", log, "--verificacion", gt,
					"--parser", parser, "--modo", modo, "--topn-metodo", met, "-w", "3")
				if err != nil {
					t.Fatalf("%s/%s/%s: %v\n%s", parser, modo, met, err, out)
				}
			}
		}
	}
}

func TestAnalizarGzipYStdin(t *testing.T) {
	log, gt := logGenerado(t, "10000")
	datos, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}

	gz := log + ".gz"
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(datos)
	zw.Close()
	if err := os.WriteFile(gz, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := ejecutar(t, "", "analizar", "-a", gz, "--verificacion", gt, "--anomalias"); err != nil {
		t.Fatalf("gzip: %v\n%s", err, out)
	}
	if out, err := ejecutar(t, string(datos), "analizar", "-a", "-", "--verificacion", gt); err != nil {
		t.Fatalf("stdin: %v\n%s", err, out)
	}
}

func TestCacheSegundaCorrida(t *testing.T) {
	log, _ := logGenerado(t, "5000")
	out, err := ejecutar(t, "", "analizar", "-a", log, "--cache")
	if err != nil || !strings.Contains(out, "miss (sin cache previo)") {
		t.Fatalf("primera corrida: %v\n%s", err, out)
	}
	out, err = ejecutar(t, "", "analizar", "-a", log, "--cache")
	if err != nil || !strings.Contains(out, "agregados memoizados") {
		t.Fatalf("segunda corrida: %v\n%s", err, out)
	}
}

// TestValidacionFallida: un ground truth que no coincide devuelve
// ErrValidacion (el código de salida 2), no un error genérico.
func TestValidacionFallida(t *testing.T) {
	log, gt := logGenerado(t, "5000")
	var ver loggen.Verificacion
	datos, _ := os.ReadFile(gt)
	if err := json.Unmarshal(datos, &ver); err != nil {
		t.Fatal(err)
	}
	ver.Lineas++
	datos, _ = json.Marshal(ver)
	if err := os.WriteFile(gt, datos, 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := ejecutar(t, "", "analizar", "-a", log, "--verificacion", gt)
	if !errors.Is(err, ErrValidacion) {
		t.Fatalf("esperaba ErrValidacion, obtuve %v", err)
	}
	if !strings.Contains(out, "total de líneas   DIFIERE") && !strings.Contains(out, "DIFIERE") {
		t.Errorf("la salida no marca la diferencia:\n%s", out)
	}
}

func TestFlagsInvalidos(t *testing.T) {
	casos := [][]string{
		{"analizar", "--modo", "paralelo"},
		{"analizar", "--parser", "otro"},
		{"analizar", "--topn-metodo", "burbuja"},
		{"analizar", "-a", "-", "--anomalias"},
		{"analizar", "argumento-suelto"},
		{"generar", "--desde", "ayer"},
	}
	for _, args := range casos {
		if _, err := ejecutar(t, "", args...); err == nil {
			t.Errorf("%v: esperaba error", args)
		}
	}
}

func TestArchivoInexistente(t *testing.T) {
	if _, err := ejecutar(t, "", "analizar", "-a", filepath.Join(t.TempDir(), "no-existe.log")); err == nil {
		t.Fatal("esperaba error")
	}
}

// TestCSVYGrafico: dos corridas con --csv alimentan el comando grafico.
func TestCSVYGrafico(t *testing.T) {
	log, _ := logGenerado(t, "3000")
	dir := t.TempDir()
	csv := filepath.Join(dir, "sub", "medicion.csv")
	for _, etiqueta := range []string{"seq", "conc"} {
		if out, err := ejecutar(t, "", "analizar", "-a", log, "--modo", etiqueta,
			"--csv", csv, "--version-etiqueta", etiqueta); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	datos, err := os.ReadFile(csv)
	if err != nil {
		t.Fatal(err)
	}
	if lineas := strings.Count(string(datos), "\n"); lineas != 3 {
		t.Fatalf("esperaba encabezado + 2 filas, hay %d líneas:\n%s", lineas, datos)
	}

	svg := filepath.Join(dir, "g.svg")
	out, err := ejecutar(t, "", "grafico", "--csv", csv, "-o", svg)
	if err != nil || !strings.Contains(out, "(2 series)") {
		t.Fatalf("%v\n%s", err, out)
	}
	if contenido, _ := os.ReadFile(svg); !bytes.HasPrefix(contenido, []byte("<svg")) {
		t.Fatalf("el SVG no empieza con <svg: %.40q", contenido)
	}
}

func TestPerfiles(t *testing.T) {
	log, _ := logGenerado(t, "3000")
	dir := t.TempDir()
	cpu, mem, traza := filepath.Join(dir, "cpu.pprof"), filepath.Join(dir, "mem.pprof"), filepath.Join(dir, "trace.out")
	if out, err := ejecutar(t, "", "analizar", "-a", log,
		"--cpuprofile", cpu, "--memprofile", mem, "--trace", traza); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, f := range []string{cpu, mem, traza} {
		if info, err := os.Stat(f); err != nil || info.Size() == 0 {
			t.Errorf("%s: no se escribió (%v)", filepath.Base(f), err)
		}
	}
}

func TestVersion(t *testing.T) {
	out, err := ejecutar(t, "", "--version")
	if err != nil || !strings.Contains(out, Version) {
		t.Fatalf("%v: %q", err, out)
	}
}
