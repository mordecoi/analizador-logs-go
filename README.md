# Analizador de grandes volúmenes de logs

[![CI](https://github.com/mordecoi/analizador-logs-go/actions/workflows/ci.yml/badge.svg)](https://github.com/mordecoi/analizador-logs-go/actions/workflows/ci.yml)

Parciales de **Programación Eficiente**: Opción 3, implementado en **Go**.

📖 [**Documentación del proyecto**](https://mordecoi.github.io/analizador-logs-go/): funcionamiento, arquitectura, pruebas, rendimiento, seguridad y mantenimiento.

Procesa logs de servidor en formato **Apache Combined** y extrae errores más frecuentes, endpoints
más solicitados, usuarios e IPs más activos, horas pico y patrones anómalos. El foco del trabajo no
es que funcione, sino **medir y justificar** cada decisión de diseño: complejidad, estructuras de
datos, memoización, caching, concurrencia y perfilado (primer parcial); compilador y PGO,
monitoreo con OpenTelemetry, archivos grandes, calidad, testing y un framework (segundo parcial).

**Checa, Sol · Gómez Geneiro, Facundo · Nicolais, Manu**

📊 [**Presentación del primer parcial**](https://claude.ai/code/artifact/91cbc372-020b-4393-9ba2-a520d148f35d) · también en [`presentacion.html`](presentacion.html) (se abre en el navegador; imprimir = handout)

---

## Resultado

### Segundo parcial

Versión final del primer parcial contra la del segundo: misma máquina, mismo día, corridas
intercaladas, mediana. Detalle en [`SEGUNDO-PARCIAL.md`](SEGUNDO-PARCIAL.md).

| Líneas | Primer parcial | Segundo parcial | Speedup | Memoria asignada | Con memoización |
|---|---:|---:|---:|---:|---:|
| 1 M (184 MB) | 358 ms | **127 ms** | 2,8× | 267 → 76 MB | 65 ms |
| 5 M (919 MB) | 2 620 ms | **814 ms** | 3,2× | 1 429 → 451 MB | 413 ms |
| 20 M (3,6 GB) | 13 375 ms | **3 986 ms** | 3,4× | 5 667 → 1 770 MB | 1 905 ms |

| Calidad | Primer parcial | Segundo parcial |
|---|---:|---:|
| `golangci-lint` (21 linters) | 42 issues | **0** |
| Código inalcanzable (`deadcode`) | 15 funciones | **1** (justificada) |
| Cobertura de tests | 51,0 % | **92,6 %** |
| Anomalías a 20 M líneas | recall 2/3 | **3/3**, precisión 100 % |

De dónde salió: la fusión del map-reduce pasó a ser paralela (lo mostró una traza de Jaeger:
era el 70 % del tiempo), la lectura pasó a ser por lotes con un buffer reutilizado (de 7,0 a
2,0 M de asignaciones en 5 M líneas) y el compilador optimiza con un perfil (PGO, −7,5 %). Las herramientas
nuevas encontraron 8 bugs, entre ellos un desborde de enteros (fuzzing) y un comportamiento
de los `map` de Go que corrompía claves.

### Primer parcial

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
| [`SEGUNDO-PARCIAL.md`](SEGUNDO-PARCIAL.md) | **Segundo parcial:** los 10 temas, con herramienta, evidencia y mediciones |
| [`REQUERIMIENTOS.md`](REQUERIMIENTOS.md) | Requerimientos del primer y del segundo parcial, unificados |
| [`docs/api/`](docs/api/) | Documentación de la API, generada con gomarkdoc desde el código |
| [`PLAN.md`](PLAN.md) | Primer parcial: plan de trabajo de 12 fases con entregable, criterio de "hecho" y resultados |
| [`COMPLEJIDAD.md`](COMPLEJIDAD.md) | Análisis de complejidad con derivación de cada cota (no solo la notación) |
| [`PERFILADO.md`](PERFILADO.md) | Perfilado consolidado: las 5 preguntas del enunciado respondidas con evidencia de `pprof` |
| [`TEORIA.md`](TEORIA.md) | Repaso de la teoría de la materia, traducida de los ejemplos en Python a Go |
| [`resultados/`](resultados/) | Mediciones (CSV, benchstat), gráficos, reportes de calidad y del compilador, perfiles `.pprof` |

## Estructura

```
parcial1/
├── cmd/analizador/     main (sin lógica) + default.pgo, el perfil para PGO
├── internal/
│   ├── cli/            CLI con Cobra: generar, analizar, grafico
│   ├── loggen/         generador: Poisson no homogéneo, Zipf, anomalías inyectadas
│   ├── parse/          parseo de líneas: regex (baseline) y manual; fuzzing diferencial
│   ├── pipeline/       lectura por lotes; secuencial, map-reduce, mutex y productor-consumidor
│   ├── agregado/       Contador, fusión paralela particionada, Top-N (sort vs min-heap)
│   ├── anomalias/      ventanas deslizantes, picos de 5xx con estacionalidad, escaneos
│   ├── cache/          memoización de agregados y cache de consultas
│   ├── reporte/        presentación, validación contra el ground truth, CSV
│   ├── medicion/       tiempo y memoria de una operación
│   ├── grafico/        curva de escalabilidad en SVG
│   └── telemetria/     trazas OpenTelemetry por OTLP
├── docs/api/           documentación generada
├── docker-compose.yml  Jaeger, para ver las trazas
└── .golangci.yml       configuración del análisis de calidad
```

## Uso

```bash
go build -o analizador ./cmd/analizador      # usa default.pgo automáticamente
./analizador --help

# 1. Generar datos de prueba (reproducible: semilla fija)
./analizador generar -n 1000000
#    → logfiles.log (~184 MB) + ground-truth.json

# 2. Analizar (por defecto: parser manual, Top-N con heap, concurrente)
./analizador analizar -a logfiles.log

# 3. Validar contra el ground truth y detectar anomalías
./analizador analizar -a logfiles.log --verificacion ground-truth.json --anomalias

# Entradas que no se pueden partir por offset: productor-consumidor
./analizador analizar -a logs.gz
zcat logs.gz | ./analizador analizar -a -

# Ver la traza de una corrida en Jaeger (http://localhost:16686)
docker compose up -d
./analizador --otlp localhost:4318 analizar -a logfiles.log
```

Flags de `analizar` para comparar versiones:

| Flag | Valores | Qué cambia |
|---|---|---|
| `--parser` | `manual` \| `regex` | implementación del parseo de líneas |
| `--modo` | `conc` \| `seq` \| `conc-mutex` | map-reduce, secuencial, o la variante con contención |
| `-w`, `--workers` | entero (`0` = GOMAXPROCS) | workers del modo concurrente |
| `--topn-metodo` | `heap` \| `sort` | estrategia del Top-N |
| `--cache` | — | memoiza los agregados a disco y los reusa si el log no cambió |
| `--anomalias` | — | pasada extra de detección de patrones anómalos |
| `--cpuprofile` `--memprofile` `--trace` | archivo | perfilado |
| `--csv` `--version-etiqueta` | archivo, etiqueta | agrega una fila de medición al CSV |
| `--otlp` (global) | `host:puerto` | exporta trazas OpenTelemetry |

Un valor inválido (`--modo paralelo`) es un error. Si la validación contra el ground truth
falla, el programa sale con código 2, para distinguirlo de un error de ejecución (código 1).

```bash
# Reproducir la comparación baseline vs final
./analizador analizar -a logfiles.log --parser regex --modo seq --topn-metodo sort
./analizador analizar -a logfiles.log
```

## Desarrollo

```bash
scripts/controles.sh todo                               # los mismos controles que el CI
go test ./... -cover                                    # tests de los 11 paquetes
go test ./internal/parse -run '^$' -fuzz Fuzz -fuzztime 60s
go test ./internal/pipeline -run '^$' -bench . -benchmem  # lectura: ReadString vs lotes
golangci-lint run ./...                                 # análisis de calidad
deadcode ./...                                          # código inalcanzable
gomarkdoc --output 'docs/api/{{.Dir}}.md' ./internal/...  # documentación
go build -gcflags='-m' ./internal/parse                 # decisiones de inlining y escape
go tool pprof -http=: resultados/perfiles/cpu-v2-manual.pprof
```

## Notas de implementación

Tres hallazgos del primer parcial que salieron de medir:

- **El regex era el 70 % del CPU.** Reemplazarlo por un parseo manual con `strings.IndexByte` y
  sub-slices (cero asignaciones) dio 16× en aislado y 4,9× en el pipeline, sin cambiar la clase de
  complejidad: la ganancia es de factor constante y de presión de GC.
- **Las claves de los `map` retenían las líneas enteras.** Al ser sub-slices del string de entrada,
  cada IP única mantenía viva su línea completa. Clonar la clave en la inserción bajó el heap
  retenido de 98,6 a 19,9 MB por millón de líneas. (En el segundo parcial se vio que esto
  funcionaba a medias: Go reescribe la clave en cada asignación; ver `SEGUNDO-PARCIAL.md`.)
- **La concurrencia paga por independencia, no por usar goroutines.** La variante con un contador
  compartido bajo mutex (`--modo conc-mutex`) usa 8 núcleos y es *más lenta* que la versión
  secuencial. El map-reduce con contadores por worker da 2,9×.

Y tres del segundo:

- **La parte secuencial termina mandando** (Amdahl). Con el *map* en paralelo, la fusión de
  los parciales en un solo hilo pasó a ser el 70 % del tiempo. Se vio en la traza de Jaeger,
  no en `pprof`, que suma el CPU de todos los hilos.
- **Un test que compara dos caminos vale más que muchos tests de casos.** El fuzzing
  diferencial (regex vs manual) y la equivalencia secuencial/concurrente encontraron los bugs
  más sutiles: un desborde de enteros y claves de `map` corruptas.
- **Una optimización puede dejar de convenir.** Cuando el análisis se aceleró 3,4×, cargar el
  cache, que era secuencial, pasó a tardar más que recalcular todo. Hubo que paralelizar
  también la carga.

[Enlace roto](docs/no-existe.md)
