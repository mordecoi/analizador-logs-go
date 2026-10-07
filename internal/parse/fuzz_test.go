package parse_test

import (
	"testing"

	"parcial1/internal/parse"
)

// FuzzManualIgualQueRegex es un test diferencial: el motor de fuzzing de Go
// muta las líneas semilla y, para cada entrada, exige que el parser manual
// acepte todo lo que acepta la regex (la especificación de referencia) y que
// devuelva exactamente la misma Entrada.
//
// El manual puede ser más permisivo con líneas que la regex rechaza (p. ej. una
// hora 25:00), pero nunca puede rechazar ni interpretar distinto una línea válida.
//
//	go test ./internal/parse -run '^$' -fuzz FuzzManualIgualQueRegex -fuzztime 60s
func FuzzManualIgualQueRegex(f *testing.F) {
	semillas := []string{
		`148.251.175.245 - alex [02/Jan/2024:06:18:10 -0300] "POST /usr/login HTTP/1.1" 200 4823 "https://t.co/" "python-requests/2.31.0" 342`,
		`10.0.0.1 - - [01/Jan/2024:00:00:02 -0300] "GET /favicon.ico HTTP/1.1" 304 - "-" "curl/7.81.0" 5`,
		`::1 - bob [31/Dec/2023:23:59:59 +0530] "DELETE /api/cart HTTP/2.0" 503 0 "-" "Mozilla/5.0 (X11; Linux x86_64)" 1200`,
		`1.2.3.4 - - [15/Mar/2019:14:22:07 +0000] "GET /a%20b?q=1 HTTP/1.0" 404 12 "" "" 0`,
	}
	for _, s := range semillas {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, linea string) {
		r, errR := parse.Parsear(linea)
		if errR != nil {
			return // la regex la rechaza: el manual es libre de aceptarla o no
		}
		m, errM := parse.ParsearManual(linea)
		if errM != nil {
			t.Fatalf("la regex acepta y el manual rechaza (%v)\n  %q", errM, linea)
		}
		if r.IP != m.IP || r.Usuario != m.Usuario || r.Metodo != m.Metodo ||
			r.Ruta != m.Ruta || r.Proto != m.Proto || r.Estado != m.Estado ||
			r.Bytes != m.Bytes || r.Referrer != m.Referrer || r.UA != m.UA ||
			r.RespMs != m.RespMs || !r.Fecha.Equal(m.Fecha) {
			t.Fatalf("campos distintos\n  línea  %q\n  regex  %+v\n  manual %+v", linea, r, m)
		}
	})
}
