# Segundo parcial — Programación Eficiente

**Checa, Sol · Gómez Geneiro, Facundo · Nicolais, Manu**

Sobre el analizador de logs del primer parcial se aplicaron los diez temas del segundo
(ver [`REQUERIMIENTOS.md`](REQUERIMIENTOS.md)). Este documento explica qué se hizo en cada
uno, con qué herramienta, y qué se midió. Toda la evidencia está en
[`resultados/segundo-parcial/`](resultados/segundo-parcial/),
[`resultados/calidad/`](resultados/calidad/) y [`resultados/compilador/`](resultados/compilador/).

Todas las mediciones son sobre la misma máquina (AMD Ryzen 7 7730U, 8 núcleos / 16 hilos,
Windows 11, Go 1.26.5), con el binario compilado y corridas intercaladas entre versiones
para que el ruido de la máquina afecte a ambas por igual.

---

## Resultado

Versión final del primer parcial contra versión final del segundo, sobre el mismo log y el
mismo día, 16 workers (mediana de 5 corridas intercaladas; 3 a 20 M):

| Líneas (tamaño) | Primer parcial (final) | Segundo parcial (final) | Speedup | Memoria asignada | Asignaciones | Ciclos de GC | Heap retenido |
|---|---:|---:|---:|---:|---:|---:|---:|
| 1 M (184 MB) | 358 ms | **127 ms** | **2,8×** | 267 → 76 MB | 1,41 → 0,42 M | 9 → 5 | 20,0 → 23,4 MB |
| 5 M (919 MB) | 2 620 ms | **814 ms** | **3,2×** | 1 429 → 451 MB | 7,04 → 2,04 M | 20 → 8 | 137 → 155 MB |
| 20 M (3,6 GB) | 13 375 ms | **3 986 ms** | **3,4×** | 5 667 → 1 770 MB | 28,1 → 8,1 M | 25 → 11 | 548 → 612 MB |

El speedup **crece con el volumen**: la fusión secuencial que se eliminó escalaba con la
cantidad de IPs distintas, que crece con el log. Contra la línea base del primer parcial
(regex + ordenamiento completo + secuencial, medida en el primer parcial), la versión final
es **57× más rápida a 1 M, 41× a 5 M y 37× a 20 M** (7,3 s → 0,13 s; 33,6 s → 0,81 s;
145,7 s → 4,0 s). Con la memoización (segunda corrida sobre el mismo log), 20 M líneas se
resuelven en **1,8 s** contra 4,0 s del primer parcial (sección 4). Datos crudos:
[`escalabilidad.csv`](resultados/segundo-parcial/escalabilidad.csv); gráfico log-log:
[`escalabilidad.svg`](resultados/segundo-parcial/escalabilidad.svg).

**Trade-off:** el heap retenido sube entre un 12 y un 17 %. Es el costo del `Contador`
(guarda un slice de conteos además del mapa) y de las particiones pre-dimensionadas de la
fusión paralela. A cambio, se asigna menos de un tercio de la memoria y el GC corre menos de
la mitad de las veces.

Cada corrida de la versión final se valida contra el ground truth del generador (total de
líneas, hora pico y top-10 de endpoints, estados e IPs idénticos clave por clave). La
detección de anomalías da **recall 3/3 y precisión 100 % a 1 M, 5 M y 20 M**
([`validacion-20m.txt`](resultados/segundo-parcial/validacion-20m.txt)) y en otras 15
semillas ([`anomalias-semillas.txt`](resultados/segundo-parcial/anomalias-semillas.txt)).
La versión del primer parcial solo detectaba 2 de 3 a 20 M (sección 9, bug 8).

| Indicador de calidad | Primer parcial | Segundo parcial |
|---|---:|---:|
| Issues de `golangci-lint` (21 linters + 2 formateadores) | 42 | **0** |
| Funciones inalcanzables (`deadcode`) | 15 | **1** (justificada) |
| Cobertura de tests | 51,0 % | **92,6 %** |
| Binarios / `main` con lógica | 3 | **1** (sin lógica) |
| Bugs encontrados por las herramientas nuevas | — | **8** (sección 9) |

