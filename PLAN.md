# Plan de trabajo — Primer Parcial (Opción 3: Analizador de logs)

> **Nota (segundo parcial):** este documento es el registro del primer parcial y se conserva tal cual. Los programas `cmd/generar`, `cmd/analizar` y `cmd/grafico` se unificaron en un solo binario con Cobra: `go run ./cmd/analizador generar|analizar|grafico`, con los mismos flags en formato `--flag` (por ejemplo `-archivo x` pasa a ser `--archivo x` o `-a x`; la etiqueta del CSV, `-version`, pasó a `--version-etiqueta`). Ver [`SEGUNDO-PARCIAL.md`](SEGUNDO-PARCIAL.md).

> Materia: Programación Eficiente · Lenguaje: **Go** · Opción: **3 — Analizador de grandes volúmenes de logs**

---

## 1. Decisiones ya tomadas

| Tema | Decisión | Motivo |
|---|---|---|
| Dataset | Logs sintéticos en **formato Apache Combined** | Cada punto del enunciado mapea 1:1 con un campo (status → errores, endpoint → más usados, IP → usuarios activos, fecha → horas pico). |
| Generador | **Reescrito en Go**, no el de Kaggle | El de Python tiene 3 fallas fatales: timestamp idéntico en todas las líneas (`prop=10` fijo), distribuciones uniformes (Top-N sin señal), y sin anomalías para detectar. |
| Dependencias | **Solo stdlib** | Convención del proyecto. Para gráficos: exportar CSV y graficar aparte. |
| Baseline | Secuencial + `regexp` + sort completo + `map` | Es la "línea base" que exige el enunciado; todas las mejoras se miden contra esto. |
| Versión final | Parseo manual + min-heap + map-reduce concurrente + cache | |

### Formato de línea que vamos a generar y parsear

```
190.12.44.3 - - [15/Mar/2019:14:22:07 +0530] "GET /usr/login HTTP/1.0" 200 4981 "-" "Mozilla/5.0 ..." 342
└── IP ────┘ ^ ^ └──── fecha/hora ─────────┘  └ req ┘└ endpoint ┘        │   │    │    └─ user agent ─┘  └ resp.time (ms)
          rlog uid                                              status  bytes referrer
```

---

## 2. Arquitectura del proyecto

```
parcial1/
├── PLAN.md                     ← este documento
├── go.mod                      ← module parcial1  (go 1.26.x, igual que practico4)
├── internal/
│   ├── bench/                  ← copiado de practico4 (medición, NuevoRand, Formatear, ...)
│   ├── loggen/
│   │   └── loggen.go           ← generador sintético + ground-truth de anomalías
│   ├── parse/
│   │   └── parse.go            ← parseo de una línea → struct Entrada  (2 impls: regex y manual)
│   ├── agregado/
│   │   └── agregado.go         ← contadores, Top-N (sort vs heap), merge de parciales
│   ├── anomalias/
│   │   └── anomalias.go        ← ventana deslizante por IP, pico de 5xx, scanner de endpoints
│   └── cache/
│       └── cache.go            ← memoización de agregados en disco + cache de consultas
├── cmd/
│   ├── generar/main.go         ← CLI: -lineas -semilla -salida  → logfiles.log + ground-truth.json
│   └── analizar/main.go        ← CLI: -archivo -topn -topn-metodo -verificacion -csv (-workers -modo -cache en fases próximas)
│   (benchmarks co-locados con cada paquete: internal/agregado/bench_test.go, internal/parse/bench_test.go, ...)
└── resultados/
    ├── comparativa.csv         ← tabla obligatoria (versión, n, tiempo, memoria)
    ├── escalabilidad.csv       ← tiempo vs n por versión
    └── *.pprof                 ← perfiles guardados
```

`struct Entrada` (lo que devuelve el parser):

```go
type Entrada struct {
    IP        string
    Fecha     time.Time
    Metodo    string
    Endpoint  string
    Estado    int
    Bytes     int
    Referrer  string
    UA        string
    RespMs    int
}
```

---

## 3. Cobertura de los requisitos obligatorios

El enunciado exige mínimos concretos. Así los cubrimos (y sobra margen):

### Estructuras de datos (mínimo 2 — usamos 5)

