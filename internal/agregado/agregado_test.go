package agregado_test

import (
	"bufio"
	"bytes"
	"math/rand/v2"
	"reflect"
	"strconv"
	"testing"
	"time"

	"parcial1/internal/agregado"
	"parcial1/internal/loggen"
	"parcial1/internal/parse"
)

func entrada(ip, ruta string, estado, hora int) parse.Entrada {
	return parse.Entrada{
		IP:     ip,
		Ruta:   ruta,
		Estado: estado,
		Fecha:  time.Date(2024, 1, 1, hora, 0, 0, 0, time.UTC),
	}
}

func TestContadores(t *testing.T) {
	c := agregado.NuevoContadores()
	c.Agregar(entrada("1.1.1.1", "/a", 200, 9))
	c.Agregar(entrada("1.1.1.1", "/a", 200, 9))
	c.Agregar(entrada("2.2.2.2", "/b", 404, 14))

	if c.Total != 3 {
		t.Fatalf("Total: %d", c.Total)
	}
	if c.Endpoints.Valor("/a") != 2 || c.Endpoints.Valor("/b") != 1 {
		t.Errorf("Endpoints: %v", c.Endpoints.Mapa())
	}
	if c.IPs.Valor("1.1.1.1") != 2 {
		t.Errorf("IPs: %v", c.IPs.Mapa())
	}
	if c.Estados[200] != 2 || c.Estados[404] != 1 {
		t.Errorf("Estados: %v", c.Estados)
	}
	if c.HoraPico() != 9 {
		t.Errorf("HoraPico: %d", c.HoraPico())
	}
}

var metodos = []struct {
	nombre string
	m      agregado.Metodo
}{
	{"sort", agregado.MetodoSort},
	{"heap", agregado.MetodoHeap},
}

func TestTopNOrdenYDesempate(t *testing.T) {
	m := agregado.Mapa[string]{"a": 5, "b": 5, "c": 9, "d": 1}
	for _, met := range metodos {
		t.Run(met.nombre, func(t *testing.T) {
			top := agregado.TopN(m, 3, met.m)
			if len(top) != 3 {
				t.Fatalf("len: %d", len(top))
			}
			// c primero por conteo; a antes que b por desempate alfabético.
			if top[0].Clave != "c" || top[1].Clave != "a" || top[2].Clave != "b" {
				t.Errorf("orden inesperado: %v", top)
			}
		})
	}
}

func TestTopNPideMasQueLoQueHay(t *testing.T) {
	for _, met := range metodos {
		top := agregado.TopN(agregado.Mapa[string]{"x": 1}, 10, met.m)
		if len(top) != 1 {
			t.Errorf("%s: len %d", met.nombre, len(top))
		}
	}
}

// TestSortHeapEquivalentes: las dos estrategias tienen que devolver exactamente
// el mismo slice, incluso con muchos empates (donde importa el desempate).
func TestSortHeapEquivalentes(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2)) // semilla fija: corridas reproducibles
	casos := []map[int]int{
		aleatorio(r, 50, 1000),    // conteos dispersos, pocas claves
		aleatorio(r, 100_000, 40), // muchas claves, pocos valores → muchos empates
		constante(500, 7),         // todos iguales: el desempate decide todo
	}
	for ci, m := range casos {
		for _, k := range []int{1, 10, 100, 1_000_000} {
			s := agregado.TopNSort(agregado.Mapa[int](m), k)
			h := agregado.TopNHeap(agregado.Mapa[int](m), k)
			if !reflect.DeepEqual(s, h) {
				t.Errorf("caso %d k=%d: sort y heap difieren\n sort=%v\n heap=%v", ci, k, s, h)
			}
		}
	}
}

func aleatorio(r *rand.Rand, u, maxConteo int) map[int]int {
	m := make(map[int]int, u)
	for i := 0; i < u; i++ {
		m[i] = 1 + r.IntN(maxConteo)
	}
	return m
}

func constante(u, valor int) map[int]int {
	m := make(map[int]int, u)
	for i := 0; i < u; i++ {
		m[i] = valor
	}
	return m
}

// TestContraGroundTruth genera un log, lo re-analiza y verifica que los
// agregados coinciden exactamente con lo que reportó el generador.
func TestContraGroundTruth(t *testing.T) {
	var buf bytes.Buffer
	ver, err := loggen.Generar(&buf, loggen.Opciones{Lineas: 50_000, Semilla: 99})
	if err != nil {
		t.Fatal(err)
	}

	c := agregado.NuevoContadores()
	sc := bufio.NewScanner(&buf)
	for sc.Scan() {
		e, err := parse.Parsear(sc.Text())
		if err != nil {
			t.Fatalf("línea no parseable: %v", err)
		}
		c.Agregar(e)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}

	if c.Total != ver.Lineas {
		t.Errorf("Total: %d vs %d", c.Total, ver.Lineas)
	}
	if c.HoraPico() != ver.HoraPico {
		t.Errorf("HoraPico: %d vs %d", c.HoraPico(), ver.HoraPico)
	}

	topEndpoints := agregado.TopN(c.Endpoints, len(ver.TopEndpoints), agregado.MetodoHeap)
	for i, g := range ver.TopEndpoints {
		if topEndpoints[i].Clave != g.Clave || topEndpoints[i].Conteo != g.Conteo {
			t.Errorf("endpoint %d: %s=%d vs %s=%d", i,
				topEndpoints[i].Clave, topEndpoints[i].Conteo, g.Clave, g.Conteo)
		}
	}

	topEstados := agregado.TopN(agregado.Mapa[int](c.Estados), len(ver.TopEstados), agregado.MetodoHeap)
	for i, g := range ver.TopEstados {
		if strconv.Itoa(topEstados[i].Clave) != g.Clave || topEstados[i].Conteo != g.Conteo {
			t.Errorf("estado %d: %d=%d vs %s=%d", i,
				topEstados[i].Clave, topEstados[i].Conteo, g.Clave, g.Conteo)
		}
	}
}
