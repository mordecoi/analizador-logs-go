package telemetria

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestSinEndpointNoHaceNada(t *testing.T) {
	apagar, err := Iniciar(context.Background(), "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := apagar(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestExportaSpansPorOTLP levanta un receptor OTLP/HTTP falso y verifica que
// los spans creados con la API de OpenTelemetry le lleguen al apagar.
func TestExportaSpansPorOTLP(t *testing.T) {
	var recibidos atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/traces" {
			recibidos.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	defer otel.SetTracerProvider(noop.NewTracerProvider()) // no contaminar otros tests

	ctx := context.Background()
	apagar, err := Iniciar(ctx, strings.TrimPrefix(srv.URL, "http://"), "test")
	if err != nil {
		t.Fatal(err)
	}
	_, span := otel.Tracer("test").Start(ctx, "operacion")
	span.End()
	if err := apagar(ctx); err != nil { // Shutdown vacía el lote pendiente
		t.Fatal(err)
	}
	if recibidos.Load() == 0 {
		t.Fatal("el receptor OTLP no recibió ningún lote de spans")
	}
}
