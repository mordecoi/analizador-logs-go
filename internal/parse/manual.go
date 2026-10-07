package parse

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// meses traduce el mes abreviado del log a time.Month.
var meses = map[string]time.Month{
	"Jan": time.January, "Feb": time.February, "Mar": time.March,
	"Apr": time.April, "May": time.May, "Jun": time.June,
	"Jul": time.July, "Aug": time.August, "Sep": time.September,
	"Oct": time.October, "Nov": time.November, "Dec": time.December,
}

// zonasOffset precalcula una *time.Location por cada offset de cuarto de hora
// entre -12:00 y +14:00, para no asignar una zona en cada línea. Todas las
// líneas de un log real comparten el mismo offset (el del servidor).
var zonasOffset = func() map[int]*time.Location {
	m := make(map[int]*time.Location, 128)
	for minutos := -12 * 60; minutos <= 14*60; minutos += 15 {
		nombre := fmt.Sprintf("%+03d%02d", minutos/60, abs(minutos)%60)
		m[minutos] = time.FixedZone(nombre, minutos*60)
	}
	return m
}()

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// atoiSimple convierte una cadena de dígitos a entero sin asignar ni devolver
// un error con formato (a diferencia de strconv.Atoi). Rechaza la cadena vacía,
// cualquier byte que no sea dígito y los números que no entran en un int (sin
// el chequeo de desborde, un campo de 20 dígitos daba un valor negativo: lo
// encontró el fuzzing).
func atoiSimple(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		d := int(c - '0')
		if n > (math.MaxInt-d)/10 {
			return 0, false
		}
		n = n*10 + d
	}
	return n, true
}

// cursor recorre una línea campo por campo con error "pegajoso": la primera
// lectura que falla deja ok en false y todas las siguientes devuelven "" sin
// hacer nada. Así el parser se lee como la secuencia de campos del formato y el
// error se chequea una sola vez al final, en lugar de después de cada campo
// (es el mismo patrón que usan bufio.Scanner y encoding/binary).
type cursor struct {
	resto string
	ok    bool
}

// hasta devuelve la porción del resto hasta el próximo byte sep (sin incluirlo)
// y avanza más allá del separador. Es un sub-slice: no copia.
func (c *cursor) hasta(sep byte) string {
	if !c.ok {
		return ""
	}
	i := strings.IndexByte(c.resto, sep)
	if i < 0 {
		c.ok = false
		return ""
	}
	tok := c.resto[:i]
	c.resto = c.resto[i+1:]
	return tok
}

// saltar consume el byte b si es el próximo; si no, marca el error.
func (c *cursor) saltar(b byte) {
	if !c.ok {
		return
	}
	if c.resto == "" || c.resto[0] != b {
		c.ok = false
		return
	}
	c.resto = c.resto[1:]
}

// ParsearManual interpreta la misma línea que Parsear pero recorriéndola una vez
// con strings.IndexByte. Para toda línea que acepta Parsear devuelve exactamente
// el mismo Entrada (lo verifica FuzzManualIgualQueRegex).
//
// Diferencias con la versión regex:
//   - Los campos son sub-slices del string de entrada: no asigna un []string de
//     submatches por línea.
//   - La fecha se arma por posiciones fijas + time.Date, sin time.Parse.
//   - Las zonas horarias están precalculadas (zonasOffset).
//
// Limitación (compartida con la regex `[^"]*`): asume que referrer y user agent
// no contienen comillas sin escapar. Los logs que generamos lo cumplen.
func ParsearManual(linea string) (Entrada, error) {
	var e Entrada
	c := cursor{resto: linea, ok: true}

	e.IP = c.hasta(' ')
	c.hasta(' ') // remote logname, siempre "-"
	e.Usuario = c.hasta(' ')

	c.saltar('[')
	fechaStr := c.hasta(']')

	c.saltar(' ')
	c.saltar('"')
	e.Metodo = c.hasta(' ')
	req := c.hasta('"') // "RUTA PROTO"
	if b := strings.LastIndexByte(req, ' '); b >= 0 {
		e.Ruta, e.Proto = req[:b], req[b+1:]
	} else {
		e.Ruta = req
	}

	c.saltar(' ')
	estStr := c.hasta(' ')
	bytesStr := c.hasta(' ')

	c.saltar('"')
	e.Referrer = c.hasta('"')
	c.saltar(' ')
	c.saltar('"')
	e.UA = c.hasta('"')

	if !c.ok {
		return Entrada{}, fmt.Errorf("línea no reconocida: %q", linea)
	}

	var ok bool
	if e.Estado, ok = atoiSimple(estStr); !ok {
		return Entrada{}, fmt.Errorf("estado inválido %q", estStr)
	}
	e.Bytes, _ = atoiSimple(bytesStr) // "-" (sin contenido) → 0, como la regex
	e.RespMs, _ = atoiSimple(strings.TrimSpace(c.resto))

	fecha, err := parsearFecha(fechaStr)
	if err != nil {
		return Entrada{}, err
	}
	e.Fecha = fecha
	return e, nil
}

// parsearFecha interpreta "02/Jan/2006:15:04:05 -0700" por posiciones fijas.
func parsearFecha(s string) (time.Time, error) {
	if len(s) != len(LayoutApache) {
		return time.Time{}, fmt.Errorf("fecha inválida %q", s)
	}
	dia, ok1 := atoiSimple(s[0:2])
	mes, ok2 := meses[s[3:6]]
	anio, ok3 := atoiSimple(s[7:11])
	hh, ok4 := atoiSimple(s[12:14])
	mm, ok5 := atoiSimple(s[15:17])
	ss, ok6 := atoiSimple(s[18:20])
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 {
		return time.Time{}, fmt.Errorf("fecha inválida %q", s)
	}
	loc, err := zonaDe(s[21:26])
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(anio, mes, dia, hh, mm, ss, 0, loc), nil
}

// zonaDe traduce "±HHMM" a una *time.Location, usando la tabla precalculada.
func zonaDe(off string) (*time.Location, error) {
	if len(off) != 5 || (off[0] != '+' && off[0] != '-') {
		return nil, fmt.Errorf("offset inválido %q", off)
	}
	hh, ok1 := atoiSimple(off[1:3])
	mm, ok2 := atoiSimple(off[3:5])
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("offset inválido %q", off)
	}
	minutos := hh*60 + mm
	if off[0] == '-' {
		minutos = -minutos
	}
	if loc, ok := zonasOffset[minutos]; ok {
		return loc, nil
	}
	return time.FixedZone(off, minutos*60), nil // offset no alineado a cuarto de hora
}