| Estructura | Dónde | Por qué |
|---|---|---|
| `map[string]int` | contadores de status / endpoint / IP / hora | inserción y actualización O(1) amortizado; el conteo es el 80% del trabajo |
| `container/heap` (min-heap de tamaño k) | Top-N de cada métrica | mantener sólo los k mejores en O(u·log k) en vez de ordenar u claves |
| `map[string]struct{}` (set) | endpoints distintos tocados por cada IP | detección de scanner: pertenencia y cardinalidad O(1) |
| ring buffer / deque | ventana temporal de timestamps por IP | detección de ráfagas: alta y baja por un extremo O(1) (reusa la idea del Práctico 4 ej. 4) |
| slice `[]par{clave,conteo}` | volcado de un map para ordenar / reportar | acceso secuencial y `sort.Slice` |

### Análisis de complejidad (mínimo 2 operaciones)

1. **Parseo de línea**: regex NFA con backtracking sobre patrón con grupos → peor caso malo; manual con `IndexByte` → O(L) determinista, L = largo de línea. Total: O(n·L).
2. **Top-N**: sort completo del map volcado → O(u·log u), u = claves únicas. Min-heap de tamaño k → O(u·log k). Con u ≈ 10⁵ y k = 10, `log u ≈ 17` vs `log k ≈ 3`.
3. **Merge del map-reduce**: sumar C maps parciales → O(Σ|mapᵢ|) = O(u·C) en el peor caso.
4. Mencionar el anti-patrón O(n²): comparar cada línea contra todas (p. ej. "duplicados" por `slices.Contains`) y por qué NO se hace.

### Elección de algoritmo (mínimo 1 decisión justificada)

- **Principal**: reemplazo de `regexp` por escaneo manual por posiciones de delimitadores (` `, `[`, `]`, `"`). Justificación: el parseo domina el perfil de CPU en el baseline; la regex además genera basura (submatches) que dispara el GC. Medición antes/después con pprof.
- **Secundaria**: Top-N con heap en vez de ordenar todo.

### Memoización / caching (mínimo 1 — hacemos las dos cosas, para mostrar la diferencia conceptual)

| | Qué guarda | Clave | Cuándo se reusa | Invalidación | Mejora esperada |
|---|---|---|---|---|---|
| **Memoización** | los agregados completos (maps de conteo) serializados a disco | `hash(tamaño+mtime+ruta)` del log | segunda corrida sobre el mismo archivo | el archivo cambió de tamaño o mtime | evita re-parsear: 2ª corrida ≈ tiempo de deserializar |
| **Caching de consultas** | la respuesta Top-N ya calculada, por métrica y N | `(métrica, N)` | consultas repetidas en la misma sesión / modo servidor | llega un lote nuevo de líneas → se marca sucio | evita recomputar el heap en cada pregunta |

### Concurrencia / paralelismo (con justificación técnica)

- **Qué**: partir el archivo en C bloques alineados a `\n` (desde cada offset se avanza hasta el próximo salto de línea); W goroutines, cada una parsea su bloque y arma sus maps parciales; una fase de *reduce* los fusiona.
- **Por qué son independientes**: cada bloque es un conjunto disjunto de líneas; no hay estado compartido durante el parseo (map por worker, sin mutex).
- **Mecanismo Go**: goroutines + `sync.WaitGroup` + channel de resultados parciales. Worker pool acotado a `GOMAXPROCS`.
- **Problemas a mostrar**: overhead que no compensa en archivos chicos; memoria = C copias de maps; si en vez de map-por-worker se usa un map compartido con `sync.Mutex`, la contención lo hace **más lento que secuencial** (lo medimos a propósito); balance de carga si los bloques no son parejos.

### Perfilado (herramientas Go)

El enunciado lista herramientas de Python. Equivalencias en Go — con la ventaja de que **todo viene empaquetado en el toolchain**, sin instalar nada:

