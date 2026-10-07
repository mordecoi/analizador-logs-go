# Fase 9 — Perfilado consolidado

> **Nota (segundo parcial):** este documento es el registro del primer parcial y se conserva tal cual. Los programas `cmd/generar`, `cmd/analizar` y `cmd/grafico` se unificaron en un solo binario con Cobra: `go run ./cmd/analizador generar|analizar|grafico`, con los mismos flags en formato `--flag` (por ejemplo `-archivo x` pasa a ser `--archivo x` o `-a x`; la etiqueta del CSV, `-version`, pasó a `--version-etiqueta`). Ver [`SEGUNDO-PARCIAL.md`](SEGUNDO-PARCIAL.md).

Responde las cinco preguntas del punto 7 del enunciado, con los perfiles de las
tres versiones capturados en **una misma tanda** (5 M de líneas, 919 MB, binario
compilado, máquina quieta). Extractos crudos en
[`resultados/fase9-perfiles.txt`](resultados/fase9-perfiles.txt); los `.pprof` y
la traza en `resultados/perfiles/`.

## Herramientas usadas (todas del toolchain de Go)

| Qué | Herramienta | Dónde se usó |
|---|---|---|
| Micro-benchmarks | `testing.B` (`go test -bench -benchmem`) | parseo (fase 5), Top-N (fase 4) |
| Perfil de CPU | `runtime/pprof` + `go tool pprof` (flags `-cpuprofile` del analizador) | fases 5 y 9 |
| Perfil de heap | `-memprofile` + `pprof` (`inuse_space` / `alloc_space`) | fases 6 y 9 |
| Costo línea por línea | `go tool pprof -list` | `procesarBloque` |
| Línea de tiempo de goroutines | `go tool trace` (`-trace`) | fase 7 (`trace-conc8.out`) |
| Escape analysis | `go build -gcflags=-m` | verificación del parser sin asignaciones |
| Medición macro | `runtime.MemStats` (delta de TotalAlloc/Mallocs/NumGC) + tiempo de pared | todas las corridas |

---

## Pregunta 1 — ¿Dónde está el principal cuello de botella?

**Cambió con cada versión; por eso se perfila después de cada optimización.**

**v1 (regex):** el motor de expresiones regulares es ~70 % del CPU:
`regexp.tryBacktrack` (57 % acumulado), `bitState.shouldVisit` (15 %),
`MatchRunePos` (10 %); `time.parse` suma otro 7 %. CPU-bound en el parseo.

**v2 (parser manual):** el parseo cae a ~21 % acumulado y el cuello se reparte:

- **inserción en los mapas** (~30 %: `Agregar` 31 % cum — `aeshashbody`,
  `ctrlGroup.matchH2`, `mapassign`): es el trabajo esencial del problema;
- **allocator/GC** (~25 %: `spanClass.sizeclass`, `tryDeferToSpanScan`),
  alimentado por la única asignación por línea que queda: el string de
  `ReadString` (935 MB, el 78 % de todo lo asignado — ver pregunta 5);
- **I/O** (syscalls de lectura) y el resto del pipeline.

**v3 (concurrente):** ya no hay un dominador: hash de claves 11 %, GC 13 %,
parseo 15 %, y aparece `semasleep` (workers esperando) — señal de que se saturó
el **ancho de banda de memoria**, no el CPU. Por eso el speedup se estanca en
~2,5–2,9× con 8 de 12 hilos (fase 7).

## Pregunta 2 — ¿Qué optimización se realizó?

En orden, cada una dirigida por el perfil anterior (nunca a ciegas):

1. **v1** Top-N con min-heap (estructura de datos) — dirigida por el análisis de
   complejidad, no por el perfil… y el perfil después mostró que no estaba en el
   camino crítico (ver pregunta 3).
2. **v2** Parser manual: sub-slices + fecha por posiciones fijas + zonas
   horarias precalculadas → eliminó regex y `time.Parse` del perfil.
3. **v2.5** Clon de claves al insertar → eliminó la retención invisible de
   líneas enteras por aliasing (hallazgo de la fase 6).
4. **v3** Map-reduce por bloques sin estado compartido → paraleliza mapas+parseo.
5. **v4** Memoización de agregados a disco → elimina el re-procesamiento entero.

## Pregunta 3 — ¿Qué impacto tuvo?

Sobre 5 M de líneas, misma tanda:

| Versión | Tiempo | vs anterior | Asignado | Allocs | GC |
|---|---|---|---|---|---|
| v1 regex + heap | 41,0 s | — | 3036 MB | 17,0 M | 108 |
| v2 manual | 7,2 s | **5,7×** | 1201 MB | 7,0 M | 44 |
| v3 conc w=8 | 2,9 s | **2,5×** | 1421 MB | 7,0 M | 31 |
| **total v1→v3** | | **14,3×** | | | |

- Heap retenido (todas las versiones, gracias a v2.5): **~138 MB** en vez de
  ~530 MB — 3,9× menos a esta escala.
- v4 (cache, 1 M): re-consulta en 0,13 s vs ~1,3 s (~10×).
- El heap del Top-N: 14× y 28 000× menos memoria **en aislado**, pero ~3 % en el
  total — la lección de optimizar fuera del camino crítico (slide 10 del oral).

## Pregunta 4 — ¿La mejora se mantiene al aumentar el volumen?

Sí en tiempo, con un asterisco en memoria:

- Los ratios se repiten en 1 M y 5 M (manual ~5–6×, concurrencia ~2,5–2,9×);
  el pipeline es O(n·L) y las mejoras son de factor constante, así que escalan
  con n. La fase 10 lo confirma con 20 M.
- La **memoria retenida crece O(n)** por el mapa de IPs (19,9 MB a 1 M →
  138 MB a 5 M, ~4× por 5× de datos): el límite al crecer no es el tiempo sino
  el espacio, como predijo `COMPLEJIDAD.md` §2c. Mitigaciones posibles:
  Count-Min Sketch o particionado externo.

## Pregunta 5 — ¿Qué trade-off se produjo?

| Optimización | Qué se paga |
|---|---|
| Parser manual | Acoplado al formato exacto (la regex era más tolerante y declarativa); más código y más casos borde propios. Misma clase O(n·L): la ganancia es constante+GC y solo se justifica midiendo. |
| Clon de claves | Una búsqueda extra de mapa por línea (medido: dentro del ruido) a cambio de 5× menos heap. |
| Map-reduce | +220 MB asignados (mapas parciales de 8 workers) y no sirve para los detectores de anomalías, que necesitan orden temporal → pasada secuencial aparte. |
| Min-heap Top-N | Pierde contra sort cuando k ≈ u (constantes y localidad de caché). |
| Memoización | 6 MB de disco por log + lógica de invalidación (tamaño+mtime+versión); solo paga si se re-consulta el mismo archivo. |
| Concurrencia mal hecha (mutex) | Demostrado: 8 núcleos y **más lento que secuencial**. |

---

## El residuo: qué queda por optimizar (y por qué no lo hicimos)

`alloc_space` de v2 lo dice: **935 MB (78 %) es el string por línea de
`bufio.ReadString`**. Se eliminaría parseando sobre `scanner.Bytes()` (buffer
reutilizado) — posible gracias a que v2.5 clona las claves al insertar, así que
nada necesita sobrevivir a la línea. No lo hicimos: exige manejar la vida útil
del buffer con cuidado (o `unsafe`), y el perfil muestra que el siguiente límite
(mapas + ancho de banda) acotaría la ganancia. Es la frontera entre optimizar y
la optimización prematura del deck (slides 266–282): queda documentado como
trabajo futuro con su hipótesis medible.

## Trampas de medición encontradas (checklist para el oral)

1. **La máquina cambia entre sesiones**: una comparación contra números viejos
   mostró un falso "2× más lento". Regla: A/B intercalado en la misma tanda.
2. **Liveness y perfiles de heap**: el perfil `inuse` salía vacío porque el
   compilador da por muertos los contadores tras su último uso y el GC previo al
   perfil los recolecta → `runtime.KeepAlive` después de escribir el perfil.
3. **inuse vs alloc**: `alloc_space` encuentra la presión de GC (935 MB de
   strings de línea); `inuse_space` encuentra la retención (97 MB de buckets +
   21 MB de claves clonadas). Son preguntas distintas; hay que mirar los dos.
4. **`go run` vs binario**: el linkeo mete ruido; medir siempre el binario.

## Cómo reproducir

```bash
go build -o analizar.exe ./cmd/analizar
./analizar.exe -archivo logfiles.log -parser regex  -modo seq -cpuprofile cpu-v1.pprof
./analizar.exe -archivo logfiles.log -parser manual -modo seq -cpuprofile cpu-v2.pprof -memprofile mem-v2.pprof
./analizar.exe -archivo logfiles.log -modo conc -workers 8 -trace trace.out
go tool pprof -http=: cpu-v2.pprof          # top / list / flame graph
go tool pprof -sample_index=alloc_space mem-v2.pprof
go tool trace trace.out                     # línea de tiempo de goroutines
```
