package agregado

import (
	"bytes"
	"encoding/gob"
	"errors"
	"hash/maphash"
	"iter"
	"maps"
	"runtime"
	"strings"
	"sync"
)

// Contador cuenta apariciones por clave string sin retener memoria de la
// entrada: cada parte guarda clave → índice en un mapa y los conteos en un slice.
//
// Por qué no un map[string]int: en Go, asignar sobre una clave string que ya
// existe (m[k]++) reemplaza la clave guardada por k (el runtime actualiza la
// clave en cada asignación). Clonar solo al insertar, como hacía el primer
// parcial, no alcanza: la clave vuelve a apuntar a la última línea donde
// apareció. Con strings inmutables eso solo retenía memoria; con el lector por
// lotes, que reutiliza su buffer, corrompía las claves. Acá el mapa se escribe
// una sola vez por clave (ya clonada) y los incrementos solo lo leen.
//
// De yapa, incrementar una clave existente cuesta un hash en lugar de dos
// (antes: una búsqueda para saber si existía y otra para asignar).
//
// Un Contador tiene una sola parte mientras se cuenta. FusionarParalelo
// devuelve uno con varias partes de claves disjuntas (particionadas por hash),
// cada una construida por su propia goroutine.
type Contador struct {
	partes []*parte
}

// parte es un contador simple: las claves de una partición.
type parte struct {
	indice  map[string]int // clave → posición en conteos
	conteos []int
}

func nuevaParte(capacidad int) *parte {
	return &parte{indice: make(map[string]int, capacidad), conteos: make([]int, 0, capacidad)}
}

func (p *parte) sumar(clave string, n int, clonar bool) {
	if i, ok := p.indice[clave]; ok {
		p.conteos[i] += n
		return
	}
	if clonar {
		clave = strings.Clone(clave)
	}
	p.indice[clave] = len(p.conteos)
	p.conteos = append(p.conteos, n)
}

// semilla del hash que decide a qué partición va cada clave.
var semilla = maphash.MakeSeed()

func particionDe(clave string, n int) int {
	return int(maphash.String(semilla, clave) % uint64(n))
}

// NuevoContador devuelve un Contador vacío.
func NuevoContador() *Contador { return NuevoContadorParticionado(1) }

// NuevoContadorParticionado devuelve un Contador vacío cuyas claves se reparten
// en p partes por hash desde la primera inserción. Cuesta un hash extra por
// Incrementar, pero permite que FusionarClaves sume la parte g de todos los
// contadores sin mirar las claves de las demás partes.
func NuevoContadorParticionado(p int) *Contador {
	c := &Contador{partes: make([]*parte, max(p, 1))}
	for i := range c.partes {
		c.partes[i] = nuevaParte(0)
	}
	return c
}

// parteDe devuelve la parte donde vive (o viviría) la clave.
func (c *Contador) parteDe(clave string) *parte {
	if len(c.partes) == 1 {
		return c.partes[0] // el caso de los workers: sin costo de hash extra
	}
	return c.partes[particionDe(clave, len(c.partes))]
}

// Incrementar suma 1 a la clave. La clave puede ser una vista temporal sobre
// un buffer: si es nueva se clona, y nunca se guarda tal cual.
func (c *Contador) Incrementar(clave string) { c.parteDe(clave).sumar(clave, 1, true) }

// FusionarClaves suma varios contadores en uno nuevo usando p goroutines.
//
// Es la fase reduce del map-reduce hecha en paralelo: la goroutine g arma la
// parte g del resultado con las claves cuyo hash cae en la partición g. Las
// particiones son disjuntas, así que las goroutines no comparten nada que se
// escriba. Si los contadores ya vienen particionados en p partes (los workers
// del pipeline cuentan así), la goroutine g lee solo la parte g de cada uno;
// si no, recorre todas las claves y se queda con las suyas.
//
// Con u ≈ 2·10⁶ IPs distintas (5 M de líneas), la fusión secuencial tardaba
// ~1,7 s: más que los 16 bloques en paralelo juntos (lo mostró la traza de
// Jaeger).
func FusionarClaves(contadores []*Contador, p int) *Contador {
	p = max(p, 1)
	total := 0
	alineados := true
	for _, c := range contadores {
		total += c.Len()
		alineados = alineados && len(c.partes) == p
	}
	res := &Contador{partes: make([]*parte, p)}
	var wg sync.WaitGroup
	for g := range p {
		wg.Go(func() {
			destino := nuevaParte(total / p)
			for _, c := range contadores {
				if alineados {
					destino.fusionar(c.partes[g])
				} else {
					destino.fusionarParticion(c, g, p)
				}
			}
			res.partes[g] = destino
		})
	}
	wg.Wait()
	return res
}

