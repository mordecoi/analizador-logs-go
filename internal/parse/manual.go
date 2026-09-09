package parse

import (
	"fmt"
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
	for min := -12 * 60; min <= 14*60; min += 15 {
		nombre := fmt.Sprintf("%+03d%02d", min/60, abs(min)%60)
		m[min] = time.FixedZone(nombre, min*60)
	}
	return m
}()

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// campo devuelve la porción de *s hasta el próximo byte sep (sin incluirlo) y
// avanza *s más allá del separador. Es un sub-slice: no copia.
func campo(s *string, sep byte) (string, bool) {
	i := strings.IndexByte(*s, sep)
	if i < 0 {
		return "", false
	}
	tok := (*s)[:i]
	*s = (*s)[i+1:]
	return tok, true
}

// atoiSimple convierte una cadena de dígitos a entero sin asignar ni devolver
// un error con formato (a diferencia de strconv.Atoi).
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
		n = n*10 + int(c-'0')
	}
	return n, true
}

// ParsearManual interpreta la misma línea que Parsear pero recorriéndola una vez
// con strings.IndexByte. Devuelve exactamente el mismo Entrada.
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
	s := linea
	falla := func() (Entrada, error) {
		return Entrada{}, fmt.Errorf("línea no reconocida: %q", linea)
	}

	var ok bool
	if e.IP, ok = campo(&s, ' '); !ok {
		return falla()
	}
	if _, ok = campo(&s, ' '); !ok { // remote logname, siempre "-"
		return falla()
	}
	if e.Usuario, ok = campo(&s, ' '); !ok {
		return falla()
	}

	// fecha entre corchetes
	if len(s) == 0 || s[0] != '[' {
		return falla()
	}
	s = s[1:]
	fechaStr, ok := campo(&s, ']')
	if !ok {
		return falla()
	}
	fecha, err := parsearFecha(fechaStr)
	if err != nil {
		return Entrada{}, err
	}
	e.Fecha = fecha

	// línea de request entre comillas:  ` "METODO RUTA PROTO" `
	if _, ok = campo(&s, '"'); !ok {
		return falla()
	}
	req, ok := campo(&s, '"')
	if !ok {
		return falla()
	}
	if e.Metodo, ok = campo(&req, ' '); !ok {
		return falla()
	}
	if b := strings.LastIndexByte(req, ' '); b >= 0 {
		e.Ruta, e.Proto = req[:b], req[b+1:]
	} else {
		e.Ruta = req
	}

	// ` estado bytes `
	s = strings.TrimPrefix(s, " ")
	estStr, ok := campo(&s, ' ')
	if !ok {
		return falla()
	}
	if e.Estado, ok = atoiSimple(estStr); !ok {
		return Entrada{}, fmt.Errorf("estado inválido %q", estStr)
	}
	bytesStr, ok := campo(&s, ' ')
	if !ok {
		return falla()
	}
	e.Bytes, _ = atoiSimple(bytesStr) // "-" (sin contenido) → 0, como la regex

	// referrer y user agent, cada uno entre comillas
	if _, ok = campo(&s, '"'); !ok {
		return falla()
	}
	if e.Referrer, ok = campo(&s, '"'); !ok {
		return falla()
	}
	if _, ok = campo(&s, '"'); !ok {
		return falla()
	}
	if e.UA, ok = campo(&s, '"'); !ok {
		return falla()
	}

	e.RespMs, _ = atoiSimple(strings.TrimSpace(s))
	return e, nil
}

// parsearFecha interpreta "02/Jan/2006:15:04:05 -0700" por posiciones fijas.
func parsearFecha(s string) (time.Time, error) {
	if len(s) != 26 {
		return time.Time{}, fmt.Errorf("fecha inválida %q", s)
	}
	dia, ok1 := atoiSimple(s[0:2])
	mes, ok2 := meses[s[3:6]]
	anio, ok3 := atoiSimple(s[7:11])
	hh, ok4 := atoiSimple(s[12:14])
	mm, ok5 := atoiSimple(s[15:17])
	ss, ok6 := atoiSimple(s[18:20])
	if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6) {
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
	min := hh*60 + mm
	if off[0] == '-' {
		min = -min
	}
	if loc, ok := zonasOffset[min]; ok {
		return loc, nil
	}
	return time.FixedZone(off, min*60), nil // offset no alineado a cuarto de hora
}
