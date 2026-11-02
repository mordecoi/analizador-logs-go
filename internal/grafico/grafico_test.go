package grafico

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Una corrida más rápida que la resolución del CSV queda con ms = 0.0, que no
// existe en escala logarítmica: el eje empezaba en -Inf y la grilla no terminaba.
func TestMedicionEnCero(t *testing.T) {
	ruta := filepath.Join(t.TempDir(), "m.csv")
	csv := `version,archivo,lineas,ms
final,a.log,3000,0.0
final,a.log,1000000,330
vacio,a.log,0,5
`
	if err := os.WriteFile(ruta, []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Leer(ruta)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.Orden, ",") != "final" {
		t.Fatalf("la fila con n = 0 no tiene lugar en el gráfico: %v", s.Orden)
	}
	svg := Dibujar(s, "título")
	if strings.Contains(svg, "NaN") || strings.Contains(svg, "Inf") {
		t.Fatalf("coordenadas inválidas en el SVG:\n%s", svg)
	}
	if n := strings.Count(svg, "<circle"); n != 2 {
		t.Errorf("esperaba conservar los 2 puntos de la serie, hay %d", n)
	}
}

func TestLeerYDibujar(t *testing.T) {
	ruta := filepath.Join(t.TempDir(), "m.csv")
	csv := `version,archivo,lineas,ms
base,a.log,5000000,33600
final,a.log,5000000,716
base,a.log,1000000,7300
final,a.log,1000000,330
fila,corta
final,a.log,no-es-numero,1
`
	if err := os.WriteFile(ruta, []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Leer(ruta)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.Orden, ",") != "base,final" {
		t.Fatalf("orden de leyenda: %v", s.Orden)
	}
	if pts := s.Puntos["base"]; len(pts) != 2 || pts[0].N != 1e6 || pts[1].N != 5e6 {
		t.Fatalf("los puntos tienen que quedar ordenados por n: %+v", pts)
	}

	svg := Dibujar(s, "título")
	if !strings.HasPrefix(svg, "<svg") || !strings.HasSuffix(svg, "</svg>\n") {
		t.Fatal("SVG mal formado")
	}
	if n := strings.Count(svg, "<polyline"); n != 2 {
		t.Errorf("esperaba una curva por versión, hay %d", n)
	}
	for _, etiqueta := range []string{"33.6 s", "716 ms", "título"} {
		if !strings.Contains(svg, etiqueta) {
			t.Errorf("falta la etiqueta %q", etiqueta)
		}
	}
}

func TestLeerSinDatos(t *testing.T) {
	ruta := filepath.Join(t.TempDir(), "vacio.csv")
	os.WriteFile(ruta, []byte("version,archivo,lineas,ms\n"), 0o600)
	if _, err := Leer(ruta); err == nil {
		t.Fatal("esperaba error con un CSV sin filas")
	}
	if _, err := Leer(filepath.Join(t.TempDir(), "no-existe.csv")); err == nil {
		t.Fatal("esperaba error con un archivo inexistente")
	}
}

func TestEtiquetaMs(t *testing.T) {
	casos := map[float64]string{250: "250 ms", 2500: "2.5 s", 145_700: "2.4 min"}
	for ms, esperado := range casos {
		if got := etiquetaMs(ms); got != esperado {
			t.Errorf("etiquetaMs(%v) = %q, esperaba %q", ms, got, esperado)
		}
	}
}
