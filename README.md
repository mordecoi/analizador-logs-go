# Analizador de grandes volúmenes de logs

Primer Parcial de **Programación Eficiente** — Opción 3, implementado en **Go** (solo biblioteca estándar).

Procesa logs de servidor en formato **Apache Combined** y extrae errores más frecuentes, endpoints
más solicitados, usuarios e IPs más activos, horas pico y patrones anómalos. El foco del trabajo no
es que funcione, sino **medir y justificar** cada decisión de diseño: complejidad, estructuras de
datos, memoización, caching, concurrencia y perfilado.

**Checa, Sol · Gómez Geneiro, Facundo · Nicolais, Manu**

📊 [**Presentación**](https://claude.ai/code/artifact/91cbc372-020b-4393-9ba2-a520d148f35d) · también en [`presentacion.html`](presentacion.html) (se abre en el navegador; imprimir = handout)

---

## Resultado

Sobre **5 M de líneas** (919 MB), binario compilado, tanda única de medición:

| Versión | Tiempo | Memoria asignada | Ciclos de GC | |
|---|---:|---:|---:|---|
| v0 — baseline: regex + sort + secuencial | 33,6 s | 3 036 MB | 121 | línea base |
| v1 — Top-N con min-heap | 35,2 s | 3 036 MB | 121 | 14× en aislado, invisible en el total |
| v2 — parseo manual sin regex | 6,81 s | 1 201 MB | 49 | **4,9×** |
| v3 — map-reduce concurrente (8 workers) | **2,47 s** | 1 421 MB | 31 | **13,6×** |
| v4 — memoización de agregados (2ª corrida) | **0,76 s** | 213 MB | 6 | **44×** |

Escalabilidad de la versión final ([gráfico log-log](resultados/escalabilidad.svg) — pendiente ≈ 1, o sea O(n)):

| Líneas | Baseline | Final | Speedup | Heap retenido |
|---|---:|---:|---:|---:|
| 1 M (184 MB) | 7,3 s | **0,33 s** | 22,2× | 20 MB |
| 5 M (919 MB) | 33,6 s | **2,47 s** | 13,6× | 137 MB |
| 20 M (3,59 GB) | 145,7 s | **10,9 s** | 13,4× | 549 MB |

Cada corrida se **valida automáticamente** contra el ground truth del generador: total de líneas,
hora pico y top-10 de endpoints, estados e IPs, idénticos clave por clave. Detección de anomalías:
**recall 3/3, precisión 100 %**.

## Documentación

| Documento | Contenido |
|---|---|
| [`REQUERIMIENTOS.md`](REQUERIMIENTOS.md) | Requerimientos del primer y del segundo parcial, unificados |
| [`PLAN.md`](PLAN.md) | Plan de trabajo completo: 12 fases con entregable, criterio de "hecho" y resultados de cada una |
| [`COMPLEJIDAD.md`](COMPLEJIDAD.md) | Análisis de complejidad con derivación de cada cota (no solo la notación) |
| [`PERFILADO.md`](PERFILADO.md) | Perfilado consolidado: las 5 preguntas del enunciado respondidas con evidencia de `pprof` |
| [`TEORIA.md`](TEORIA.md) | Repaso de la teoría de la materia, traducida de los ejemplos en Python a Go |
| [`resultados/`](resultados/) | CSV de mediciones, gráfico de escalabilidad, salidas por fase y perfiles `.pprof` |

## Estructura

```
parcial1/
├── cmd/
│   ├── generar/      genera el log sintético + ground-truth.json
│   ├── analizar/     el analizador (todas las versiones, por flags)
│   └── grafico/      dibuja la curva de escalabilidad en SVG
└── internal/
    ├── loggen/       generador: Poisson no homogéneo, Zipf, anomalías inyectadas
    ├── parse/        parseo de líneas: versión regex (baseline) y manual
    ├── agregado/     contadores y Top-N (ordenamiento completo vs min-heap)
    ├── pipeline/     recorrido secuencial, map-reduce y la variante con mutex
    ├── anomalias/    ventanas deslizantes, pico de 5xx, detección de escaneo
    ├── cache/        memoización de agregados y cache de consultas
    └── bench/        utilidades de medición
```

## Uso

```bash
# 1. Generar datos de prueba (reproducible: semilla fija)
go run ./cmd/generar -lineas 1000000
#    → logfiles.log (~184 MB) + ground-truth.json

# 2. Analizar (por defecto: parser manual, Top-N con heap, concurrente)
go run ./cmd/analizar -archivo logfiles.log

# 3. Validar contra el ground truth y detectar anomalías
go run ./cmd/analizar -archivo logfiles.log -verificacion ground-truth.json -anomalias
```

Flags del analizador para comparar versiones:

| Flag | Valores | Qué cambia |
|---|---|---|
| `-parser` | `regex` \| `manual` | implementación del parseo de líneas |
| `-modo` | `seq` \| `conc` \| `conc-mutex` | secuencial, map-reduce, o la variante con contención |
| `-workers` | entero (`0` = GOMAXPROCS) | workers del modo concurrente |
| `-topn-metodo` | `sort` \| `heap` | estrategia del Top-N |
| `-cache` | — | memoiza los agregados a disco y los reusa si el log no cambió |
| `-anomalias` | — | pasada extra de detección de patrones anómalos |
| `-cpuprofile` `-memprofile` `-trace` | archivo | perfilado |
| `-csv` `-version` | archivo, etiqueta | agrega una fila de medición al CSV |

```bash
# Reproducir la comparación baseline vs final
go run ./cmd/analizar -archivo logfiles.log -parser regex  -modo seq -topn-metodo sort
go run ./cmd/analizar -archivo logfiles.log -parser manual -modo conc -workers 8
```

## Desarrollo

```bash
go test ./...                                  # tests de los 6 paquetes
go test ./internal/parse/ -bench . -benchmem   # benchmarks (regex vs manual)
go vet ./... && gofmt -l .                     # chequeo estático y formato
go tool pprof -http=: resultados/perfiles/cpu-v2-manual.pprof
go tool trace resultados/perfiles/trace-conc8.out
```

## Notas de implementación

Tres hallazgos que salieron de medir y que están documentados en detalle:

- **El regex era el 70 % del CPU.** Reemplazarlo por un parseo manual con `strings.IndexByte` y
  sub-slices (cero asignaciones) dio 16× en aislado y 4,9× en el pipeline — sin cambiar la clase de
  complejidad: la ganancia es factor constante y presión de GC.
- **Las claves de los `map` retenían las líneas enteras.** Al ser sub-slices del string de entrada,
  cada IP única mantenía viva su línea completa. Clonar la clave solo en la inserción bajó el heap
  retenido de 98,6 a 19,9 MB por millón de líneas, con costo de tiempo nulo.
- **La concurrencia paga por independencia, no por usar goroutines.** La variante con un contador
  compartido bajo mutex (`-modo conc-mutex`) usa 8 núcleos y es *más lenta* que la versión
  secuencial. El map-reduce con contadores por worker da 2,9×.

Los requerimientos de ambos parciales están en [`REQUERIMIENTOS.md`](REQUERIMIENTOS.md).
