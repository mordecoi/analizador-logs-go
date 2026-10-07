package agregado

import (
	"cmp"
	"container/heap"
)

// monticuloMin es un min-heap acotado: la raíz es siempre el "peor" par según
// peorQue (menor conteo; a igualdad, clave mayor). Se mantiene con a lo sumo n
// elementos, así que al recorrer las u claves del mapa siempre queda el Top-n.
type monticuloMin[K cmp.Ordered] []Par[K]

func (h monticuloMin[K]) Len() int           { return len(h) }
func (h monticuloMin[K]) Less(i, j int) bool { return peorQue(h[i], h[j]) }
func (h monticuloMin[K]) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *monticuloMin[K]) Push(x any) { *h = append(*h, x.(Par[K])) }

func (h *monticuloMin[K]) Pop() any {
	viejo := *h
	n := len(viejo)
	p := viejo[n-1]
	*h = viejo[:n-1]
	return p
}

// TopNHeap recorre las u claves manteniendo un min-heap de a lo sumo n
// elementos: si el heap no está lleno, inserta (O(log n)); si está lleno y la
// clave nueva es mejor que la raíz, la reemplaza y reordena (O(log n)); si no,
// la descarta (O(1)).
//
// Complejidad O(u·log n); espacio O(n). Frente a TopNSort (O(u·log u), espacio
// O(u)) conviene cuando n ≪ u — el caso del ranking de IPs, donde u ≈ millones.
func TopNHeap[K cmp.Ordered](f Fuente[K], n int) []Par[K] {
	if n <= 0 {
		return nil
	}
	h := make(monticuloMin[K], 0, n+1)
	for k, v := range f.All() {
		nuevo := Par[K]{Clave: k, Conteo: v}
		if h.Len() < n {
			heap.Push(&h, nuevo)
			continue
		}
		if peorQue(h[0], nuevo) { // nuevo mejor que la raíz (el peor del Top-n)
			h[0] = nuevo
			heap.Fix(&h, 0)
		}
	}

	// Extraer de la raíz hacia afuera da orden ascendente (peor primero);
	// se llena el resultado desde el final para dejarlo descendente.
	res := make([]Par[K], h.Len())
	for i := len(res) - 1; i >= 0; i-- {
		res[i] = heap.Pop(&h).(Par[K])
	}
	return res
}
