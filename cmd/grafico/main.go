// Comando grafico: lee el CSV de mediciones (version,archivo,lineas,ms,...) y
// dibuja la curva de escalabilidad tiempo-vs-líneas por versión en un SVG.
// Ejes log-log: n va de 10⁶ a 2·10⁷ y el tiempo de ~10² a ~10⁶ ms, así que en
// escala lineal las curvas chicas serían invisibles; en log-log, además, una
// complejidad O(n) se ve como recta de pendiente 1.
//
//	go run ./cmd/grafico -csv resultados/escalabilidad.csv -salida resultados/escalabilidad.svg
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
)

type punto struct {
	n  float64 // líneas
	ms float64
}

var colores = []string{"#c0392b", "#d68910", "#2471a3", "#1e8449", "#7d3c98", "#5d6d7e"}

func main() {
	rutaCSV := flag.String("csv", "resultados/escalabilidad.csv", "CSV de mediciones")
	salida := flag.String("salida", "resultados/escalabilidad.svg", "SVG de salida")
	titulo := flag.String("titulo", "Escalabilidad: tiempo vs líneas (log-log)", "título del gráfico")
	flag.Parse()

	series, orden, err := leer(*rutaCSV)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*salida, []byte(dibujar(series, orden, *titulo)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("gráfico escrito en %s (%d series)\n", *salida, len(orden))
}

// leer agrupa las filas del CSV por versión, ordenadas por n.
func leer(ruta string) (map[string][]punto, []string, error) {
	f, err := os.Open(ruta)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	filas, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(filas) < 2 {
		return nil, nil, fmt.Errorf("%s: sin datos", ruta)
	}

	series := make(map[string][]punto)
	var orden []string // versiones en orden de aparición
	for _, fila := range filas[1:] {
		if len(fila) < 4 {
			continue
		}
		n, err1 := strconv.ParseFloat(fila[2], 64)
		ms, err2 := strconv.ParseFloat(fila[3], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		v := fila[0]
		if _, visto := series[v]; !visto {
			orden = append(orden, v)
		}
		series[v] = append(series[v], punto{n, ms})
	}
	for v := range series {
		slices.SortFunc(series[v], func(a, b punto) int {
			switch {
			case a.n < b.n:
				return -1
			case a.n > b.n:
				return 1
			default:
				return 0
			}
		})
	}
	return series, orden, nil
}

func dibujar(series map[string][]punto, orden []string, titulo string) string {
	const ancho, alto = 860.0, 520.0
	const mIzq, mDer, mArr, mAba = 80.0, 190.0, 50.0, 60.0

	// rango de los ejes en log10, redondeado a décadas
	minX, maxX := math.Inf(1), math.Inf(-1)
	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, pts := range series {
		for _, p := range pts {
			minX, maxX = math.Min(minX, p.n), math.Max(maxX, p.n)
			minY, maxY = math.Min(minY, p.ms), math.Max(maxY, p.ms)
		}
	}
	lx0, lx1 := math.Floor(math.Log10(minX)), math.Ceil(math.Log10(maxX))
	ly0, ly1 := math.Floor(math.Log10(minY)), math.Ceil(math.Log10(maxY))

	x := func(n float64) float64 {
		return mIzq + (math.Log10(n)-lx0)/(lx1-lx0)*(ancho-mIzq-mDer)
	}
	y := func(ms float64) float64 {
		return alto - mAba - (math.Log10(ms)-ly0)/(ly1-ly0)*(alto-mArr-mAba)
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" font-family="system-ui,sans-serif">`+"\n", ancho, alto)
	fmt.Fprintf(&b, `<rect width="%.0f" height="%.0f" fill="white"/>`+"\n", ancho, alto)
	fmt.Fprintf(&b, `<text x="%.0f" y="28" font-size="17" font-weight="bold">%s</text>`+"\n", mIzq, titulo)

	// grilla y rótulos por década
	for d := lx0; d <= lx1; d++ {
		px := x(math.Pow(10, d))
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#ddd"/>`+"\n", px, mArr, px, alto-mAba)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-size="12" text-anchor="middle">10^%.0f</text>`+"\n", px, alto-mAba+18, d)
	}
	for d := ly0; d <= ly1; d++ {
		py := y(math.Pow(10, d))
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#ddd"/>`+"\n", mIzq, py, ancho-mDer, py)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-size="12" text-anchor="end">%s</text>`+"\n", mIzq-6, py+4, etiquetaMs(math.Pow(10, d)))
	}
	fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-size="13" text-anchor="middle">líneas del log</text>`+"\n", (mIzq+ancho-mDer)/2, alto-16)

	// series
	for i, v := range orden {
		pts := series[v]
		color := colores[i%len(colores)]
		var camino []string
		for _, p := range pts {
			camino = append(camino, fmt.Sprintf("%.1f,%.1f", x(p.n), y(p.ms)))
		}
		fmt.Fprintf(&b, `<polyline points="%s" fill="none" stroke="%s" stroke-width="2.5"/>`+"\n", strings.Join(camino, " "), color)
		for _, p := range pts {
			fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="4" fill="%s"/>`+"\n", x(p.n), y(p.ms), color)
			fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-size="11" text-anchor="middle" fill="%s">%s</text>`+"\n",
				x(p.n), y(p.ms)-9, color, etiquetaMs(p.ms))
		}
		// leyenda
		ly := mArr + 8 + float64(i)*22
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="2.5"/>`+"\n", ancho-mDer+14, ly, ancho-mDer+42, ly, color)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-size="12">%s</text>`+"\n", ancho-mDer+48, ly+4, v)
	}

	b.WriteString("</svg>\n")
	return b.String()
}

func etiquetaMs(ms float64) string {
	switch {
	case ms >= 60_000:
		return fmt.Sprintf("%.1f min", ms/60_000)
	case ms >= 1000:
		return fmt.Sprintf("%.1f s", ms/1000)
	default:
		return fmt.Sprintf("%.0f ms", ms)
	}
}
