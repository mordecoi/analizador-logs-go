package reporte

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"parcial1/internal/agregado"
	"parcial1/internal/anomalias"
	"parcial1/internal/cache"
	"parcial1/internal/loggen"
	"parcial1/internal/medicion"
)

func TestBarra(t *testing.T) {
	casos := []struct {
		v, maximo, ancho int
		esperado         string
	}{
		{10, 10, 5, "#####"},
		{5, 10, 4, "##"},
		{0, 10, 4, ""},
		{3, 0, 4, ""}, // sin máximo no hay escala
	}
	for _, c := range casos {
		if got := Barra(c.v, c.maximo, c.ancho); got != c.esperado {
			t.Errorf("Barra(%d, %d, %d) = %q, esperaba %q", c.v, c.maximo, c.ancho, got, c.esperado)
		}
	}
}

func TestPrimeraDiferencia(t *testing.T) {
	p := func(k string, n int) agregado.Par[string] { return agregado.Par[string]{Clave: k, Conteo: n} }
	gt := []agregado.Par[string]{p("a", 3), p("b", 2)}
	casos := []struct {
		nombre  string
		nuestro []agregado.Par[string]
		dif     int
	}{
		{"idéntico", []agregado.Par[string]{p("a", 3), p("b", 2)}, -1},
		{"más largo que el gt", []agregado.Par[string]{p("a", 3), p("b", 2), p("c", 1)}, -1},
		{"conteo distinto", []agregado.Par[string]{p("a", 3), p("b", 1)}, 1},
		{"orden distinto", []agregado.Par[string]{p("b", 2), p("a", 3)}, 0},
		{"más corto", []agregado.Par[string]{p("a", 3)}, 1},
		{"vacío", nil, 0},
	}
	for _, c := range casos {
		if got := PrimeraDiferencia(c.nuestro, gt); got != c.dif {
			t.Errorf("%s: %d, esperaba %d", c.nombre, got, c.dif)
		}
		detalle := detalleRanking(c.nuestro, gt, c.dif)
		if c.dif < 0 && !strings.Contains(detalle, "idénticas") || c.dif >= 0 && !strings.HasPrefix(detalle, "pos ") {
			t.Errorf("%s: detalle %q", c.nombre, detalle)
		}
	}
}

func TestEvaluarAnomalias(t *testing.T) {
	base := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	ver := loggen.Verificacion{Anomalias: []loggen.Anomalia{
		{Tipo: "rafaga", IP: "1.1.1.1", Desde: base, Hasta: base.Add(time.Minute)},
		{Tipo: "escaneo", IP: "2.2.2.2", Desde: base, Hasta: base.Add(time.Minute)},
	}}
	hs := []anomalias.Hallazgo{
		{Tipo: "rafaga", IP: "1.1.1.1", Desde: base.Add(10 * time.Second), Hasta: base.Add(20 * time.Second)},
		{Tipo: "rafaga", IP: "1.1.1.1", Desde: base.Add(30 * time.Second), Hasta: base.Add(40 * time.Second)}, // repetido
		{Tipo: "escaneo", IP: "9.9.9.9", Desde: base, Hasta: base.Add(time.Minute)},                           // otra IP
	}
	var w bytes.Buffer
	ev := EvaluarAnomalias(&w, hs, ver)
	if ev.Detectadas != 1 || ev.FalsosPositivos != 2 {
		t.Fatalf("detectadas %d, falsos positivos %d\n%s", ev.Detectadas, ev.FalsosPositivos, w.String())
	}
	if p := ev.Precision(); p < 0.33 || p > 0.34 {
		t.Errorf("precisión %.2f, esperaba 1/3", p)
	}
	if (Evaluacion{}).Precision() != 1 {
		t.Error("sin hallazgos la precisión es 1 (no hay falsos positivos)")
	}
}