De dónde salió la ganancia, en orden de impacto:

| Cambio | Tema | Efecto medido |
|---|---|---|
| Fusión (reduce) en paralelo, particionada por hash | Paralelismo | `fusionar`: 1 747 → 133 ms; `procesar`: 2 493 → 716 ms |
| Lectura por lotes con buffer reutilizado | Archivos grandes | asignado: 1 429 → 612 MB; asignaciones: 7,0 → 2,0 M |
| PGO (optimización guiada por perfil) | Compilador | −7,5 % total (p = 0,000), −17 % el parser |
| `Contador` (clave → índice) | Refactorización | un hash por línea en vez de dos; corrige un bug |
| Carga del cache con índice armado en paralelo | Archivos grandes | memoización a 20 M: 4,0 → 1,8 s |

---

## 1. Eliminación de código redundante

**Herramientas:** [`deadcode`](https://pkg.go.dev/golang.org/x/tools/cmd/deadcode) (análisis de
alcanzabilidad desde `main`) y los linters `dupl`, `goconst` y `unparam` de golangci-lint.

`deadcode` encontró **15 funciones inalcanzables** ([antes](resultados/calidad/deadcode-antes.txt)):
14 eran el paquete `internal/bench` completo, copiado del práctico 4, del que el proyecto
solo usaba una semilla aleatoria. Se eliminó el paquete. La que queda
([después](resultados/calidad/deadcode-despues.txt)) es `Consultas.Agregar`, que solo se
alcanza desde los tests y se conserva a propósito: es el camino de **invalidación** del cache
que pide el requisito 5 del primer parcial.

Código duplicado eliminado:

| Duplicación | Dónde estaba | Ahora |
|---|---|---|
| Bloque `ReadMemStats` antes/después | `analizar` y `cargarMemoizado` | [`medicion.Medir`](internal/medicion/medicion.go) |
| Tres funciones para formatear bytes | `mb`, `formatearBytes`, `bench.FormatearBytes` | `medicion.Bytes` (genérica) |
| Arranque de perfil de CPU y de traza (detectado por `dupl`) | `cmd/analizar` | `iniciarPerfiles` |
| Fan-out de goroutines por bloque | `Concurrente` y `ConcurrenteMutex` | `enBloques` |
| Consulta con cache, una por tipo de clave | `topDeStr` y `Estados` | `Consultas.consultar` |
| Top-N y hora pico del ground truth | reimplementados en `loggen` | usa `agregado.TopNSort` y `agregado.HoraPico` |
| Comparación de rankings campo a campo | `coincideRanking` + `resumenRanking` | `PrimeraDiferencia` (mismo tipo `Par` en ambos lados) |
| Comparador de orden escrito a mano (×3) | `TopNSort`, `peorQue`, `grafico` | `CompararRanking` con `cmp.Compare` |
| Manejo de flags y errores ×3 programas | `cmd/generar`, `cmd/analizar`, `cmd/grafico` | un binario Cobra (sección 10) |
| Literales repetidos (`"api"` ×13, `"estatico"` ×11, …) | `loggen` | constantes con nombre |
| Fusiones secuenciales que quedaron sin uso | `Contadores.Fusionar`, `Contador.Fusionar` | eliminadas |

Un detalle sobre el último punto: `deadcode` **no** las detectó, porque `Contadores` se
serializa con `gob` (reflexión) y el análisis da por alcanzables todos los métodos de un tipo
que pasa por una interfaz. Se encontraron con el reporte de cobertura (0 %), que es la otra
herramienta que muestra código sin uso.

## 2. Compilador y herramientas de optimización

En Go, el compilador (`gc`) y su toolchain ocupan el lugar de Scalene/py-spy o de JFR/JMC:
`pprof` y `go tool trace` ya se usaron en el primer parcial; acá se sumaron **PGO** y los
**diagnósticos del compilador**.

### Diagnósticos de inlining y de escape (`-gcflags=-m`)

```bash
go build -gcflags='-m=2' ./internal/parse ./internal/agregado ./internal/pipeline
```

El compilador inlinea una función si su "costo" no supera 80. En el camino caliente
([`inlining-sin-pgo.txt`](resultados/compilador/inlining-sin-pgo.txt)):