| Python | Go | Qué hace |
|---|---|---|
| `timeit` | `testing.B` / `go test -bench` | micro-benchmarks con `b.N` elegido automáticamente y resultados en ns/op |
| `line_profiler` | `go tool pprof -list <func>` | costo anotado línea por línea de una función |
| `memory_profiler` | `-benchmem` + heap profile (`-memprofile`) | asignaciones/op y bytes/op; perfil de heap con `pprof` |
| `scalene` | `go tool pprof` | CPU y heap en la misma herramienta (top / list / web / graph) |
| — | `go tool trace` | línea de tiempo de goroutines, GC, scheduler, bloqueos — sin equivalente directo en Python |
| — | `go test -race` | detector de condiciones de carrera en la versión concurrente |
| — | `go build -gcflags=-m` | reporte de escape analysis e inlining (por qué algo va al heap) |

- `go test -bench` con `-cpuprofile` / `-memprofile` / `-benchmem` para el antes/después de cada versión.
- `go tool pprof` (top, list, web) para ver dónde se va el tiempo y las asignaciones.
- `go tool trace` para goroutines, GC y balance en la versión concurrente.
- Responder explícitamente: dónde está el cuello de botella, qué se cambió, qué impacto tuvo, si se sostiene al crecer n, qué se pagó a cambio.

---

## 4. Fases de desarrollo

> Regla del proyecto: **un tema por vez**, teoría antes que código, ciclo de corrección iterativo (correr → pegar salida → si hay algo raro, encontrar la causa y arreglarla de raíz).

### Fase 0 — Teoría y setup ✅
- ~~Repasar en el deck (skill `teoria-curso`)~~ → resumen en [`TEORIA.md`](TEORIA.md): complejidad, estructuras, Top-N/heaps, batching/map-reduce, CPU vs I/O bound, memoización vs caching, profiling, optimización prematura, con números de slide.
- ~~`go mod init parcial1`; copiar `internal/bench` de `practico4`~~ → hecho (`go.mod` module parcial1, go 1.26.5; `internal/bench/bench.go` copiado).
- **Hecho**: `go build ./...` y `go vet ./...` pasan en verde.

### Fase 1 — Generador de datos ✅
- [`internal/loggen/loggen.go`](internal/loggen/loggen.go) + [`cmd/generar/main.go`](cmd/generar/main.go). Formato Apache Combined.
  - Timestamps por **proceso de Poisson no homogéneo** (curva horaria + fin de semana) → salida cronológica, pico 11 h.
  - **Muestreo Zipf** para endpoints (27 rutas), IPs (pool de 200 heavy + cola larga) y usuarios; status/bytes/latencia por categoría de endpoint.
  - **3 anomalías** con ventana exacta en `ground-truth.json`: (a) ráfaga de 500 POST a `/usr/login` en 30 s desde una IP nueva, (b) ~600 5xx en `/api/checkout` en 1 h, (c) escaneo de 41 rutas por una IP en 2 min. Calibradas para ser invisibles en el agregado a escala ≥ 1M pero detectables por ventana.
  - `ground-truth.json` incluye además los agregados exactos (top endpoints/status/IPs, solicitudes por hora, hora pico) → validación automática del baseline en Fase 2.
  - Flags: `-lineas -semilla -salida -verificacion -desde -tasa -heavy`.
  - Tests en [`loggen_test.go`](internal/loggen/loggen_test.go): determinismo, orden cronológico, sesgo de distribución, patrón horario, presencia de anomalías.
- **Hecho**: 1 M líneas en ~1,7 s (184 MB); `go test ./internal/loggen/` en verde; distribuciones sesgadas verificadas.

### Fase 2 — Baseline (v0) ✅
- [`internal/parse`](internal/parse/parse.go): `Parsear(string)` con **una regex + submatches** + `time.Parse` + `strconv.Atoi`.
- [`internal/agregado`](internal/agregado/agregado.go): `Contadores` (`map[int]int` estados, `map[string]int` endpoints/IPs/usuarios, `[24]int` por hora) + `TopN[K]` genérico por **sort completo** O(u·log u).
- [`cmd/analizar`](cmd/analizar/main.go): `bufio.Scanner` secuencial, mide tiempo/heap/total-asignado/GC, reporta con `tabwriter` (rankings + histograma horario), **valida contra `ground-truth.json`** y agrega fila a `resultados/comparativa.csv`.
- Tests: parseo de líneas válidas/malformadas, contadores, orden y desempate de `TopN`, y roundtrip generar→parsear→agregar contra el ground truth.
- **Hecho**: 1 M líneas, validación contra `ground-truth.json` toda en OK. Baseline: **~3,1–3,8 s**, heap retenido **98,6 MB**, **584 MB** total asignado (6× el heap → presión de GC del regex), 3,0 M asignaciones, 30 ciclos de GC.

