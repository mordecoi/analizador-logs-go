package anomalias_test

import (
	"bufio"
	"bytes"
	"fmt"
	"testing"
	"time"

	"parcial1/internal/anomalias"
	"parcial1/internal/loggen"
	"parcial1/internal/parse"
)

var base = time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

func entrada(ip, ruta string, estado int, t time.Time) parse.Entrada {
	return parse.Entrada{IP: ip, Ruta: ruta, Estado: estado, Fecha: t}
}

// fondo genera tráfico normal esparcido para que las franjas de 5xx tengan una
// media contra la cual comparar.
func fondo(d *anomalias.Detector, horas int) {
	for h := 0; h < horas; h++ {
		for i := 0; i < 10; i++ {
			t := base.Add(time.Duration(h)*time.Hour + time.Duration(i)*5*time.Minute)
			d.Procesar(entrada(fmt.Sprintf("10.0.%d.%d", h, i), "/", 200, t))
			if i%3 == 0 {
				d.Procesar(entrada(fmt.Sprintf("10.1.%d.%d", h, i), "/api/x", 500, t))
			}
		}
	}
}

func TestDetectaRafaga(t *testing.T) {
	d := anomalias.NuevoDetector(anomalias.ConfigPorDefecto())
	fondo(d, 4)
	// 200 solicitudes de una IP en 20 segundos
	for i := 0; i < 200; i++ {
		d.Procesar(entrada("6.6.6.6", "/usr/login", 200, base.Add(time.Duration(i)*100*time.Millisecond)))
	}
	hs := d.Hallazgos()
	var rafagas []anomalias.Hallazgo
	for _, h := range hs {
		if h.Tipo == "rafaga" {
			rafagas = append(rafagas, h)
		}
	}
	if len(rafagas) != 1 {
		t.Fatalf("esperaba 1 ráfaga, hubo %d: %+v", len(rafagas), rafagas)
	}
	if rafagas[0].IP != "6.6.6.6" || rafagas[0].Cantidad < 150 {
		t.Errorf("ráfaga rara: %+v", rafagas[0])
	}
}

func TestNoDetectaTraficoNormal(t *testing.T) {
	d := anomalias.NuevoDetector(anomalias.ConfigPorDefecto())
	fondo(d, 6)
	// una IP activa pero razonable: 1 solicitud cada 4 segundos durante una hora
	for i := 0; i < 900; i++ {
		d.Procesar(entrada("7.7.7.7", "/api/products", 200, base.Add(time.Duration(i)*4*time.Second)))
	}
	if hs := d.Hallazgos(); len(hs) != 0 {
		t.Fatalf("falsos positivos: %+v", hs)
	}
}

func TestDetectaEscaneo(t *testing.T) {
	d := anomalias.NuevoDetector(anomalias.ConfigPorDefecto())
	fondo(d, 4)
	// una IP recorre 30 rutas inexistentes en 2 minutos
	for i := 0; i < 30; i++ {
		d.Procesar(entrada("9.9.9.9", fmt.Sprintf("/probe/%d", i), 404, base.Add(time.Duration(i)*4*time.Second)))
	}
	hs := d.Hallazgos()
	var escaneos []anomalias.Hallazgo
	for _, h := range hs {
		if h.Tipo == "escaneo" {
			escaneos = append(escaneos, h)
		}
	}
	if len(escaneos) != 1 || escaneos[0].IP != "9.9.9.9" {
		t.Fatalf("esperaba 1 escaneo de 9.9.9.9: %+v", escaneos)
	}
}

func TestDetectaPico5xx(t *testing.T) {
	d := anomalias.NuevoDetector(anomalias.ConfigPorDefecto())
	fondo(d, 8) // ~40 5xx/h de fondo... fondo() mete ~4 por hora
	// franja con 300 errores 500 concentrados en /api/checkout
	pico := base.Add(3 * time.Hour)
	for i := 0; i < 300; i++ {
		d.Procesar(entrada(fmt.Sprintf("8.8.%d.%d", i/250, i%250), "/api/checkout", 500,
			pico.Add(time.Duration(i)*10*time.Second)))
	}
	hs := d.Hallazgos()
	var picos []anomalias.Hallazgo
	for _, h := range hs {
		if h.Tipo == "pico_5xx" {
			picos = append(picos, h)
		}
	}
	if len(picos) != 1 {
		t.Fatalf("esperaba 1 pico, hubo %d: %+v", len(picos), picos)
	}
	if picos[0].Endpoint != "/api/checkout" || picos[0].Cantidad < 300 {
		t.Errorf("pico raro: %+v", picos[0])
	}
}

