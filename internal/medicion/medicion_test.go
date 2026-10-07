package medicion

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestBytes(t *testing.T) {
	casos := []struct {
		b        int64
		esperado string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{5 << 20, "5.0 MB"},
		{3 << 30, "3.00 GB"},
	}
	for _, c := range casos {
		if got := Bytes(c.b); got != c.esperado {
			t.Errorf("Bytes(%d) = %q, esperaba %q", c.b, got, c.esperado)
		}
	}
}

var retenido []byte

func TestMedir(t *testing.T) {
	m, err := Medir(func() error {
		retenido = make([]byte, 8<<20) // queda vivo: tiene que verse en HeapFinal
		time.Sleep(5 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.Transcurrido < 5*time.Millisecond {
		t.Errorf("Transcurrido %s, esperaba ≥ 5ms", m.Transcurrido)
	}
	if m.TotalAsign < 8<<20 || m.HeapFinal < 8<<20 {
		t.Errorf("TotalAsign %d, HeapFinal %d: esperaba ≥ 8 MiB", m.TotalAsign, m.HeapFinal)
	}

	errEsperado := errors.New("falló")
	if _, err := Medir(func() error { return errEsperado }); !errors.Is(err, errEsperado) {
		t.Errorf("el error de fn no se propagó: %v", err)
	}
}

func ExampleBytes() {
	fmt.Println(Bytes(int64(919 << 20)))
	fmt.Println(Bytes(uint64(3_590_000_000)))
	// Output:
	// 919.0 MB
	// 3.34 GB
}
