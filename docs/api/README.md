# Documentación de la API

Generada con [gomarkdoc](https://github.com/princjef/gomarkdoc) a partir de los comentarios
de documentación del código. Los ejemplos (`Example…`) son tests: `go test` verifica su salida.

```bash
gomarkdoc --output 'docs/api/{{.Dir}}.md' ./internal/...
```

También se puede navegar como en pkg.go.dev con `go run golang.org/x/pkgsite/cmd/pkgsite@latest`
(→ http://localhost:8080/parcial1) o consultar desde la terminal con `go doc`.

| Paquete | Responsabilidad |
|---|---|
| [`cli`](internal/cli.md) | Interfaz de línea de comandos (Cobra): `generar`, `analizar`, `grafico` |
| [`loggen`](internal/loggen.md) | Generador de logs sintéticos con anomalías inyectadas y ground truth |
| [`parse`](internal/parse.md) | Parseo de líneas Apache Combined: regex (baseline) y manual |
| [`pipeline`](internal/pipeline.md) | Lectura por lotes; recorridos secuencial, map-reduce y productor-consumidor |
| [`agregado`](internal/agregado.md) | `Contador`, fusión paralela particionada y Top-N (sort vs heap) |
| [`anomalias`](internal/anomalias.md) | Ventanas deslizantes, picos de 5xx con estacionalidad, escaneos |
| [`cache`](internal/cache.md) | Memoización de agregados en disco y cache de consultas Top-N |
| [`reporte`](internal/reporte.md) | Presentación de resultados, validación y CSV de mediciones |
| [`medicion`](internal/medicion.md) | Medición de tiempo y memoria, formato de tamaños |
| [`grafico`](internal/grafico.md) | Curva de escalabilidad en SVG log-log |
| [`telemetria`](internal/telemetria.md) | Exportación de trazas OpenTelemetry por OTLP |