### Fase 3 — Documento de complejidad ✅
- [`COMPLEJIDAD.md`](COMPLEJIDAD.md): derivación de parseo (regex RE2 vs manual, ambos O(n·L) — la diferencia es constante+GC), conteo (hash O(1) amort. vs slice O(n²), espacio O(n) por las IPs), Top-N (sort O(u·log u) vs heap O(u·log k), punto de equilibrio, tabla de `log u / log k` por métrica), I/O como piso de rendimiento, y vistazo a merge concurrente y anomalías.
- Cubre las 4 formas que menciona el enunciado: scan O(n), hashmap O(1), sort O(n log n), todos-contra-todos O(n²).
- **Hecho**: cada cota con derivación (recurrencias, amortización, tamaño del autómata), no sólo notación. Alimenta la slide 3 de la presentación.

### Fase 4 — Optimización 1: Top-N con heap ✅
- [`internal/agregado/topn_heap.go`](internal/agregado/topn_heap.go): `TopNHeap` con `container/heap`, min-heap acotado a n (raíz = el "peor" según `peorQue`, mismo orden que `TopNSort` → resultados idénticos, verificado en `TestSortHeapEquivalentes` incluso con empates masivos).
- `agregado.TopN(m, n, Metodo)` despacha entre `MetodoSort` y `MetodoHeap`; `cmd/analizar` toma `-topn-metodo` (default `heap` = v1).
- Benchmark [`internal/agregado/bench_test.go`](internal/agregado/bench_test.go) variando u y k → [`resultados/fase4-topn.txt`](resultados/fase4-topn.txt).
- **Resultados (benchmark aislado):**

  | u (claves) | k | sort | heap | |
  |---|---|---|---|---|
  | 15 (status) | 10 | **0,39 µs**, 1 alloc | 1,24 µs, 23 allocs | sort gana: overhead del heap |
  | 40 (endpoints) | 10 | 1,75 µs | 1,84 µs | empate |
  | 100 000 | 10 | 15,2 ms, 1,6 MB | **1,1 ms**, 680 B | **heap 14×** |
  | 1 200 000 (IPs) | 10 | 220 ms, **19 MB** | **16 ms**, 680 B | **heap 14× y 28 000× menos memoria** |
  | 1 200 000 | 1000 | 217 ms | 18 ms | heap 12× |
  | 20 000 | 10 000 (k≈u/2) | **2,8 ms** | 4,4 ms | sort gana |
  | 20 000 | 18 000 (k≈u) | **2,9 ms** | 6,1 ms | sort gana 2× |

- **Resultados (pipeline completo, 5 M líneas):** sort 24,3 s vs heap 23,5 s → **~3 %**, dentro del ruido. El Top-N es <3 % del tiempo total porque el parseo con regex domina el 97 %. Decisión: **heap igual queda como default** — es estrictamente mejor a u grande, gratis a u chico, y va a pesar cuando (a) la Fase 5 arregle el parseo y (b) a 20 M líneas el slice volcado de `sort` sean ~128 MB sobre los ~2 GB de mapas.
- **Confirma** la tabla `log u / log k` de `COMPLEJIDAD.md`: heap conviene solo cuando `n ≪ u`.

