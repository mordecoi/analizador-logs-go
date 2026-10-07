// Package agregado acumula las métricas del análisis y calcula los rankings Top-N.
package agregado

import (
	"cmp"
	"maps"
	"slices"

	"parcial1/internal/parse"
)

// Contadores acumula las métricas de interés a medida que se leen las entradas.
// Los mapas dan actualización O(1) amortizada por línea; el orden no importa
// hasta el reporte final, así que no se paga nada por mantenerlo.
type Contadores struct {
	Estados   map[int]int // status → cantidad
	Endpoints *Contador   // ruta → cantidad
	IPs       *Contador   // IP cliente → cantidad
	Usuarios  *Contador   // usuario autenticado → cantidad ("-" no cuenta)
	PorHora   [24]int     // hora del día → cantidad
	Total     int
}

// NuevoContadores devuelve unos Contadores vacíos, listos para Agregar.
func NuevoContadores() *Contadores { return NuevoContadoresParticionados(1) }

// NuevoContadoresParticionados es como NuevoContadores pero con las IPs (el
// único contador con millones de claves) repartidas en p partes, para que
// FusionarParalelo con el mismo p no tenga que recorrer claves ajenas.
func NuevoContadoresParticionados(p int) *Contadores {
	return &Contadores{
		Estados:   make(map[int]int),
		Endpoints: NuevoContador(),
		IPs:       NuevoContadorParticionado(p),
		Usuarios:  NuevoContador(),
	}
}

// Agregar suma una entrada a todos los contadores. Los strings de e pueden ser
// vistas temporales sobre el buffer del lector: Contador los clona al guardarlos.
func (c *Contadores) Agregar(e parse.Entrada) {
	c.Total++
	c.Estados[e.Estado]++
	c.Endpoints.Incrementar(e.Ruta)
	c.IPs.Incrementar(e.IP)
	if e.Usuario != "-" {
		c.Usuarios.Incrementar(e.Usuario)
	}
	c.PorHora[e.Fecha.Hour()]++
}

// FusionarParalelo suma todos los parciales en unos Contadores nuevos (la fase
// reduce del map-reduce), fusionando cada contador de claves string con p
// goroutines (ver FusionarClaves). Lo demás es de tamaño fijo y
// se suma secuencialmente.
func FusionarParalelo(parciales []*Contadores, p int) *Contadores {
	res := NuevoContadores()
	endpoints := make([]*Contador, len(parciales))
	ips := make([]*Contador, len(parciales))
	usuarios := make([]*Contador, len(parciales))
	for i, o := range parciales {
		res.Total += o.Total
		for k, v := range o.Estados {
			res.Estados[k] += v
		}
		for h, v := range o.PorHora {
			res.PorHora[h] += v
		}
		endpoints[i], ips[i], usuarios[i] = o.Endpoints, o.IPs, o.Usuarios
	}
	res.Endpoints = FusionarClaves(endpoints, 1) // pocas claves: no vale repartir
	res.IPs = FusionarClaves(ips, p)
	res.Usuarios = FusionarClaves(usuarios, 1)
	return res
}

// Igual informa si dos Contadores tienen exactamente los mismos conteos. No se
// puede usar reflect.DeepEqual: el orden interno de un Contador depende del
// orden en que llegaron las claves, que cambia entre recorridos.
func (c *Contadores) Igual(o *Contadores) bool {
	return c.Total == o.Total && c.PorHora == o.PorHora &&
		maps.Equal(c.Estados, o.Estados) &&
		maps.Equal(c.Endpoints.Mapa(), o.Endpoints.Mapa()) &&
		maps.Equal(c.IPs.Mapa(), o.IPs.Mapa()) &&
		maps.Equal(c.Usuarios.Mapa(), o.Usuarios.Mapa())
}

// HoraPico devuelve la hora (0..23) con más solicitudes.
func (c *Contadores) HoraPico() int { return HoraPico(c.PorHora) }

// HoraPico devuelve la hora (0..23) con más solicitudes en un histograma
// horario; a igualdad, la más temprana. La usan tanto el analizador como el
// generador del ground truth, para que las dos definiciones no puedan divergir.
func HoraPico(porHora [24]int) int {
	pico := 0
	for h := 1; h < len(porHora); h++ {
		if porHora[h] > porHora[pico] {
			pico = h
		}
	}
	return pico
}

// Par es una clave con su conteo. Las etiquetas JSON son las del
// ground-truth.json, así los rankings del generador y del analizador son del
// mismo tipo y se comparan directamente.
type Par[K comparable] struct {
	Clave  K   `json:"clave"`
	Conteo int `json:"conteo"`
}

// CompararRanking define el orden del ranking, al estilo de cmp.Compare:
// negativo si a va antes que b. Va antes el de mayor conteo; a igualdad de
// conteo, la clave menor (desempate estable). TopNSort, TopNHeap y el generador
// del ground truth usan este único criterio, así que devuelven lo mismo.
func CompararRanking[K cmp.Ordered](a, b Par[K]) int {
	if c := cmp.Compare(b.Conteo, a.Conteo); c != 0 {
		return c
	}
	return cmp.Compare(a.Clave, b.Clave)
}

// peorQue indica si a va más abajo que b en el ranking.
func peorQue[K cmp.Ordered](a, b Par[K]) bool { return CompararRanking(a, b) > 0 }

// Metodo selecciona la estrategia de Top-N.
type Metodo int

// Estrategias de Top-N disponibles.
const (
	MetodoSort Metodo = iota // ordenar todo el mapa: O(u log u)
	MetodoHeap               // min-heap de tamaño n: O(u log n)
)

// TopN devuelve las n claves con mayor conteo, de mayor a menor, con la
// estrategia indicada. Las dos estrategias producen exactamente el mismo slice.
func TopN[K cmp.Ordered](f Fuente[K], n int, met Metodo) []Par[K] {
	if met == MetodoHeap {
		return TopNHeap(f, n)
	}
	return TopNSort(f, n)
}

// TopNSort vuelca el mapa a un slice y lo ordena entero.
//
// Complejidad O(u·log u) con u = claves únicas; espacio O(u). `slices.SortFunc`
// usa pdqsort (quicksort con detección de patrones y fallback a heapsort), que
// garantiza O(u·log u) también en el peor caso.
func TopNSort[K cmp.Ordered](f Fuente[K], n int) []Par[K] {
	pares := make([]Par[K], 0, f.Len())
	for k, v := range f.All() {
		pares = append(pares, Par[K]{Clave: k, Conteo: v})
	}
	slices.SortFunc(pares, CompararRanking[K])
	if n > len(pares) {
		n = len(pares)
	}
	return pares[:n]
}
