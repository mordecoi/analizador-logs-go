package parse_test

import (
	"bufio"
	"bytes"
	"testing"
	"time"

	"parcial1/internal/loggen"
	"parcial1/internal/parse"
)

// impls son las dos implementaciones de parseo, para correr los tests sobre ambas.
var impls = []struct {
	nombre string
	fn     func(string) (parse.Entrada, error)
}{
	{"regex", parse.Parsear},
	{"manual", parse.ParsearManual},
}

func TestParsearLineaValida(t *testing.T) {
	linea := `148.251.175.245 - alex [02/Jan/2024:06:18:10 -0300] "POST /usr/login HTTP/1.1" 200 4823 "https://t.co/" "python-requests/2.31.0" 342`
	for _, impl := range impls {
		t.Run(impl.nombre, func(t *testing.T) {
			e, err := impl.fn(linea)
			if err != nil {
				t.Fatalf("%v", err)
			}
			casos := []struct {
				nombre             string
				obtenido, esperado any
			}{
				{"IP", e.IP, "148.251.175.245"},
				{"Usuario", e.Usuario, "alex"},
				{"Metodo", e.Metodo, "POST"},
				{"Ruta", e.Ruta, "/usr/login"},
				{"Proto", e.Proto, "HTTP/1.1"},
				{"Estado", e.Estado, 200},
				{"Bytes", e.Bytes, 4823},
				{"Referrer", e.Referrer, "https://t.co/"},
				{"UA", e.UA, "python-requests/2.31.0"},
				{"RespMs", e.RespMs, 342},
				{"Hora", e.Fecha.Hour(), 6},
			}
			for _, c := range casos {
				if c.obtenido != c.esperado {
					t.Errorf("%s: obtuve %v, esperaba %v", c.nombre, c.obtenido, c.esperado)
				}
			}
			if !e.Fecha.Equal(time.Date(2024, 1, 2, 6, 18, 10, 0, time.FixedZone("-0300", -3*3600))) {
				t.Errorf("Fecha: %s", e.Fecha)
			}
		})
	}
}

func TestParsearUsuarioAnonimo(t *testing.T) {
	linea := `10.0.0.1 - - [01/Jan/2024:00:00:02 -0300] "GET /favicon.ico HTTP/1.1" 304 0 "-" "curl/7.81.0" 5`
	for _, impl := range impls {
		t.Run(impl.nombre, func(t *testing.T) {
			e, err := impl.fn(linea)
			if err != nil {
				t.Fatalf("%v", err)
			}
			if e.Usuario != "-" {
				t.Errorf("Usuario: %q, esperaba %q", e.Usuario, "-")
			}
			if e.Bytes != 0 {
				t.Errorf("Bytes: %d, esperaba 0", e.Bytes)
			}
		})
	}
}

func TestParsearLineasMalformadas(t *testing.T) {
	malas := []string{
		"",
		"esto no es un log",
		`10.0.0.1 - - [fecha-basura] "GET / HTTP/1.1" 200 10 "-" "-" 1`,
		`10.0.0.1 - - [01/Jan/2024:00:00:02 -0300] "GET / HTTP/1.1" XXX 10 "-" "-" 1`,
	}
	for _, impl := range impls {
		t.Run(impl.nombre, func(t *testing.T) {
			for _, l := range malas {
				if _, err := impl.fn(l); err == nil {
					t.Errorf("esperaba error para %q", l)
				}
			}
		})
	}
}

// TestManualIgualQueRegex genera un log y verifica que las dos implementaciones
// producen el mismo Entrada para cada línea.
func TestManualIgualQueRegex(t *testing.T) {
	var buf bytes.Buffer
	if _, err := loggen.Generar(&buf, loggen.Opciones{Lineas: 20_000, Semilla: 8}); err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(&buf)
	n := 0
	for sc.Scan() {
		linea := sc.Text()
		r, errR := parse.Parsear(linea)
		m, errM := parse.ParsearManual(linea)
		if (errR == nil) != (errM == nil) {
			t.Fatalf("línea %d: regex err=%v, manual err=%v\n  %s", n, errR, errM, linea)
		}
		if errR != nil {
			continue
		}
		if r.IP != m.IP || r.Usuario != m.Usuario || r.Metodo != m.Metodo ||
			r.Ruta != m.Ruta || r.Proto != m.Proto || r.Estado != m.Estado ||
			r.Bytes != m.Bytes || r.Referrer != m.Referrer || r.UA != m.UA ||
			r.RespMs != m.RespMs {
			t.Fatalf("línea %d: campos difieren\n regex=%+v\n manual=%+v", n, r, m)
		}
		if !r.Fecha.Equal(m.Fecha) || r.Fecha.Hour() != m.Fecha.Hour() {
			t.Fatalf("línea %d: fecha difiere: regex=%s manual=%s", n, r.Fecha, m.Fecha)
		}
		n++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if n < 19_000 {
		t.Fatalf("solo se compararon %d líneas", n)
	}
}