### Fase 5 — Optimización 2: parseo manual ✅
- [`internal/parse/manual.go`](internal/parse/manual.go): `ParsearManual(string)` recorre la línea con `strings.IndexByte` (helper `campo`); los campos son **sub-slices del string de entrada** (cero copias). Fecha por posiciones fijas + `time.Date` (sin `time.Parse`); zonas horarias **precalculadas** por offset (`zonasOffset`), sin asignar una `*Location` por línea. `atoiSimple` sin `strconv` ni error con formato.
- `cmd/analizar -parser regex|manual` (default `manual` = v2). También `-cpuprofile` / `-memprofile`.
- Tests: los mismos casos corren sobre las dos implementaciones; `TestManualIgualQueRegex` compara campo a campo sobre 20 k líneas.
- **Benchmark aislado** ([`resultados/fase5-parseo.txt`](resultados/fase5-parseo.txt)): regex **2,9 µs/op, 385 B, 2 allocs** → manual **175 ns/op, 0 B, 0 allocs** = **~16× más rápido, sin asignar**.
- **Pipeline completo (1 M líneas):** 3,2 s → **0,70 s** (~5×); total asignado 584 → **217 MB**; asignaciones 3,0 → **1,0 M**; GC 30 → **12**. (5 M: ~24 s → ~4,6 s.)
- **pprof antes/después:** en v1 el regex es ~70 % del CPU (`tryBacktrack`, `shouldVisit`, `MatchRune`). En v2 el parseo baja a ~17 %; ahora domina la **inserción en los mapas** (`mapassign_faststr`, `aeshashbody` ~35 %), el **GC/alloc del `scanner.Text()`** (~30 %) y las **syscalls de lectura** (`cgocall` ~18 %). → El `Text()` por línea (la única asignación que queda) y el CPU de los mapas son lo que ataca la Fase 7 (concurrencia).
- **Hecho**: parser manual idéntico al de regex, 16× en aislado / 5× en el pipeline, validación contra ground truth en OK.

### Fase 6 — Caching / memoización ✅
- [`internal/cache/cache.go`](internal/cache/cache.go), dos estrategias (el enunciado pide diferenciarlas):
  - **Memoización** (`Guardar`/`Cargar`): los `Contadores` completos con `gob` + metadatos del log (tamaño + mtime + versión). Función pura del archivo → sin política de expiración: si el log no cambió, el resultado vale. `cmd/analizar -cache`.
  - **Cache de consultas** (`Consultas`): respuestas Top-N por `(métrica, n)` con **marca de sucio** — `Agregar` invalida, la próxima consulta recalcula. Con contadores de hits/misses; el reporte + la validación piden los mismos rankings → 3 hits reales por corrida.
- **Medido (1 M líneas)**: 2ª corrida con cache = **131 ms** vs ~1,3 s de cómputo (**~10×**); archivo de cache 6,1 MB. `touch` al log → miss "el log cambió"; tests cubren tamaño, mtime, versión, hits/misses e invalidación por datos nuevos.
- **Hallazgo colateral (¡el mejor de la fase!)**: comparando heap del camino parseado (98,6 MB) vs el deserializado (19,8 MB) apareció **aliasing de claves**: la clave del map era un sub-slice de la línea y retenía viva la línea entera (~190 B por clave única). Arreglo en `agregado.incrementar`: **clonar la clave solo al insertarla** → heap retenido **98,6 → 19,9 MB (5×)** con costo de tiempo nulo (verificado con A/B intercalado). Aplica a todas las versiones.
- **Lección de medición**: una comparación ingenua entre corridas separadas mostró "el clon cuesta 2×"; el A/B intercalado mostró que era **la máquina** que se había puesto 2× más lenta entre sesiones de medición. Números anómalos → A/B en el momento, nunca comparar contra corridas viejas.
- **Hecho**: 2ª corrida cae a tiempo de deserialización, invalidación correcta, tests en verde.

### Fase 7 — Concurrencia (map-reduce) ✅
- [`internal/pipeline/pipeline.go`](internal/pipeline/pipeline.go): `Secuencial`, `Concurrente` (bloques por offsets, alineación a `\n` con la regla "la línea pertenece al bloque de su primer byte", contadores por worker sin estado compartido, `WaitGroup` + `Fusionar` en `agregado`) y `ConcurrenteMutex` (contador único bajo mutex, a propósito).
- `cmd/analizar -modo seq|conc|conc-mutex -workers N -trace archivo`. Traza en `resultados/perfiles/trace-conc8.out`.
- **Resultados** ([`resultados/fase7-concurrencia.txt`](resultados/fase7-concurrencia.txt)):
  - 1 M: seq 1,1 s → conc w=8 **0,36–0,40 s (~2,9×)**; se estanca en w≥8 (de 12 hilos).
  - 5 M: seq 6,6 s → conc w=8 **2,6 s (2,6×)**.
  - 20 k líneas: incluso ahí conc gana ~2× — el overhead fijo recién dominaría a tamaños de KB.
  - **`conc-mutex` w=8: 1,2 s — más lento que secuencial** usando 8 núcleos: el lock serializa el trabajo útil. La moraleja del requisito 6: la concurrencia paga por *independencia* (mapas parciales + reduce), no por "usar goroutines".
  - Por qué no es lineal: el trabajo por línea es corto y limitado por memoria (misses de los mapas), allocator/GC compartido e I/O — se satura el ancho de banda antes que el CPU.
