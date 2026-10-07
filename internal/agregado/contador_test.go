package agregado_test

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"maps"
	"math/rand/v2"
	"testing"
	"unsafe"

	"parcial1/internal/agregado"
)

// TestContadorNoRetieneLaClave es la regresión del bug que motivó Contador:
// con un map[string]int, incrementar una clave existente con una vista sobre
// un buffer reutilizado reemplazaba la clave guardada por esa vista.
func TestContadorNoRetieneLaClave(t *testing.T) {
	c := agregado.NuevoContador()
	buf := []byte("hola")
	vista := unsafe.String(&buf[0], len(buf)) //nolint:gosec // G103: el test reproduce a propósito la vista del lector
	c.Incrementar(vista)
	c.Incrementar(vista)
	copy(buf, "chau") // el lector reutiliza el buffer para la línea siguiente

	if got := c.Mapa(); !maps.Equal(got, map[string]int{"hola": 2}) {
		t.Fatalf("el contador quedó apuntando al buffer: %v", got)
	}
}

// contadoresAleatorios arma k contadores con claves que se repiten entre ellos.
func contadoresAleatorios(k, claves, particiones int) ([]*agregado.Contador, map[string]int) {
	r := rand.New(rand.NewPCG(7, 7))
	esperado := map[string]int{}
	cs := make([]*agregado.Contador, k)
	for i := range cs {
		cs[i] = agregado.NuevoContadorParticionado(particiones)
		for range 2000 {
			clave := fmt.Sprintf("ip-%d", r.IntN(claves))
			cs[i].Incrementar(clave)
			esperado[clave]++
		}
	}
	return cs, esperado
}

// TestFusionarClaves: la fusión paralela da lo mismo que contar todo junto,
// tanto con contadores ya particionados como con particiones distintas.
func TestFusionarClaves(t *testing.T) {
	casos := []struct{ particionesOrigen, p int }{
		{1, 1}, {1, 4}, {4, 4}, {8, 8}, {3, 5},
	}
	for _, c := range casos {
		t.Run(fmt.Sprintf("origen=%d/p=%d", c.particionesOrigen, c.p), func(t *testing.T) {
			cs, esperado := contadoresAleatorios(6, 1500, c.particionesOrigen)
			res := agregado.FusionarClaves(cs, c.p)
			if !maps.Equal(res.Mapa(), esperado) {
				t.Fatal("la fusión no coincide con el conteo directo")
			}
			if res.Len() != len(esperado) {
				t.Fatalf("Len %d, esperaba %d", res.Len(), len(esperado))
			}
			for k, v := range esperado {
				if res.Valor(k) != v {
					t.Fatalf("Valor(%q) = %d, esperaba %d", k, res.Valor(k), v)
				}
			}
			if res.Valor("no-existe") != 0 {
				t.Fatal("una clave ausente vale 0")
			}
			// un contador particionado sigue admitiendo incrementos
			res.Incrementar("ip-0")
			if res.Valor("ip-0") != esperado["ip-0"]+1 {
				t.Fatal("Incrementar sobre un contador particionado")
			}
		})
	}
}

func TestContadorGob(t *testing.T) {
	cs, esperado := contadoresAleatorios(3, 500, 4)
	orig := agregado.FusionarClaves(cs, 4)

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(orig); err != nil {
		t.Fatal(err)
	}
	var cargado agregado.Contador
	if err := gob.NewDecoder(&buf).Decode(&cargado); err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(cargado.Mapa(), esperado) {
		t.Fatal("el contador no sobrevive a gob")
	}
	if err := cargado.GobDecode([]byte("basura")); err == nil {
		t.Fatal("esperaba error con datos inválidos")
	}
}

func TestAllCorteTemprano(t *testing.T) {
	cs, _ := contadoresAleatorios(1, 100, 3)
	n := 0
	for range cs[0].All() {
		n++
		if n == 5 {
			break
		}
	}
	if n != 5 {
		t.Fatalf("el iterador no respetó el break: %d", n)
	}
}

func ExampleTopN() {
	visitas := agregado.Mapa[string]{"/": 120, "/api/cart": 38, "/login": 55, "/about": 8}
	for _, p := range agregado.TopN(visitas, 3, agregado.MetodoHeap) {
		fmt.Println(p.Clave, p.Conteo)
	}
	// Output:
	// / 120
	// /login 55
	// /api/cart 38
}

func ExampleContador() {
	c := agregado.NuevoContador()
	for _, ip := range []string{"10.0.0.1", "10.0.0.2", "10.0.0.1"} {
		c.Incrementar(ip)
	}
	fmt.Println(c.Len(), c.Valor("10.0.0.1"), c.Valor("10.9.9.9"))
	// Output: 2 2 0
}

func ExampleFusionarClaves() {
	// dos workers contaron cada uno su bloque del archivo
	a, b := agregado.NuevoContadorParticionado(2), agregado.NuevoContadorParticionado(2)
	a.Incrementar("10.0.0.1")
	b.Incrementar("10.0.0.1")
	b.Incrementar("10.0.0.2")

	total := agregado.FusionarClaves([]*agregado.Contador{a, b}, 2)
	fmt.Println(total.Valor("10.0.0.1"), total.Valor("10.0.0.2"))
	// Output: 2 1
}

func TestFusionarParaleloContadores(t *testing.T) {
	a, b := agregado.NuevoContadoresParticionados(3), agregado.NuevoContadoresParticionados(3)
	a.Agregar(entrada("1.1.1.1", "/a", 200, 9))
	b.Agregar(entrada("1.1.1.1", "/b", 500, 9))
	b.Agregar(entrada("2.2.2.2", "/a", 200, 23))

	total := agregado.FusionarParalelo([]*agregado.Contadores{a, b}, 3)
	if total.Total != 3 || total.IPs.Valor("1.1.1.1") != 2 || total.Endpoints.Valor("/a") != 2 ||
		total.Estados[200] != 2 || total.PorHora[9] != 2 || total.PorHora[23] != 1 {
		t.Fatalf("fusión incorrecta: %+v", total)
	}

	otro := agregado.FusionarParalelo([]*agregado.Contadores{b, a}, 1)
	if !total.Igual(otro) {
		t.Fatal("el resultado no puede depender del orden ni de la cantidad de particiones")
	}
	otro.Agregar(entrada("3.3.3.3", "/c", 404, 1))
	if total.Igual(otro) {
		t.Fatal("Igual no detecta una diferencia")
	}
}
