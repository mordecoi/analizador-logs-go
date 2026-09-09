package loggen_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"parcial1/internal/loggen"
)

// campos extrae de una línea de log Apache Combined lo mínimo para los tests:
// IP, timestamp, ruta y status.
func campos(t *testing.T, linea string) (ip string, ts time.Time, ruta, estado string) {
	t.Helper()
	ip = linea[:strings.IndexByte(linea, ' ')]

	i1 := strings.IndexByte(linea, '[')
	i2 := strings.IndexByte(linea, ']')
	var err error
	ts, err = time.Parse("02/Jan/2006:15:04:05 -0700", linea[i1+1:i2])
	if err != nil {
		t.Fatalf("timestamp inválido en %q: %v", linea, err)
	}

	q1 := strings.IndexByte(linea, '"')
	q2 := strings.IndexByte(linea[q1+1:], '"') + q1 + 1
	if partes := strings.Fields(linea[q1+1 : q2]); len(partes) >= 2 {
		ruta = partes[1]
	}
	if resto := strings.Fields(linea[q2+1:]); len(resto) >= 1 {
		estado = resto[0]
	}
	return
}

func generar(t *testing.T, opts loggen.Opciones) ([]string, loggen.Verificacion) {
	t.Helper()
	var buf bytes.Buffer
	ver, err := loggen.Generar(&buf, opts)
	if err != nil {
		t.Fatalf("Generar: %v", err)
	}
	lineas := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lineas) != opts.Lineas {
		t.Fatalf("esperaba %d líneas, obtuve %d", opts.Lineas, len(lineas))
	}
	return lineas, ver
}

func TestGenerarDeterminista(t *testing.T) {
	opts := loggen.Opciones{Lineas: 5000, Semilla: 42}
	var a, b bytes.Buffer
	if _, err := loggen.Generar(&a, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := loggen.Generar(&b, opts); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("misma semilla, salida distinta")
	}
}

func TestOrdenCronologico(t *testing.T) {
	lineas, _ := generar(t, loggen.Opciones{Lineas: 20_000, Semilla: 7})
	var prev time.Time
	for i, l := range lineas {
		_, ts, _, _ := campos(t, l)
		if ts.Before(prev) {
			t.Fatalf("línea %d fuera de orden: %s antes de %s", i, ts, prev)
		}
		prev = ts
	}
}

func TestDistribucionSesgada(t *testing.T) {
	lineas, _ := generar(t, loggen.Opciones{Lineas: 60_000, Semilla: 3})
	cont := map[string]int{}
	for _, l := range lineas {
		_, _, ruta, _ := campos(t, l)
		cont[ruta]++
	}
	var conteos []int
	var maxC int
	for _, c := range cont {
		conteos = append(conteos, c)
		if c > maxC {
			maxC = c
		}
	}
	if len(cont) < 15 {
		t.Fatalf("esperaba al menos 15 endpoints distintos, hay %d", len(cont))
	}
	// El endpoint más pedido tiene que superar largamente a la mediana:
	// si fuera uniforme, max ≈ mediana.
	slicesSort(conteos)
	mediana := conteos[len(conteos)/2]
	if maxC < 5*mediana {
		t.Fatalf("distribución poco sesgada: max=%d mediana=%d (ratio %.1f)", maxC, mediana, float64(maxC)/float64(mediana))
	}
}

func TestPatronHorario(t *testing.T) {
	_, ver := generar(t, loggen.Opciones{Lineas: 80_000, Semilla: 11})
	pico := ver.SolicitudesPorHora[ver.HoraPico]
	// buscar el valle
	valle := pico
	for _, c := range ver.SolicitudesPorHora {
		if c < valle {
			valle = c
		}
	}
	if pico < 3*valle {
		t.Fatalf("patrón horario plano: pico=%d valle=%d", pico, valle)
	}
	if ver.HoraPico < 8 || ver.HoraPico > 20 {
		t.Errorf("hora pico inesperada: %d h", ver.HoraPico)
	}
}

func TestAnomaliasPresentes(t *testing.T) {
	lineas, ver := generar(t, loggen.Opciones{Lineas: 200_000, Semilla: 5})

	var rafaga, escaneo loggen.Anomalia
	for _, a := range ver.Anomalias {
		switch a.Tipo {
		case "rafaga":
			rafaga = a
		case "escaneo":
			escaneo = a
		}
	}
	if rafaga.IP == "" || escaneo.IP == "" {
		t.Fatal("faltan anomalías en la verificación")
	}

	rafagaHits := 0
	escaneoRutas := map[string]struct{}{}
	for _, l := range lineas {
		ip, _, ruta, _ := campos(t, l)
		if ip == rafaga.IP {
			rafagaHits++
		}
		if ip == escaneo.IP {
			escaneoRutas[ruta] = struct{}{}
		}
	}
	// La IP de ráfaga aparece al menos las veces declaradas (puede tener también
	// tráfico normal por ser del pool activo).
	if rafagaHits < rafaga.Cantidad {
		t.Errorf("IP de ráfaga %s: %d apariciones, esperaba ≥ %d", rafaga.IP, rafagaHits, rafaga.Cantidad)
	}
	if len(escaneoRutas) < 25 {
		t.Errorf("IP de escaneo %s tocó %d rutas distintas, esperaba ≥ 25", escaneo.IP, len(escaneoRutas))
	}
}

// slicesSort ordena in situ (evita importar slices solo para el test).
func slicesSort(s []int) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