- Tests: igualdad exacta seq vs conc para w ∈ {2,3,7,8,16,61} (ejercita todos los límites de bloque), archivo minúsculo/vacío, errores sumados entre workers. (`-race` requiere cgo/gcc, no disponible en esta máquina; el diseño no comparte estado y los tests de igualdad cubren el resultado.)
- **Hecho**: resultados idénticos al secuencial, speedup ~3× (sub-lineal, explicado), contención demostrada.

### Fase 8 — Detección de anomalías ✅
- [`internal/anomalias/anomalias.go`](internal/anomalias/anomalias.go), tres detectores O(1) amortizado por evento en una pasada secuencial (`pipeline.Recorrer`): la detección **necesita orden temporal**, por eso no corre dentro del map-reduce (las ventanas se romperían en los límites de bloque) — tradeoff documentado.
  - **Ráfaga**: ventana deslizante de timestamps por IP (≥100 ev/60 s) con histéresis de episodios. Umbral elegido entre la IP normal más activa (~15/min) y el escáner (~66/min), para que cada ataque lo reporte un solo detector.
  - **Pico 5xx**: conteo por franja horaria + umbral `μ + 3σ` **leave-one-out**. Lección: con pocas franjas, el outlier infla σ y *se esconde a sí mismo* — μ/σ se calculan sin la franja candidata.
  - **Escaneo**: rutas distintas con 404 en ventana de 10 min por IP (≥15). El discriminador no es "toca muchas rutas" (un heavy hitter legítimo las toca todas en días) sino "muchas rutas con 404 en minutos". Umbral 10 daba falsos positivos con la IP más activa → 15.
- `cmd/analizar -anomalias` + matching contra el ground truth (tipo + IP/endpoint + solapamiento de ventanas) con **precisión/recall**.
- **Resultado (1 M)** ([`resultados/fase8-anomalias.txt`](resultados/fase8-anomalias.txt)): **recall 3/3, precisión 100 % (0 falsos positivos)**; pasada extra ~1,2 s. Tests unitarios por detector + negativo (tráfico intenso legítimo no dispara) + integración contra ground truth a 300 k.
- **Hecho**: las 3 anomalías inyectadas detectadas sin falsos positivos.

### Fase 9 — Perfilado consolidado ✅
- [`PERFILADO.md`](PERFILADO.md): responde las 5 preguntas del punto 7 con perfiles de v1/v2/v3 capturados en **una misma tanda** (5 M). Crudos en [`resultados/fase9-perfiles.txt`](resultados/fase9-perfiles.txt); `.pprof` + traza en `resultados/perfiles/`.
- La historia del cuello de botella: v1 regex ~70 % CPU → v2 se reparte (mapas ~30 %, GC del string por línea ~25 %, parseo ~21 %) → v3 sin dominador y con `semasleep` (saturación de ancho de banda, explica el techo de ~2,9×).
- Tanda 5 M: v1 41,0 s → v2 7,2 s (5,7×) → v3 2,9 s (2,5×) = **14,3× total**; asignado 3036 → 1201 MB; GC 108 → 31.
- `alloc_space` identifica el residuo: 935 MB (78 %) es el string por línea de `ReadString` → optimización futura documentada con su hipótesis (parsear sobre `Bytes()`, posible gracias al clon de claves) y por qué se corta ahí (optimización prematura).
- Trampas documentadas: A/B intercalado, `runtime.KeepAlive` para el perfil `inuse` (el compilador daba por muertos los contadores y el perfil salía vacío), inuse vs alloc, `go run` vs binario.
- **Hecho**: las 5 preguntas respondidas con evidencia de perfil por versión.