// TestContraGroundTruth: el detector tiene que encontrar las 3 anomalías que el
// generador inyectó (recall 3/3) sin ahogarse en falsos positivos.
func TestContraGroundTruth(t *testing.T) {
	var buf bytes.Buffer
	ver, err := loggen.Generar(&buf, loggen.Opciones{Lineas: 300_000, Semilla: 4})
	if err != nil {
		t.Fatal(err)
	}

	d := anomalias.NuevoDetector(anomalias.ConfigPorDefecto())
	sc := bufio.NewScanner(&buf)
	for sc.Scan() {
		e, err := parse.ParsearManual(sc.Text())
		if err != nil {
			t.Fatal(err)
		}
		d.Procesar(e)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	hs := d.Hallazgos()

	for _, a := range ver.Anomalias {
		encontrada := false
		for _, h := range hs {
			if coincide(h, a.Tipo, a.IP, a.Endpoint, a.Desde, a.Hasta) {
				encontrada = true
				break
			}
		}
		if !encontrada {
			t.Errorf("anomalía inyectada NO detectada: %s %s%s [%s]",
				a.Tipo, a.IP, a.Endpoint, a.Detalle)
		}
	}

	// tolerancia de falsos positivos: ninguno además de los 3 inyectados
	if len(hs) > len(ver.Anomalias) {
		t.Errorf("hallazgos de más (%d vs %d inyectadas): %+v", len(hs), len(ver.Anomalias), hs)
	}
}

func coincide(h anomalias.Hallazgo, tipo, ip, endpoint string, desde, hasta time.Time) bool {
	if h.Tipo != tipo {
		return false
	}
	if ip != "" && h.IP != ip {
		return false
	}
	if endpoint != "" && h.Endpoint != endpoint {
		return false
	}
	return h.Desde.Before(hasta) && desde.Before(h.Hasta)
}

// TestPicoNocturnoQueCruzaLaHora es la regresión del caso que encontró el log
// de 20 M líneas: un pico de 5xx de 03:30 a 04:30 queda partido en dos franjas
// de ~300, cada una por debajo del umbral global (que dominan las horas pico
// del día) aunque sean 10× lo típico de la madrugada. Tiene que salir como un
// único episodio.
func TestPicoNocturnoQueCruzaLaHora(t *testing.T) {
	d := anomalias.NuevoDetector(anomalias.ConfigPorDefecto())
	inicio := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	for dia := range 10 {
		for h := range 24 {
			errores := 15 // madrugada
			if h >= 9 && h <= 18 {
				errores = 250 // horario laboral
			}
			for i := range errores {
				t := inicio.Add(time.Duration(dia*24+h)*time.Hour + time.Duration(i)*time.Hour/time.Duration(errores))
				d.Procesar(entrada(fmt.Sprintf("10.%d.%d.%d", dia, h, i%250), "/api/x", 500, t))
			}
		}
	}
	pico := inicio.Add(5*24*time.Hour + 3*time.Hour + 30*time.Minute) // día 5, 03:30
	for i := range 600 {
		d.Procesar(entrada(fmt.Sprintf("8.8.%d.%d", i/250, i%250), "/api/checkout", 503,
			pico.Add(time.Duration(i)*6*time.Second)))
	}

	var picos []anomalias.Hallazgo
	for _, h := range d.Hallazgos() {
		if h.Tipo == "pico_5xx" {
			picos = append(picos, h)
		}
	}
	if len(picos) != 1 {
		t.Fatalf("esperaba 1 episodio, hubo %d: %+v", len(picos), picos)
	}
	p := picos[0]
	if p.Endpoint != "/api/checkout" || !p.Desde.Equal(pico.Truncate(time.Hour)) ||
		!p.Hasta.Equal(pico.Truncate(time.Hour).Add(2*time.Hour)) || p.Cantidad < 600 {
		t.Errorf("episodio inesperado: %+v", p)
	}
}
