// Package agregado acumula las métricas del análisis y calcula los rankings Top-N.
package agregado

import (
	"cmp"
	"slices"
	"strings"

	"parcial1/internal/parse"
)

// Contadores acumula las métricas de interés a medida que se leen las entradas.
// Los mapas dan actualización O(1) amortizada por línea; el orden no importa
// hasta el reporte final, así que no se paga nada por mantenerlo.
type Contadores struct {
	Estados   map[int]int    // status → cantidad
	Endpoints map[string]int // ruta → cantidad
	IPs       map[string]int // IP cliente → cantidad
	Usuarios  map[string]int // usuario autenticado → cantidad ("-" no cuenta)
	PorHora   [24]int        // hora del día → cantidad
	Total     int
}

func NuevoContadores() *Contadores {
	return &Contadores{
		Estados:   make(map[int]int),
		Endpoints: make(map[string]int),
		IPs:       make(map[string]int),
		Usuarios:  make(map[string]int),
	}
}

// incrementar suma 1, clonando la clave solo cuando se inserta por primera vez.
//
// Sin el clon, la clave guardada en el mapa es un sub-slice de la línea completa
// del log (el parser no copia) y la retiene entera: ~190 B vivos por cada clave
// única en vez de ~15. Medido con 10⁶ líneas: 98,6 MB retenidos sin clon vs
// ~20 MB con clon. El costo es una búsqueda extra de mapa por llamada.
func incrementar(m map[string]int, clave string) {
	if _, existe := m[clave]; !existe {
		clave = strings.Clone(clave)
	}
	m[clave]++
}

// Agregar suma una entrada a todos los contadores.
func (c *Contadores) Agregar(e parse.Entrada) {
	c.Total++
	c.Estados[e.Estado]++
	incrementar(c.Endpoints, e.Ruta)
	incrementar(c.IPs, e.IP)
	if e.Usuario != "-" {
		incrementar(c.Usuarios, e.Usuario)
	}
	c.PorHora[e.Fecha.Hour()]++
}

// Fusionar suma los contadores de otro (la fase "reduce" del map-reduce).
// Costo O(Σ claves de o); las claves de o ya son strings independientes
// (clonadas al insertar), así que moverlas a este mapa no retiene nada de más.
func (c *Contadores) Fusionar(o *Contadores) {
	c.Total += o.Total
	for k, v := range o.Estados {
		c.Estados[k] += v
	}
	for k, v := range o.Endpoints {
		c.Endpoints[k] += v
	}
	for k, v := range o.IPs {
		c.IPs[k] += v
	}
	for k, v := range o.Usuarios {
		c.Usuarios[k] += v
	}
	for h, v := range o.PorHora {
		c.PorHora[h] += v
	}
}

// HoraPico devuelve la hora (0..23) con más solicitudes.
func (c *Contadores) HoraPico() int {
	pico := 0
	for h := 1; h < 24; h++ {
		if c.PorHora[h] > c.PorHora[pico] {
			pico = h
		}
	}
	return pico
}

// Par es una clave con su conteo.
type Par[K comparable] struct {
	Clave  K
	Conteo int
}

// peorQue define el orden del ranking: un par va más abajo si tiene menos
// conteo; a igualdad de conteo, la clave mayor va más abajo (desempate estable).
// Tanto TopNSort como TopNHeap respetan este orden, así que devuelven lo mismo.
func peorQue[K cmp.Ordered](a, b Par[K]) bool {
	if a.Conteo != b.Conteo {
		return a.Conteo < b.Conteo
	}
	return a.Clave > b.Clave
}

// Metodo selecciona la estrategia de Top-N.
type Metodo int

const (
	MetodoSort Metodo = iota // ordenar todo el mapa: O(u log u)
	MetodoHeap               // min-heap de tamaño n: O(u log n)
)

// TopN devuelve las n claves con mayor conteo, de mayor a menor, con la
// estrategia indicada. Las dos estrategias producen exactamente el mismo slice.
func TopN[K cmp.Ordered](m map[K]int, n int, met Metodo) []Par[K] {
	if met == MetodoHeap {
		return TopNHeap(m, n)
	}
	return TopNSort(m, n)
}

// TopNSort vuelca el mapa a un slice y lo ordena entero.
//
// Complejidad O(u·log u) con u = claves únicas; espacio O(u). `slices.SortFunc`
// usa pdqsort (quicksort con detección de patrones y fallback a heapsort), que
// garantiza O(u·log u) también en el peor caso.
func TopNSort[K cmp.Ordered](m map[K]int, n int) []Par[K] {
	pares := make([]Par[K], 0, len(m))
	for k, v := range m {
		pares = append(pares, Par[K]{Clave: k, Conteo: v})
	}
	slices.SortFunc(pares, func(a, b Par[K]) int {
		switch {
		case peorQue(a, b):
			return 1
		case peorQue(b, a):
			return -1
		default:
			return 0
		}
	})
	if n > len(pares) {
		n = len(pares)
	}
	return pares[:n]
}