### Fase 10 — Tabla comparativa y escalabilidad ✅
- Tanda única y continua sobre **1 M / 5 M / 20 M** (20 M = 3,59 GB) con el binario compilado → [`resultados/comparativa.csv`](resultados/comparativa.csv), [`resultados/escalabilidad.csv`](resultados/escalabilidad.csv), resumen en [`resultados/fase10-escalabilidad.txt`](resultados/fase10-escalabilidad.txt).
- [`cmd/grafico`](cmd/grafico/main.go) (solo stdlib: el SVG es texto) genera [`resultados/escalabilidad.svg`](resultados/escalabilidad.svg): **log-log, 4 rectas de pendiente ~1 = O(n) a la vista**.
- Validación @20 M contra ground truth: toda OK.
- Hallazgo fino: la *forma* es O(n) pero la constante de v3 sube ~40 % de 1 M a 20 M (3,06 → 1,84 M líneas/s): los mapas de IPs (0,4 → 8 M claves) desbordan las cachés. v0 en cambio es plano (~140 k/s): el regex domina tanto que esconde el efecto.
- **Hecho**: tabla obligatoria completa (sección 5) y gráfico con pendientes.

### Fase 11 — Presentación ✅ (falta solo el ensayo)
- [`presentacion.html`](presentacion.html): deck navegable de 11 diapositivas (portada + las 10 de la estructura del enunciado), con **todos los números medidos**, el gráfico de escalabilidad embebido, el guion de la demo en vivo y tiempos sugeridos por slide (~15 min total). Teclas ← →; **imprimir = handout** con todas las diapositivas.
- Publicada como artifact para compartir/proyectar desde el navegador.
- **Pendiente del grupo**: ensayo cronometrado y agregar los nombres en la portada.

---

## 5. Tabla comparativa obligatoria (FINAL — tanda única de la Fase 10)

Binario compilado, misma máquina, corridas continuas. Referencia: **5 M de líneas (919 MB)**.
Todas las versiones incluyen el clon de claves de la fase 6 (por eso el heap es igual entre versiones).

| Versión | Tiempo (5 M) | Heap retenido | Total asignado | Observación |
|---|---|---|---|---|
| v0 — baseline (regex + sort + secuencial) | 33,6 s | 137,7 MB | 3036 MB · 17,0 M allocs · 121 GC | Línea base |
| v1 — Top-N con heap | 35,2 s | 137,4 MB | 3036 MB | Top-N 14× y 28 000× menos memoria **en aislado**; invisible en el total (el parseo domina) |
| v2 — parseo manual sin regex | **6,81 s** | 137,5 MB | **1201 MB · 7,0 M allocs · 49 GC** | **4,9×.** El regex era ~70 % del CPU |
| v3 — map-reduce concurrente (w=8) | **2,47 s** | 137,3 MB | 1421 MB · 31 GC | **13,6× vs v0**; `conc-mutex` es MÁS LENTO que seq (contención medida) |
| v4 — + memoización (2ª corrida) | **0,76 s** | 137,5 MB | 213 MB · 6 GC | **44× vs v0**: deserializa ~29 MB de gob en vez de re-parsear 919 MB |
| **Versión final (v3, y v4 si el log no cambió)** | **2,47 s / 0,76 s** | **137 MB** | | Resultado final |

**Escalabilidad de la versión final** (misma tanda; gráfico log-log en [`resultados/escalabilidad.svg`](resultados/escalabilidad.svg)):

| Líneas | v0 baseline | v3 conc (final) | speedup | heap retenido |
|---|---|---|---|---|
| 1 M (184 MB) | 7,26 s | **0,33 s** | 22,2× | 19,9 MB |
| 5 M (919 MB) | 33,6 s | **2,47 s** | 13,6× | 137 MB |
| 20 M (3,59 GB) | 145,7 s | **10,9 s** | 13,4× | 549 MB |

La mejora se sostiene al crecer (pendiente ~1 en log-log = O(n)); el heap crece O(u_ip) ≈ O(n) — sin el clon de claves de la fase 6 serían ~2,7 GB a 20 M. Validación contra ground truth: OK en los tres tamaños.

> Nota histórica: las mediciones preliminares por fase (con `go run`, sesiones distintas) están en los `fase*.txt`; los números comparables entre sí son los de esta tanda.

---

## 6. Presentación oral (10–15 min)

Estructura del enunciado, con el contenido nuestro y quién la puede llevar:

| # | Slide | Contenido concreto | Tiempo |
|---|---|---|---|
| 1 | El problema | Analizar logs de servidor a escala; por qué es interesante: parseo (CPU) + lectura (I/O) + agregación, todo sobre millones de líneas | 1 min |
| 2 | Diseño inicial | Arquitectura (diagrama del pipeline: leer → parsear → contar → Top-N → reportar); la v0 con regex y sort | 1.5 min |
| 3 | Complejidad | Parseo O(n·L), Top-N sort O(u·log u) vs heap O(u·log k), merge O(u·C); derivación de cada una | 2 min |
| 4 | Algoritmos y estructuras | Alternativas consideradas y decisiones: regex→manual, sort→heap, map-por-worker vs map+mutex | 2 min |
| 5 | Memoización y caching | Qué se guarda (agregados a disco / respuestas Top-N), clave, invalidación, mejora medida | 1.5 min |
| 6 | Concurrencia | Particionado por bloques alineados a `\n`, worker pool, reduce; independencia; trace; overhead en archivos chicos | 2 min |
| 7 | Perfilado | pprof antes/después (regex arriba → desaparece), memprofile (caída de asignaciones), trace de goroutines | 1.5 min |
| 8 | Comparación de resultados | Tabla obligatoria + gráfico tiempo-vs-n (1M/5M/20M); qué versión ganó | 1.5 min |
| 9 | Demo de código | `generar -lineas 10000000` y `analizar` en modo secuencial vs concurrente en vivo; mostrar el parser manual y el heap | 1.5 min |
| 10 | Conclusiones | Qué optimización pegó más fuerte (probable: parseo manual), qué no rindió lo esperado (¿cache? ¿concurrencia en archivos chicos?), qué haríamos distinto | 1 min |

Demo lista de antes: tener `logfiles.log` de 10 M ya generado por si la generación en vivo tarda.

---

## 7. Reparto de trabajo sugerido

Tres frentes que se pueden avanzar bastante en paralelo una vez cerrada la Fase 1:

- **Frente A — datos y parseo**: Fases 1, 5 (generador, parser regex y manual, benchmarks de parseo).
- **Frente B — agregación y concurrencia**: Fases 2, 4, 7 (pipeline, Top-N heap, map-reduce).
- **Frente C — anomalías, cache, medición**: Fases 6, 8, 9, 10 (detector, cache, perfilado, tablas y gráficos).

La Fase 3 (complejidad) y la 11 (presentación) se escriben entre todos con lo que produjo cada frente.

---

## 8. Riesgos y cosas a cuidar

- **Datos sin sesgo = Top-N sin sentido.** Verificar las distribuciones del generador antes de construir nada encima (lección del Práctico 4: verificar la hipótesis antes de armar el benchmark).
- **Concurrencia que no rinde.** Es un resultado válido y esperable en archivos chicos; hay que medirlo y explicarlo, no esconderlo.
- **Medir preparación junto con el trabajo.** `b.StopTimer()` / `runtime.GC()` alrededor de la carga del archivo y la generación de datos.
- **El cache en batch puede sonar forzado.** Por eso mostramos las dos variantes (memoización de agregados + cache de consultas) y explicamos la diferencia conceptual que pide el enunciado.
- **Salida no determinista de los `map` de Go.** Ordenar antes de reportar o comparar (lección del Práctico 4 ej. 3).
- **20 M de líneas ≈ varios GB de texto.** Confirmar que hay disco; si no, tope en 10 M y se explica.
- **No parchear síntomas.** Si un número del benchmark es raro, encontrar la causa antes de seguir.

---

## 9. Comandos

```bash
cd parcial1
go mod init parcial1                      # una vez
go run ./cmd/generar  -lineas 1000000     # genera logfiles.log + ground-truth.json
go run ./cmd/analizar -archivo logfiles.log -modo conc -workers 8 -topn 10
go test ./... -bench . -benchmem -cpuprofile cpu.pprof -memprofile mem.pprof
go tool pprof -http=: cpu.pprof            # top / list / graph
go tool pprof -list '.*procesarBloque' cpu.pprof   # equivalente a line_profiler
go tool trace trace.out                   # goroutines, GC, scheduler
go test ./internal/agregado/ -run TestConcurrente -race   # carreras en el map-reduce
go build -gcflags=-m ./internal/parse 2>&1 | grep escapes   # escape analysis
go vet ./...
gofmt -l .
```