func TestAgregarCSVEncabezadoUnaVez(t *testing.T) {
	ruta := filepath.Join(t.TempDir(), "a", "b.csv")
	m := medicion.Medicion{Transcurrido: time.Second, HeapFinal: 1 << 20}
	for range 3 {
		if err := AgregarCSV(ruta, "v1", "/x/log.txt", 1000, m); err != nil {
			t.Fatal(err)
		}
	}
	datos, _ := os.ReadFile(ruta)
	lineas := strings.Split(strings.TrimSpace(string(datos)), "\n")
	if len(lineas) != 4 || lineas[0] != encabezadoCSV {
		t.Fatalf("contenido inesperado:\n%s", datos)
	}
	if lineas[1] != "v1,log.txt,1000,1000.0,1000,1.0,0.0,0.00,0" {
		t.Errorf("fila: %q", lineas[1])
	}
}

func TestLeerVerificacionErrores(t *testing.T) {
	dir := t.TempDir()
	if _, err := LeerVerificacion(filepath.Join(dir, "no-existe.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("archivo inexistente: %v", err)
	}
	malo := filepath.Join(dir, "malo.json")
	os.WriteFile(malo, []byte("{no es json"), 0o600)
	if _, err := LeerVerificacion(malo); err == nil {
		t.Error("JSON inválido: esperaba error")
	}
}

func ExampleBarra() {
	for _, v := range []int{100, 50, 10} {
		fmt.Println(Barra(v, 100, 20))
	}
	// Output:
	// ####################
	// ##########
	// ##
}

// contadoresDePrueba arma un log mínimo ya agregado.
func contadoresDePrueba() *agregado.Contadores {
	c := agregado.NuevoContadores()
	c.Total = 4
	c.Estados[200], c.Estados[404] = 3, 1
	c.Endpoints.Incrementar("/")
	c.Endpoints.Incrementar("/")
	c.Endpoints.Incrementar("/")
	c.Endpoints.Incrementar("/x")
	for _, ip := range []string{"1.1.1.1", "1.1.1.1", "2.2.2.2", "3.3.3.3"} {
		c.IPs.Incrementar(ip)
	}
	c.Usuarios.Incrementar("ana")
	c.PorHora[9], c.PorHora[14] = 3, 1
	return c
}

func TestResumenYValidar(t *testing.T) {
	q := cache.NuevasConsultas(contadoresDePrueba(), agregado.MetodoHeap)
	var w bytes.Buffer
	Resumen(&w, q, Corrida{Medicion: medicion.Medicion{Transcurrido: time.Second}, Errores: 2, DesdeCache: true}, 5)
	out := w.String()
	for _, esperado := range []string{"Analizadas 4 líneas (2 no reconocidas)", "memoizados", "pico: 09:00", "1.1.1.1  2  50.0%", "ana"} {
		if !strings.Contains(out, esperado) {
			t.Errorf("falta %q en:\n%s", esperado, out)
		}
	}

	p := func(k string, n int) agregado.Par[string] { return agregado.Par[string]{Clave: k, Conteo: n} }
	ver := loggen.Verificacion{
		Lineas: 4, HoraPico: 9,
		TopEndpoints:  []agregado.Par[string]{p("/", 3), p("/x", 1)},
		TopEstados:    []agregado.Par[string]{p("200", 3), p("404", 1)},
		TopIPsActivas: []agregado.Par[string]{p("1.1.1.1", 2)},
	}
	if !Validar(&w, q, ver, 5) {
		t.Fatalf("esperaba validación OK:\n%s", w.String())
	}
	ver.TopEndpoints[1].Conteo = 9
	w.Reset()
	if Validar(&w, q, ver, 5) || !strings.Contains(w.String(), "DIFIERE") {
		t.Fatalf("esperaba DIFIERE:\n%s", w.String())
	}
}

func TestAnomaliasSinHallazgos(t *testing.T) {
	var w bytes.Buffer
	Anomalias(&w, nil, time.Millisecond)
	if !strings.Contains(w.String(), "sin hallazgos") {
		t.Fatal(w.String())
	}
	Anomalias(&w, []anomalias.Hallazgo{{Tipo: "pico_5xx", Endpoint: "/api", Detalle: "d"}}, time.Millisecond)
	if !strings.Contains(w.String(), "[pico_5xx]  /api") {
		t.Fatal(w.String())
	}
}
