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
//
// La fecha se valida con su forma exacta (dd/Mmm/aaaa:hh:mm:ss ±hhmm) y no solo
// con time.Parse: time.Parse tolera el mes en cualquier capitalización ("DeC")
// y horas de un dígito, que Apache nunca escribe. El fuzzing diferencial
// (fuzz_test.go) encontró esa diferencia con ParsearManual, y también que `\S`
// dejaba pasar comillas dentro del request, que Apache siempre escapa.
var lineaRE = regexp.MustCompile(
	`^(\S+) \S+ (\S+) \[(\d{2}/[A-Z][a-z]{2}/\d{4}:\d{2}:\d{2}:\d{2} [+-]\d{4})\] "([^\s"]+) ([^\s"]+) ([^\s"]+)" (\d{3}) (\d+|-) "([^"]*)" "([^"]*)" (\d+)$`)

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
	bytes := atoiOCero(m[8])
	respMs := atoiOCero(m[11])
	return Entrada{
		IP: m[1], Usuario: m[2], Fecha: fecha,
		Metodo: m[4], Ruta: m[5], Proto: m[6],
		Estado: estado, Bytes: bytes,
		Referrer: m[9], UA: m[10], RespMs: respMs,
	}, nil
}

// atoiOCero convierte a entero y devuelve 0 si el campo no es numérico ("-")
// o no entra en un int. Sin esto, strconv.Atoi devuelve math.MaxInt ante un
// desborde y las dos implementaciones de parseo no coincidirían.
func atoiOCero(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
