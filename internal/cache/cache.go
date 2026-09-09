// Package cache implementa las dos estrategias de reutilización de resultados
// del parcial, que el enunciado pide diferenciar:
//
//   - Memoización (Guardar/Cargar): los Contadores completos son una función
//     pura del archivo de log — mismo archivo, mismos agregados. Se serializan
//     a disco con gob junto con los metadatos del log (tamaño + mtime); una
//     segunda corrida sobre el mismo archivo los deserializa en vez de
//     re-parsear. No hay política de expiración: si el archivo no cambió, el
//     resultado sigue siendo válido por definición.
//
//   - Cache de consultas (Consultas): las respuestas Top-N por (métrica, n),
//     con una marca de sucio. Acá sí hay invalidación: si entran datos nuevos,
//     las respuestas guardadas quedan obsoletas y la próxima consulta recalcula.
package cache

import (
	"bufio"
	"encoding/gob"
	"fmt"
	"os"

	"parcial1/internal/agregado"
	"parcial1/internal/parse"
)

// versionCache invalida caches viejos si cambia el formato de Contadores.
const versionCache = 1

// archivoCache es lo que se serializa: los agregados más los metadatos del log
// con el que se calcularon, para poder detectar que el log cambió.
type archivoCache struct {
	Version int
	Tamano  int64
	ModTime int64 // UnixNano del mtime del log
	Cont    *agregado.Contadores
}

// RutaCache devuelve dónde se guarda la memoización para un log dado.
func RutaCache(rutaLog string) string { return rutaLog + ".agregados.gob" }

// Cargar devuelve los agregados memoizados para el log, o nil y el motivo del
// miss ("sin cache previo", "el log cambió", etc.).
func Cargar(rutaLog string) (*agregado.Contadores, string) {
	info, err := os.Stat(rutaLog)
	if err != nil {
		return nil, "no se pudo leer el log"
	}
	f, err := os.Open(RutaCache(rutaLog))
	if err != nil {
		return nil, "sin cache previo"
	}
	defer f.Close()

	var ac archivoCache
	if err := gob.NewDecoder(bufio.NewReaderSize(f, 1<<20)).Decode(&ac); err != nil {
		return nil, "cache corrupto"
	}
	if ac.Version != versionCache {
		return nil, "versión de cache distinta"
	}
	if ac.Tamano != info.Size() || ac.ModTime != info.ModTime().UnixNano() {
		return nil, "el log cambió desde que se calculó"
	}
	return ac.Cont, ""
}

// Guardar memoiza los agregados calculados para el log en ruta.
func Guardar(rutaLog string, c *agregado.Contadores) error {
	info, err := os.Stat(rutaLog)
	if err != nil {
		return fmt.Errorf("cache: %w", err)
	}
	f, err := os.Create(RutaCache(rutaLog))
	if err != nil {
		return fmt.Errorf("cache: %w", err)
	}
	defer f.Close()

	bw := bufio.NewWriterSize(f, 1<<20)
	err = gob.NewEncoder(bw).Encode(archivoCache{
		Version: versionCache,
		Tamano:  info.Size(),
		ModTime: info.ModTime().UnixNano(),
		Cont:    c,
	})
	if err != nil {
		return fmt.Errorf("cache: %w", err)
	}
	return bw.Flush()
}

// Consultas envuelve unos Contadores y cachea las respuestas Top-N.
//
// Qué se guarda: el slice de resultados por clave "métrica:n".
// Cuándo se reutiliza: en consultas repetidas con la misma métrica y n
// (p. ej. el reporte y la validación piden los mismos rankings).
// Invalidación: Agregar marca los datos como sucios; la próxima consulta
// descarta todo lo cacheado y recalcula.
type Consultas struct {
	Cont *agregado.Contadores

	met    agregado.Metodo
	topStr map[string][]agregado.Par[string]
	topInt map[string][]agregado.Par[int]
	sucio  bool

	Hits, Misses int
}

func NuevasConsultas(c *agregado.Contadores, met agregado.Metodo) *Consultas {
	return &Consultas{
		Cont:   c,
		met:    met,
		topStr: make(map[string][]agregado.Par[string]),
		topInt: make(map[string][]agregado.Par[int]),
	}
}

// Agregar suma una entrada y marca como sucio: los rankings cacheados ya no
// reflejan los datos (camino para uso incremental / streaming).
func (q *Consultas) Agregar(e parse.Entrada) {
	q.Cont.Agregar(e)
	q.sucio = true
}

// limpiarSiSucio descarta el cache si entraron datos desde la última consulta.
func (q *Consultas) limpiarSiSucio() {
	if q.sucio {
		clear(q.topStr)
		clear(q.topInt)
		q.sucio = false
	}
}

func (q *Consultas) topDeStr(clave string, m map[string]int, n int) []agregado.Par[string] {
	q.limpiarSiSucio()
	k := fmt.Sprintf("%s:%d", clave, n)
	if v, ok := q.topStr[k]; ok {
		q.Hits++
		return v
	}
	q.Misses++
	v := agregado.TopN(m, n, q.met)
	q.topStr[k] = v
	return v
}

func (q *Consultas) Endpoints(n int) []agregado.Par[string] {
	return q.topDeStr("endpoints", q.Cont.Endpoints, n)
}

func (q *Consultas) IPs(n int) []agregado.Par[string] {
	return q.topDeStr("ips", q.Cont.IPs, n)
}

func (q *Consultas) Usuarios(n int) []agregado.Par[string] {
	return q.topDeStr("usuarios", q.Cont.Usuarios, n)
}

func (q *Consultas) Estados(n int) []agregado.Par[int] {
	q.limpiarSiSucio()
	k := fmt.Sprintf("estados:%d", n)
	if v, ok := q.topInt[k]; ok {
		q.Hits++
		return v
	}
	q.Misses++
	v := agregado.TopN(q.Cont.Estados, n, q.met)
	q.topInt[k] = v
	return v
}
