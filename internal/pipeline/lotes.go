package pipeline

import (
	"bytes"
	"errors"
	"io"
	"unsafe"

	"parcial1/internal/parse"
)

// TamLotePorDefecto es el tamaño del buffer de lectura de cada worker. Con
// líneas de ~180 B, un lote de 1 MiB trae ~5800 líneas por llamada a Read; por
// encima de eso el tiempo ya no baja (ver BenchmarkTamLote).
const TamLotePorDefecto = 1 << 20

// leerLote completa buf leyendo de r, a partir de los n bytes que ya tiene al
// comienzo (la línea incompleta que sobró del lote anterior), y lo corta en el
// último '\n'.
//
// Devuelve el lote de líneas completas y la cola incompleta, ambos sub-slices
// del buffer devuelto, que es buf salvo que una sola línea no entrara: entonces
// se duplica el tamaño. Al llegar al EOF la cola deja de esperar su '\n' y se
// entrega dentro del lote (última línea sin salto final), junto con io.EOF.
func leerLote(r io.Reader, buf []byte, n int) (lote, cola, nuevoBuf []byte, err error) {
	for {
		m, err := io.ReadFull(r, buf[n:])
		n += m
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return buf[:n], nil, buf, io.EOF
		}
		if err != nil {
			return nil, nil, buf, err
		}
		if i := bytes.LastIndexByte(buf[:n], '\n'); i >= 0 {
			return buf[:i+1], buf[i+1 : n], buf, nil
		}
		mas := make([]byte, 2*len(buf))
		copy(mas, buf[:n])
		buf = mas
	}
}

// lectorLotes entrega un io.Reader como una secuencia de lotes de líneas
// completas reutilizando siempre el mismo buffer. Como bufio.Scanner.Bytes, el
// lote devuelto por siguiente solo es válido hasta la próxima llamada.
type lectorLotes struct {
	r    io.Reader
	buf  []byte
	cola []byte // línea incompleta al final del lote anterior (sub-slice de buf)
	fin  bool
}

func nuevoLectorLotes(r io.Reader, tamLote int) *lectorLotes {
	return &lectorLotes{r: r, buf: make([]byte, tamLote)}
}

// siguiente devuelve el próximo lote, o io.EOF cuando ya no quedan.
func (l *lectorLotes) siguiente() ([]byte, error) {
	if l.fin {
		return nil, io.EOF
	}
	n := copy(l.buf, l.cola) // mueve la cola al comienzo: pisa el lote anterior
	lote, cola, buf, err := leerLote(l.r, l.buf, n)
	l.buf, l.cola = buf, cola
	if errors.Is(err, io.EOF) {
		l.fin = true
		return lote, nil
	}
	return lote, err
}

// vista convierte bytes a string sin copiarlos. El string comparte memoria con
// el buffer del lote, que se reutiliza: es válido solo mientras el lote lo sea.
// Por eso el contrato de Parser y de los callbacks: quien retiene un campo de
// una Entrada más allá de la llamada debe clonarlo (strings.Clone).
//
// Auditado (gosec G103): el único consumidor que retiene strings más allá del
// callback (anomalias.Detector) los clona; agregado.Contador clona al
// insertar. TestAnomaliasSobreLotes verifica el contrato.
func vista(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b)) //nolint:gosec // G103 auditado, ver arriba
}

// procesarLote parsea las líneas de un lote cuyo primer byte cae antes de
// hasta, medido en offsets del archivo (offset es el del primer byte del lote).
// Devuelve las líneas procesadas, las no reconocidas y si ya se pasó de hasta.
func procesarLote(lote []byte, offset, hasta int64, parsear Parser, agregar func(parse.Entrada)) (lineas, errores int, terminado bool) {
	for i := 0; i < len(lote); {
		if offset+int64(i) >= hasta {
			return lineas, errores, true
		}
		fin := bytes.IndexByte(lote[i:], '\n')
		var linea []byte
		if fin < 0 {
			linea, i = lote[i:], len(lote)
		} else {
			linea, i = lote[i:i+fin], i+fin+1
		}
		linea = bytes.TrimRight(linea, "\r")
		lineas++
		if e, err := parsear(vista(linea)); err != nil {
			errores++
		} else {
			agregar(e)
		}
	}
	return lineas, errores, false
}