// fusionar suma todas las claves de o (de la misma partición que p).
func (p *parte) fusionar(o *parte) {
	for k, i := range o.indice {
		p.sumar(k, o.conteos[i], false)
	}
}

// fusionarParticion suma las claves de c que caen en la partición g de n.
func (p *parte) fusionarParticion(c *Contador, g, n int) {
	for k, v := range c.All() {
		if n == 1 || particionDe(k, n) == g {
			p.sumar(k, v, false)
		}
	}
}

// Valor devuelve el conteo de la clave (0 si no apareció).
func (c *Contador) Valor(clave string) int {
	p := c.parteDe(clave)
	if i, ok := p.indice[clave]; ok {
		return p.conteos[i]
	}
	return 0
}

// Len devuelve la cantidad de claves distintas.
func (c *Contador) Len() int {
	n := 0
	for _, p := range c.partes {
		n += len(p.conteos)
	}
	return n
}

// All recorre los pares (clave, conteo) en orden no especificado.
func (c *Contador) All() iter.Seq2[string, int] {
	return func(yield func(string, int) bool) {
		for _, p := range c.partes {
			for k, i := range p.indice {
				if !yield(k, p.conteos[i]) {
					return
				}
			}
		}
	}
}

// Mapa devuelve una copia de los conteos como map, para comparar y serializar.
func (c *Contador) Mapa() map[string]int {
	return maps.Collect(c.All())
}

// serializado es la forma en disco de un Contador: dos slices paralelos.
// Gob decodifica slices mucho más rápido que un map (no tiene que hashear ni
// insertar clave por clave), y el índice se reconstruye una sola vez, ya con
// su tamaño final. Serializar un map costaba el doble a 20 M de líneas: un map
// que armaba gob y otro, el índice, copiado de ese.
type serializado struct {
	Claves  []string
	Conteos []int
}

// GobEncode serializa el Contador (la partición no se guarda).
func (c *Contador) GobEncode() ([]byte, error) {
	sz := serializado{Claves: make([]string, 0, c.Len()), Conteos: make([]int, 0, c.Len())}
	for k, v := range c.All() {
		sz.Claves = append(sz.Claves, k)
		sz.Conteos = append(sz.Conteos, v)
	}
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(sz)
	return buf.Bytes(), err
}

// GobDecode reconstruye el Contador a partir de lo que escribió GobEncode.
func (c *Contador) GobDecode(datos []byte) error {
	var sz serializado
	if err := gob.NewDecoder(bytes.NewReader(datos)).Decode(&sz); err != nil {
		return err
	}
	if len(sz.Claves) != len(sz.Conteos) {
		return errors.New("contador serializado inconsistente")
	}
	c.partes = indexarEnParalelo(sz.Claves, sz.Conteos)
	return nil
}

// minClavesParalelo es a partir de cuántas claves conviene repartir el armado
// del índice entre goroutines (por debajo, el costo de lanzarlas no se paga).
const minClavesParalelo = 1 << 16

// indexarEnParalelo arma las partes de un Contador a partir de claves y conteos
// paralelos, con una goroutine por parte: la g recorre todas las claves y se
// queda con las de su partición. Recalcular la partición es necesario porque la
// semilla del hash cambia entre procesos. Insertar en un map es lo caro de
// cargar el cache (8 M de IPs a 20 M de líneas), y así se reparte entre núcleos.
func indexarEnParalelo(claves []string, conteos []int) []*parte {
	p := runtime.GOMAXPROCS(0)
	if len(claves) < minClavesParalelo {
		p = 1
	}
	partes := make([]*parte, p)
	var wg sync.WaitGroup
	for g := range p {
		wg.Go(func() {
			parte := nuevaParte(len(claves) / p)
			for i, k := range claves {
				if p == 1 || particionDe(k, p) == g {
					parte.sumar(k, conteos[i], false)
				}
			}
			partes[g] = parte
		})
	}
	wg.Wait()
	return partes
}

// Fuente es cualquier colección de conteos por clave sobre la que se puede
// calcular un Top-N: un *Contador o un Mapa.
type Fuente[K comparable] interface {
	Len() int
	All() iter.Seq2[K, int]
}

// Mapa adapta un map[K]int común a Fuente.
type Mapa[K comparable] map[K]int

// Len devuelve la cantidad de claves.
func (m Mapa[K]) Len() int { return len(m) }

// All recorre los pares (clave, conteo).
func (m Mapa[K]) All() iter.Seq2[K, int] { return maps.All(m) }