| Función | Costo | ¿Se inlinea? | Llamadas por línea |
|---|---:|---|---:|
| `(*cursor).saltar` | 29 | sí | 7 |
| `atoiSimple` | 72 | sí | 9 |
| `(*cursor).hasta` | 111 | **no** | 10 |
| `(*Contador).Incrementar` | 135 | **no** | 3 |

El análisis de escape ([`escape.txt`](resultados/compilador/escape.txt)) confirma que el
camino caliente **no asigna memoria**: el `cursor` vive en el stack, y lo único que va al heap
son los `fmt.Errorf` de líneas inválidas y el `strings.Clone` al insertar una clave nueva.

### PGO — optimización guiada por perfil

Desde Go 1.21, si el paquete `main` tiene un `default.pgo` (un perfil de CPU), `go build` lo
usa para optimizar los puntos calientes: les sube el presupuesto de inlining y desvirtualiza
llamadas indirectas.

```bash
# perfil representativo: 3 análisis + 1 con anomalías, fusionados
analizador analizar -a datos/5m.log --cpuprofile cpu.pprof
go tool pprof -proto cpu1.pprof cpu2.pprof ... > cmd/analizador/default.pgo
go build ./cmd/analizador        # usa default.pgo automáticamente (-pgo=auto)
```

Con el perfil, el compilador inlinea `cursor.hasta` en sus **10** llamadas, `Incrementar` en
las **3**, y también `parteDe` y `zonaDe`
([`inlining-con-pgo.txt`](resultados/compilador/inlining-con-pgo.txt)). Sin PGO, ninguna.

| Medición | Sin PGO | Con PGO | Diferencia |
|---|---:|---:|---|
| Análisis completo, 5 M líneas (n = 12) | 764 ms | 707 ms | **−7,5 %** (p = 0,000) |
| `ParsearManual` por línea (n = 10) | 436 ns | 361 ns | **−17,2 %** (p = 0,000) |
| Memoria asignada | 450 MiB | 451 MiB | sin cambio (p = 0,47) |

