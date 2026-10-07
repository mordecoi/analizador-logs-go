// Package telemetria conecta el analizador con un backend de APM mediante
// OpenTelemetry.
//
// El código instrumentado (pipeline, cli) solo depende de la API de
// OpenTelemetry: crea spans con otel.Tracer y no sabe adónde van. Este paquete
// decide el destino: si se pasa un endpoint, instala un TracerProvider que
// exporta por OTLP/HTTP; si no, queda el provider no-op por defecto y los spans
// no cuestan prácticamente nada.
//
// OTLP es el protocolo estándar que aceptan Jaeger, Grafana Tempo, New Relic,
// Elastic APM y Datadog, entre otros: cambiar de herramienta es cambiar el
// endpoint, no el código. Para la demo local se usa Jaeger (docker-compose.yml).
package telemetria

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// NombreServicio identifica al analizador en la interfaz del APM.
const NombreServicio = "analizador-logs"

// Apagar vacía los spans pendientes y cierra el exportador.
type Apagar func(context.Context) error

// Iniciar configura la exportación de trazas a endpoint ("localhost:4318").
// Con endpoint vacío no hace nada y devuelve un Apagar que no hace nada.
func Iniciar(ctx context.Context, endpoint, version string) (Apagar, error) {
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}
	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(), // colector local, sin TLS
	)
	if err != nil {
		return nil, fmt.Errorf("telemetría: %w", err)
	}
	res := resource.NewWithAttributes(semconv.SchemaURL,
		semconv.ServiceName(NombreServicio),
		semconv.ServiceVersion(version),
	)
	tp := sdktrace.NewTracerProvider(
		// por lotes: los spans se envían en segundo plano, fuera del camino crítico
		sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(time.Second)),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}
