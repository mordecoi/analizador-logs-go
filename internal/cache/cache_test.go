package cache_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"parcial1/internal/agregado"
	"parcial1/internal/cache"
	"parcial1/internal/parse"
)

func entrada(ip, ruta string, estado, hora int) parse.Entrada {
	return parse.Entrada{
		IP:     ip,
		Ruta:   ruta,
		Estado: estado,
		Fecha:  time.Date(2024, 1, 1, hora, 0, 0, 0, time.UTC),
	}
}

func contadoresDePrueba() *agregado.Contadores {
	c := agregado.NuevoContadores()
	c.Agregar(entrada("1.1.1.1", "/a", 200, 9))
	c.Agregar(entrada("1.1.1.1", "/a", 200, 9))
	c.Agregar(entrada("2.2.2.2", "/b", 404, 14))
	return c
}

// logDePrueba crea un archivo que hace de log (el contenido no importa: la
// memoización solo mira tamaño y mtime).
func logDePrueba(t *testing.T) string {
	t.Helper()
	ruta := filepath.Join(t.TempDir(), "prueba.log")
	if err := os.WriteFile(ruta, []byte("contenido de prueba\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return ruta
}

func TestGuardarYCargar(t *testing.T) {
	ruta := logDePrueba(t)
	orig := contadoresDePrueba()

	if err := cache.Guardar(ruta, orig); err != nil {
		t.Fatal(err)
	}
	cargado, motivo := cache.Cargar(ruta)
	if cargado == nil {
		t.Fatalf("esperaba hit, miss por: %s", motivo)
	}
	// Igual y no reflect.DeepEqual: el orden interno de un Contador depende
	// del orden en que gob recorre el mapa al decodificar, que es aleatorio.
	if !orig.Igual(cargado) {
		t.Fatalf("agregados distintos:\n orig=%+v\n cargado=%+v", orig, cargado)
	}
}

func TestCargarSinCachePrevio(t *testing.T) {
	ruta := logDePrueba(t)
	if c, motivo := cache.Cargar(ruta); c != nil {
		t.Fatal("esperaba miss")
	} else if motivo != "sin cache previo" {
		t.Errorf("motivo: %q", motivo)
	}
}

func TestCargarInvalidaSiElLogCambia(t *testing.T) {
	ruta := logDePrueba(t)
	if err := cache.Guardar(ruta, contadoresDePrueba()); err != nil {
		t.Fatal(err)
	}

	// cambiar el mtime simula que el log fue modificado
	nuevo := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(ruta, nuevo, nuevo); err != nil {
		t.Fatal(err)
	}
	if c, motivo := cache.Cargar(ruta); c != nil {
		t.Fatal("esperaba miss: el log cambió")
	} else if motivo != "el log cambió desde que se calculó" {
		t.Errorf("motivo: %q", motivo)
	}

	// cambiar el tamaño también invalida
	if err := cache.Guardar(ruta, contadoresDePrueba()); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(ruta, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("línea nueva\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if c, _ := cache.Cargar(ruta); c != nil {
		t.Fatal("esperaba miss: el log creció")
	}
}

func TestConsultasHitsMissesEInvalidacion(t *testing.T) {
	q := cache.NuevasConsultas(contadoresDePrueba(), agregado.MetodoHeap)

	primera := q.Endpoints(2)
	if q.Misses != 1 || q.Hits != 0 {
		t.Fatalf("tras 1ª consulta: hits=%d misses=%d", q.Hits, q.Misses)
	}
	segunda := q.Endpoints(2)
	if q.Misses != 1 || q.Hits != 1 {
		t.Fatalf("tras 2ª consulta: hits=%d misses=%d", q.Hits, q.Misses)
	}
	if !reflect.DeepEqual(primera, segunda) {
		t.Fatal("el hit devolvió otra cosa")
	}
	// otro n = otra clave → miss
	q.Endpoints(1)
	if q.Misses != 2 {
		t.Fatalf("n distinto tendría que ser miss: misses=%d", q.Misses)
	}

	// entra un dato nuevo → lo cacheado queda obsoleto y se recalcula
	q.Agregar(entrada("3.3.3.3", "/c", 200, 9))
	q.Agregar(entrada("3.3.3.3", "/c", 200, 9))
	q.Agregar(entrada("3.3.3.3", "/c", 200, 9))
	tras := q.Endpoints(2)
	if q.Misses != 3 {
		t.Fatalf("tras invalidar tendría que recalcular: misses=%d", q.Misses)
	}
	if tras[0].Clave != "/c" || tras[0].Conteo != 3 {
		t.Fatalf("el recálculo no ve los datos nuevos: %v", tras)
	}
}