([`benchstat-pgo-5m.txt`](resultados/compilador/benchstat-pgo-5m.txt),
[`benchstat-pgo-parser.txt`](resultados/compilador/benchstat-pgo-parser.txt)). La comparación
estadística la hace [`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat):
informa la mediana, el intervalo de confianza y el p-valor (Mann-Whitney), así que "−7,5 %"
no es una corrida con suerte. **Trade-off:** el binario crece (más código inlineado) y el
perfil hay que regenerarlo cuando cambia el código caliente; uno viejo no rompe nada, pero
deja de ayudar.

### Eliminación de bounds checks

```bash
go build -gcflags='-d=ssa/check_bce/debug=1' ./internal/parse
```

Quedan 4 chequeos de límites en el parser
([`bounds-check-antes.txt`](resultados/compilador/bounds-check-antes.txt)): dos en
`cursor.hasta`, porque el compilador no puede probar que el índice que devuelve `IndexByte`
está en rango, y dos en la separación ruta/protocolo. **`parsearFecha` no tiene ninguno**,
aunque indexa 14 posiciones fijas: el `if len(s) != 26` del comienzo le alcanza al
compilador para probar que todos son válidos. No hizo falta agregar pistas a mano.

## 3. Herramienta general de monitoreo: OpenTelemetry + Jaeger

De la lista (YourKit, New Relic, Elastic APM, "cualquier otra") se eligió
**OpenTelemetry**, el estándar abierto de instrumentación, con **Jaeger** como backend
local. El código instrumentado solo depende de la API de OpenTelemetry; el destino lo decide
[`internal/telemetria`](internal/telemetria/telemetria.go) y viaja por **OTLP**, el mismo
protocolo que reciben New Relic, Elastic APM, Grafana Tempo y Datadog. Cambiar de
herramienta es cambiar el endpoint, no el código.

```bash
docker compose up -d                                        # Jaeger (docker-compose.yml)
analizador --otlp localhost:4318 analizar -a datos/5m.log   # http://localhost:16686
```

Cada corrida genera una traza con un span por etapa: `analizar` → `procesar` → un
`bloque` por worker (con offsets, líneas y errores como atributos) → `fusionar`, y
`anomalias`. Sin `--otlp` los spans son no-ops y no cuestan nada medible.

**Lo que encontró.** La primera traza de la versión con lectura por lotes mostró esto
(5 M líneas, 16 workers):

| Span | Versión | Inicio | Duración |
|---|---|---:|---:|
| `bloque` ×16 (en paralelo) | fusión secuencial | 2 ms | 646 – 744 ms |
| `fusionar` (secuencial) | fusión secuencial | 747 ms | **1 747 ms** |
| `bloque` ×16 | fusión paralela por hash | 2 ms | 518 – 562 ms |
| `fusionar` | fusión paralela por hash | 566 ms | 494 ms |
| `bloque` ×16 | particionado en origen | 2 ms | 531 – 581 ms |
| `fusionar` | particionado en origen | 584 ms | **133 ms** |

La fusión de los parciales, la única etapa secuencial del map-reduce, tardaba **más del
doble que todo el trabajo paralelo junto**: la ley de Amdahl a la vista. El perfil de `pprof`
lo mostraba solo como "14 % del CPU", porque suma el tiempo de los 16 hilos. La traza, al
mostrar cada etapa en la línea de tiempo real, dejó claro que era el cuello de botella. La
solución está en la sección 4.

## 4. Lectura y escritura de archivos grandes

### Lectura por lotes con buffer reutilizado (batching + buffer)

El perfil de memoria de la versión del primer parcial mostró que **el 64 % de lo asignado
(933 MB de 1 429 MB en 5 M líneas) era `bufio.Reader.ReadString`**: un string nuevo por
cada línea. Se reemplazó por un lector por lotes
([`pipeline/lotes.go`](internal/pipeline/lotes.go)):

- Cada worker lee bloques de 1 MiB a **un único buffer que reutiliza** y los corta en el
  último `'\n'`. La línea incompleta del final se copia al comienzo del buffer para el lote
  siguiente.
- Cada línea se entrega como una **vista sin copia** sobre el buffer (`unsafe.String`).
  El contrato, el mismo que el de `bufio.Scanner.Bytes()`: los strings de una `Entrada` solo
  valen durante el callback, y quien los retiene los clona. Hoy son dos consumidores: el
  `Contador` (clona al insertar una clave) y el detector de anomalías (clona IPs e interna
  rutas).

| Benchmark (200 k líneas, mismo parseo y conteo) | `ReadString` | Por lotes | |
|---|---:|---:|---|
| Tiempo | 266 ms | 204 ms | −23 % |
| Asignaciones | 281 k | 81 k | −71 % |
| Bytes asignados | 50,2 MiB | 12,0 MiB | −76 % |

El tamaño del lote importa, hasta un punto (lectura sin parseo, 37 MB):

| Lote | 512 B | 4 KiB | 64 KiB | **1 MiB** | 8 MiB |
|---|---:|---:|---:|---:|---:|
| Tiempo | 570 ms | 90 ms | 28 ms | **24 ms** | 28 ms |
| Throughput | 64 MiB/s | 408 MiB/s | 1,29 GiB/s | **1,48 GiB/s** | 1,30 GiB/s |

Con lotes chicos domina el costo de las llamadas al sistema; a partir de 64 KiB hay una
meseta, y 8 MiB ya no mejora y gasta más memoria (y caché).
([`bench-lectura.txt`](resultados/segundo-parcial/bench-lectura.txt))

### Escritura con buffer y formateo sin asignaciones

El generador escribe ~1 GB por cada 5 M líneas. Dos cambios, medidos aparte
([`bench-escritura.txt`](resultados/segundo-parcial/bench-escritura.txt)):

| Buffer de escritura (50 k líneas a disco) | sin buffer | 4 KiB | **64 KiB** | 1 MiB | 8 MiB |
|---|---:|---:|---:|---:|---:|
| Tiempo | 599 ms | 233 ms | **197 ms** | 198 ms | 201 ms |

| Formateo de una línea | `fmt.Fprintf` | `strconv.Append*` sobre buffer reutilizado |
|---|---:|---:|
| Tiempo | 3,80 µs | 2,12 µs (−44 %) |
| Asignaciones | 11 | **0** |

La salida del generador nuevo es **idéntica byte a byte** a la anterior (verificado con
SHA-1 sobre dos semillas), y `TestAgregarLineaIgualQueFprintf` lo sigue comprobando.

### Procesamiento en paralelo: también la fase reduce

El map-reduce del primer parcial paralelizaba el *map* (bloques por offsets) pero fusionaba
los parciales en un solo hilo. Con ~2 M de IPs distintas en 5 M líneas, esa fusión era el
cuello de botella (sección 3). La versión final
([`agregado.FusionarClaves`](internal/agregado/contador.go)):

1. **Particiona las claves por hash en origen:** cada worker cuenta sus IPs ya repartidas
   en P partes (`hash(ip) mod P`). Cuesta un hash extra por línea, repartido en 16 núcleos.
2. **Fusiona en paralelo:** la goroutine *g* suma la parte *g* de todos los workers. Las
   particiones son disjuntas, así que no comparten nada que se escriba: no hacen falta locks.
3. El resultado queda particionado y el Top-N lo recorre sin cambios, porque `TopN` acepta
   cualquier `Fuente` (un iterador `iter.Seq2`).

`fusionar`: **1 747 → 133 ms**. **Trade-off:** el heap retenido sube de 137 a 154 MB en 5 M
líneas, por el slice de conteos del `Contador` y los mapas pre-dimensionados de cada parte.

### Lectura del cache: la memoización que dejó de convenir

El cache de agregados del primer parcial (memoización en disco con `gob`) hizo falta
**rehacerlo**. Al pasar a `Contador`, su primera serialización codificaba un `map`: gob
armaba un mapa al decodificar y después se copiaba al índice, es decir, dos mapas de 8 M de
claves. **La segunda corrida a 20 M tardaba 8,7 s, el doble que procesar el archivo
(4,0 s).** Lo detectó la medición de punta a punta, no ningún test: el resultado era
correcto, solo lento.

| Memoización (2ª corrida) | 5 M | 20 M |
|---|---:|---:|
| Primer parcial (`map` en gob, un solo mapa) | 760 ms | 4 046 ms |
| `Contador` serializado como `map` | no medido | 8 524 – 9 053 ms |
| `Contador` como dos slices paralelos | 503 – 649 ms | 3 407 – 4 131 ms |
| **+ índice armado en paralelo** | **358 – 421 ms** | **1 670 – 2 002 ms** |

(Rangos de 2 a 6 corridas; el primer parcial, de su tabla de escalabilidad.)

La corrección tuvo dos pasos. Primero, serializar dos slices paralelos (claves y conteos),
que gob decodifica sin hashear. Segundo, armar el índice en paralelo: cada goroutine recorre
las claves y se queda con su partición, igual que en la fusión. La lección: cuando el
análisis se aceleró 3,4×, la carga del cache, que era secuencial, quedó casi tan lenta como
recalcular todo. Una optimización (memoizar) puede dejar de convenir cuando cambia el costo
de la alternativa, y hay que volver a medirla.

Para entradas que **no se pueden partir por offset**, como un `.gz` o `stdin`, se agregó un
productor-consumidor ([`pipeline.DesdeLector`](internal/pipeline/pipeline.go)): un lector
corta lotes y N workers los parsean. Los buffers circulan por un pool (un canal), así que la
memoria queda acotada a 2·N lotes sin importar el tamaño de la entrada. Descomprimir gzip
es inherentemente secuencial, pero el parseo sí se reparte.

```bash
analizador analizar -a logs.gz
zcat logs.gz | analizador analizar -a -
```

## 5. Legibilidad y mantenibilidad

En Go, el equivalente de PEP 8 no es una guía sino una herramienta: **`gofmt`** fija el
formato y no se discute. Encima de eso se siguieron *Effective Go* y las *Go Code Review
Comments*, verificadas con `revive` y `golangci-lint`:

- **Nombres:** se eliminó un identificador no ASCII (`montículoMin` → `monticuloMin`, lo
  marcó `asciicheck`), las variables que ocultaban funciones del lenguaje (`min` y `max`,
  regla `redefines-builtin-id` de revive) y una variable `csv` que tapaba al paquete
  `encoding/csv`. Los nombres de flags y campos quedaron alineados entre CLI y código
  (`--version-etiqueta` en vez de `-version`, que chocaba con el `--version` estándar).
- **Comentarios de documentación:** todo identificador exportado tiene su doc comment, que
  empieza con el nombre (regla `exported` de revive: 9 faltaban). Cada paquete tiene su
  comentario de paquete.
- **Funciones con una sola responsabilidad:** `loggen.Generar` bajó de complejidad cognitiva
  **38** a menos de 20, dividida en `completarOpciones`, `nuevosMuestreadores`,
  `corrienteBase`, `escritor` y `acumulador`. El `main` del analizador (complejidad **39**) se
  repartió en `internal/cli`, `internal/reporte` y `internal/medicion`.
- **Organización:** los `main` ya no tienen lógica; todo lo que imprime escribe sobre un
  `io.Writer`, así que se puede testear.

## 6. Generación de documentación

Go no necesita un Sphinx aparte: la documentación sale de los comentarios del código, y
las **funciones `Example`** son a la vez documentación y tests (`go test` verifica que la
salida coincida con el comentario `// Output:`).

| Herramienta | Uso |
|---|---|
| `go doc` | consulta en la terminal: `go doc parcial1/internal/agregado FusionarClaves` |
| [`pkgsite`](https://pkg.go.dev/golang.org/x/pkgsite/cmd/pkgsite) | el mismo sitio que pkg.go.dev, local: `go run golang.org/x/pkgsite/cmd/pkgsite@latest` → http://localhost:8080 |
| [`gomarkdoc`](https://github.com/princjef/gomarkdoc) | genera [`docs/api/`](docs/api/): un Markdown por paquete, versionable y legible en GitHub |

Se agregaron 6 ejemplos ejecutables (`ExampleTopN`, `ExampleContador`,
`ExampleFusionarClaves`, `ExampleParsearManual`, `ExampleBytes` y `ExampleBarra`) que
aparecen en la documentación generada.

```bash
gomarkdoc --output 'docs/api/{{.Dir}}.md' ./internal/...
```

## 7. Herramienta de análisis de calidad: golangci-lint

En lugar de SonarQube se usó [**golangci-lint**](https://golangci-lint.run/), el estándar de
facto en Go: corre en una sola pasada 21 linters y 2 formateadores configurados en
[`.golangci.yml`](.golangci.yml). Cubre lo mismo que las reglas de SonarQube para Go
(bugs, code smells, complejidad, duplicación, seguridad) y además puede correr en CI o como
hook sin servidor.

| Categoría | Linters |
|---|---|
| Bugs | `govet`, `staticcheck`, `errcheck`, `ineffassign`, `unused` |
| Redundancia | `dupl`, `goconst`, `unparam`, `unconvert`, `wastedassign` |
| Complejidad | `gocyclo`, `gocognit`, `funlen`, `nestif` |
| Estilo | `revive`, `predeclared`, `asciicheck`, `godot` (+ formateadores `gofmt`, `goimports`) |
| Rendimiento | `prealloc`, `perfsprint` |
| Seguridad | `gosec` |

**42 issues → 0** ([antes](resultados/calidad/lint-antes.txt),
[después](resultados/calidad/lint-despues.txt)). Las exclusiones están justificadas en el
archivo. Por ejemplo, `misspell` quedó afuera porque solo conoce inglés y marcaba "Comando"
como error de "Commando". El único `unsafe` del proyecto quedó **auditado** con un
comentario explícito (regla G103 de gosec).

## 8. Refactorización

Cada refactor se hizo con los tests en verde antes y después, y cuando tocó código caliente
se verificó con `benchstat` que no costara rendimiento:

| Refactor | Motivo | Verificación |
|---|---|---|
| Parser con cursor de error "pegajoso" (patrón de `bufio.Scanner`) | 14 repeticiones de `if !ok { return falla() }`; complejidad ciclomática 19 | sin diferencia significativa: 458 → 475 ns, p = 0,083 |
| `Contador` (mapa clave → índice + slice de conteos) | bug de claves reescritas (sección 9) | un hash por línea en vez de dos |
| `TopN` sobre `Fuente` (iterador `iter.Seq2`) | aceptar el `Contador` particionado y los `map` comunes | mismos resultados: `TestSortHeapEquivalentes` |
| `loggen.Generar` en componentes | complejidad cognitiva 38 | salida idéntica byte a byte |
| `pipeline` con `context.Context` | cancelación con Ctrl+C y propagación de spans | tests de equivalencia seq/conc |
| Flags validados | un `-modo` mal escrito caía en silencio en el valor por defecto | `TestFlagsInvalidos` |

## 9. Testing

`go test` trae todo lo necesario sin dependencias: tests de tabla, benchmarks, fuzzing,
ejemplos y cobertura.

| Tipo | Qué se agregó |
|---|---|
| **Fuzzing diferencial** | [`FuzzManualIgualQueRegex`](internal/parse/fuzz_test.go): el motor de Go muta líneas y exige que el parser manual acepte todo lo que acepta la regex y devuelva lo mismo. 45 M de ejecuciones en 5 minutos sin divergencias después de las correcciones |
| **De punta a punta** | [`cli_test.go`](internal/cli/cli_test.go): ejecuta el comando Cobra real (generar → analizar → validar), todas las combinaciones de parser × modo × Top-N, gzip, stdin, cache y códigos de salida |
| **Contrato de vistas** | `TestAnomaliasSobreLotes` y `TestContadorNoRetieneLaClave`. Para comprobar que el test sirve se hizo una **prueba de mutación**: sacar un `strings.Clone` del detector lo hace fallar con IPs corruptas |
| **Bordes del lector** | lotes de 1 byte, líneas más largas que el buffer, CRLF, sin salto final, líneas vacías |
| **Integración con OTLP** | un receptor OTLP falso (`httptest`) verifica que los spans lleguen |
| **Ejemplos** | 6 funciones `Example`, verificadas por `go test` |
| **Barrido de semillas** | 12 logs de 1 M y 3 de 5 M con semillas distintas: 45/45 anomalías detectadas, 0 falsos positivos |

**Cobertura: 51,0 % → 92,6 %**
([antes](resultados/calidad/cobertura-antes.txt), [después](resultados/calidad/cobertura-despues.txt)).

**Bugs que encontraron los tests y las herramientas nuevas:**

1. **Desborde de enteros en el parser manual** (fuzzing): un campo de bytes de 20 dígitos
   daba un número negativo (`-8446744073709551616`). `atoiSimple` ahora controla el desborde.
2. **El parser regex y el manual no aceptaban las mismas líneas** (fuzzing, 3 casos):
   `time.Parse` acepta el mes en cualquier capitalización (`DeC`) y horas de un dígito, y `\S`
   dejaba pasar comillas dentro del request. Se hizo explícito el formato de Apache en la
   especificación de referencia. Los 4 casos quedan en `testdata/fuzz/` como regresión.
3. **El runtime de Go reescribe la clave de un `map[string]int` en cada asignación**
   (test de equivalencia secuencial/concurrente). `m[k]++` sobre una clave existente
   reemplaza la clave guardada por `k`. El "clonar al insertar" del primer parcial
   funcionaba solo a medias: las claves frecuentes volvían a apuntar a la última línea donde
   aparecían. Con strings inmutables solo retenía memoria; con el buffer reutilizado corrompía
   datos. Se confirmó aislado en un programa de 15 líneas y motivó el `Contador`.
4. **El detector de anomalías retenía strings de las entradas sin copiarlos** (IPs como claves
   de mapa, rutas en la ventana de 404). En el primer parcial eso retenía la línea entera por
   cada IP; con la lectura por lotes habría sido incorrecto.
5. **El ground truth del top de IPs estaba incompleto en logs chicos** (test de la CLI): solo
   contaba el pool de IPs activas, y en logs de pocas decenas de miles de líneas la IP de la
   ráfaga (500 solicitudes) entra al top-10.
6. **El graficador nunca salteaba filas incompletas del CSV** (test de `grafico`): el chequeo
   existía, pero `csv.Reader` aborta la lectura antes de que se ejecute.
7. **Flags mal escritos se ignoraban en silencio** (refactor de la CLI):
   `-modo paralelo` corría el modo por defecto sin avisar.
8. **El detector de picos de 5xx no veía un pico que cruza una hora en punto** (validación a
   20 M líneas, que el primer parcial no había hecho con anomalías). El pico inyectado iba de
   03:30 a 04:30 y quedaba partido en dos franjas de ~320 errores. Cada una estaba por debajo
   del umbral global (117 ± 81 → 360), que dominan las horas pico del día, aunque era 18×
   lo típico de la madrugada (17 ± 25). La versión del primer parcial también daba recall
   2/3 sobre ese log. El detector nuevo:
   - compara cada franja contra las de **la misma hora del día** (estacionalidad);
   - une las franjas anómalas consecutivas en **un solo episodio**;
   - exige un **tamaño de efecto** (2× lo típico) además del umbral de 3σ, y un piso de
     Poisson (σ ≥ √μ). Sin esto, la primera versión dio un falso positivo en una de cada
     cuatro semillas: con cientos de franjas evaluadas a 3σ, alguna lo supera por azar
     (comparaciones múltiples).

   Resultado: 3/3 y 0 falsos positivos en 1 M, 5 M, 20 M y 15 semillas más. Con 1 M el
   detector viejo también acertaba en las 12 semillas probadas
   ([`anomalias-semillas-detector-p1.txt`](resultados/segundo-parcial/anomalias-semillas-detector-p1.txt)),
   porque a ese tamaño el pico siempre cae cerca del mediodía y dentro de una sola hora.
   La mejora aparece cuando el pico cae de noche o en el borde de una hora.
   `TestPicoNocturnoQueCruzaLaHora` reproduce el caso; falla con el detector viejo.

## 10. Uso de un framework: Cobra

[**Cobra**](https://cobra.dev/) es el framework de CLI más usado en Go (lo usan `kubectl`,
`docker`, `hugo` y `gh`). Reemplazó a los tres programas del primer parcial por un único
binario con subcomandos ([`internal/cli`](internal/cli/)):

```
analizador generar   genera un log sintético y su ground truth
analizador analizar  analiza un log (archivo, .gz o stdin)
analizador grafico   dibuja la curva de escalabilidad desde un CSV
analizador completion bash|zsh|fish|powershell
```

Lo que aporta: flags POSIX (`--archivo`, `-a`), ayuda y ejemplos generados (`--help`),
validación de argumentos (`cobra.NoArgs`), un flag persistente que heredan todos los
subcomandos (`--otlp`), ganchos `PreRun`/`PostRun` (donde se inicia y se apaga la telemetría)
y autocompletado para la shell. `NuevoComando()` arma el árbol sin estado global, así que
los tests lo ejecutan entero con argumentos, entrada y salida propias.

**Trade-off:** el primer parcial usaba solo la biblioteca estándar. Cobra y OpenTelemetry son
las únicas dependencias, y ninguna está en el camino caliente: el parseo, el conteo y el
Top-N siguen siendo solo biblioteca estándar.

---

## Cómo reproducir

```bash
go install golang.org/x/perf/cmd/benchstat@latest
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
go install golang.org/x/tools/cmd/deadcode@latest

go test ./... -cover                                  # tests y cobertura
go test ./internal/parse -run '^$' -fuzz Fuzz -fuzztime 60s
go test ./internal/pipeline -run '^$' -bench 'Lectura|TamLote' -benchmem
go test ./internal/loggen   -run '^$' -bench 'Escritura|Formato' -benchmem
golangci-lint run ./...
deadcode ./...

go build -o analizador.exe ./cmd/analizador            # con PGO (default.pgo)
go build -pgo=off -o analizador-sin-pgo.exe ./cmd/analizador
./analizador generar -n 5000000 -o datos/5m.log --verificacion datos/5m-gt.json
./analizador analizar -a datos/5m.log --verificacion datos/5m-gt.json --anomalias

docker compose up -d && ./analizador --otlp localhost:4318 analizar -a datos/5m.log
```
