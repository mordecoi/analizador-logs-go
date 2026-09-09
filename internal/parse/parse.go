// Package parse interpreta una línea de log en formato Apache Combined
// (con el campo extra de tiempo de respuesta al final).
package parse

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// LayoutApache es el formato de fecha del log.
const LayoutApache = "02/Jan/2006:15:04:05 -0700"

// Entrada es una línea de log ya interpretada.
type Entrada struct {
	IP       string
	Usuario  string
	Fecha    time.Time
	Metodo   string
	Ruta     string
	Proto    string
	Estado   int
	Bytes    int
	Referrer string
	UA       string
	RespMs   int
}

// lineaRE reconoce:
//
//	IP - usuario [fecha] "METODO RUTA PROTO" estado bytes "referrer" "ua" respMs
var lineaRE = regexp.MustCompile(
	`^(\S+) \S+ (\S+) \[([^\]]+)\] "(\S+) (\S+) (\S+)" (\d{3}) (\S+) "([^"]*)" "([^"]*)" (\d+)$`)

// Parsear interpreta una línea con expresión regular. Es la versión legible y
// directa que usamos como baseline.
//
// Costo conocido (a exponer en el perfil): FindStringSubmatch asigna un []string
// de 12 elementos por línea, y time.Parse resuelve el offset horario en cada
// llamada. Para millones de líneas eso domina el tiempo de CPU y presiona al GC.
func Parsear(linea string) (Entrada, error) {
	m := lineaRE.FindStringSubmatch(linea)
	if m == nil {
		return Entrada{}, fmt.Errorf("línea no reconocida: %q", linea)
	}
	fecha, err := time.Parse(LayoutApache, m[3])
	if err != nil {
		return Entrada{}, fmt.Errorf("fecha inválida %q: %w", m[3], err)
	}
	estado, err := strconv.Atoi(m[7])
	if err != nil {
		return Entrada{}, fmt.Errorf("estado inválido %q: %w", m[7], err)
	}
	bytes, _ := strconv.Atoi(m[8])
	respMs, _ := strconv.Atoi(m[11])
	return Entrada{
		IP: m[1], Usuario: m[2], Fecha: fecha,
		Metodo: m[4], Ruta: m[5], Proto: m[6],
		Estado: estado, Bytes: bytes,
		Referrer: m[9], UA: m[10], RespMs: respMs,
	}, nil
}
