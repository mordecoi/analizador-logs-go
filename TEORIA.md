# Fase 0 — Repaso teórico (deck de la cátedra) aplicado a la Opción 3

Resumen de los conceptos del deck que vamos a usar, con el número de slide para
contrastar, y la traducción del ejemplo Python del deck a lo que hacemos en Go.

---

## 1. Complejidad algorítmica (slides 5–33)

### Cómo se calcula (slide 28, 30)
1. Contar los bucles: un `for` sobre la entrada → O(n); dos anidados → O(n²).
2. Mirar la recursión: cuántas llamadas y cómo se achica el problema.
3. Descartar constantes: O(2n) → O(n).
4. Conservar el término dominante: O(n² + n + 10) → O(n²).

### Modelo de coste (slides 32–33)
No se mide en segundos de una máquina concreta. Se cuentan operaciones
elementales **y accesos a disco / red / I/O cuando son relevantes**. Para un
analizador de logs el I/O *es* relevante: parte del análisis va a ser leer el
archivo, no solo procesarlo.

### Las operaciones que importan en nuestro problema
| Operación | Complejidad | Por qué |
|---|---|---|
| Leer + parsear las n líneas | O(n·L), L = largo de línea | se toca cada byte una vez |
| Contar (map por status/endpoint/IP/hora) | O(n) total, O(1) amortizado por línea | slide 13, 44 |
| Top-N ordenando todo | O(u·log u), u = claves únicas | slide 22, 29 (merge/heap sort) |
| Top-N con min-heap de tamaño k | O(u·log k) | slide 107 |
| Anti-patrón a evitar: "duplicados" con `contains` | O(n²) | slide 18, 35, 279 |

Comparación de escala del deck (slide 26–27): con n = 1.000.000, O(n) ≈ 10⁶
operaciones, O(n²) ≈ 10¹² (inabordable). Por eso todo el pipeline tiene que
quedar en O(n) o O(n log k).

---

## 2. Estructuras de datos (slides 34, 43–48, 95–113)

> "No existe una estructura mejor en absoluto. Existe una más adecuada para las
> operaciones que necesita el algoritmo." (slide 42)

| Deck (Python) | Nuestro uso en Go | Costo | Slide |
|---|---|---|---|
| `dict` / HashMap | `map[string]int` para los contadores | O(1) prom. búsqueda e inserción | 103, 44 |
| `set` | `map[string]struct{}` — endpoints únicos por IP (scanner) | O(1) prom., sin duplicados | 101–102 |
| `heapq` (cola de prioridad) | `container/heap` — min-heap de tamaño k para Top-N | push/pop O(log n) | 107–108 |
| `collections.deque` | ring buffer / slice como cola — ventana temporal por IP (ráfagas) | alta/baja por un extremo O(1) | 105–106 |
| `list` | slice para volcar un map y ordenarlo, y buffers de lectura | acceso O(1), inserción al medio O(n) | 97–98 |

Criterios de elección del deck (slide 113): acceso, modificación, tamaño, orden.
Para los contadores: acceso por clave, muchísimas modificaciones, ninguna
necesidad de orden hasta el reporte final → el `map` gana. Para el Top-N el
orden importa pero solo entre k elementos → heap, no ordenar los u.

---

## 3. Top-N: ordenar todo vs heap (slides 22, 29, 107–108)

- **Ordenar todo el map volcado**: O(u·log u). Simple, y para nuestro caso u
  (claves únicas: ~5 endpoints, ~7 status, ~decenas de miles de IPs) no es
  gigante. Es la opción del **baseline**.
- **Min-heap de tamaño k**: recorrés las u claves una vez; mantenés solo las k
  mejores; si el conteo nuevo supera la raíz (el menor de las k), reemplazás.
  O(u·log k). Con k = 10 y u = 50.000: `log k ≈ 3` vs `log u ≈ 16`.
- Punto de equilibrio: cuando k se acerca a u, ordenar todo es igual o mejor
  (menos constante). Lo vamos a **medir** en `bench/topn_test.go`.

Este es exactamente el tipo de decisión que pide el requisito 3 del enunciado
("justificar al menos una decisión de selección de algoritmo").

---

## 4. Procesamiento por lotes y lazy (slides 37–38, 89–92, 180–190)

- **Leer por bloques, no todo a memoria** (slide 37, 89, 176): el baseline lee
  con `bufio.Scanner` línea a línea → memoria O(1) en la lectura, no O(n).
  Equivalente Go del generador `yield` del deck (slide 89–92): un `chan` o un
  iterador que entrega líneas bajo demanda.
- **Batching para habilitar paralelismo** (slide 69, 181, 187): "los lotes se
  pueden procesar en paralelo". Nuestra versión concurrente parte el archivo en
  C bloques y cada worker procesa el suyo. El deck lo dice explícito para logs:
  "procesar logs de a 1000 líneas en vez de cargar archivos enteros" (slide 73,
  189).
- **Preprocesamiento / indexado** (slide 39): invertir tiempo en preparar datos
  para ahorrar trabajo repetido después. En nuestro caso: construir los
  agregados una vez y responder muchas consultas sobre ellos.

---

## 5. CPU-bound vs I/O-bound y concurrencia (slides 33, 114–122)

El deck razona desde Python y el **GIL** (slide 11, 120):
- En CPython, threading sirve para tareas de **E/S** (esperan afuera); para
  tareas de **CPU** hace falta `multiprocessing` (procesos separados).
- Sincronización: locks, semáforos, colas para acceso concurrente (slide 120, 122).

Traducción a Go (equivalencias del `CLAUDE.md`):
- **No hay GIL.** Las goroutines corren en paralelo real sobre múltiples núcleos
  con un scheduler M:N. La misma herramienta (goroutine) sirve para I/O-bound y
  CPU-bound.
- `multiprocessing` de Python → goroutines; `Queue` → channel; `Lock` →
  `sync.Mutex`; `Thread.join()` → `sync.WaitGroup`.
- Nuestro parseo+conteo es **CPU-bound** una vez que el archivo está en page
  cache; la primera lectura desde disco es **I/O-bound**. El perfilado tiene que
  distinguir los dos casos (correr dos veces: fría y caliente).
- Problema clásico a mostrar (slide 120, "condiciones de carrera"): si los
  workers comparten un `map` con `sync.Mutex`, la contención lo hace más lento
  que secuencial. Solución: un `map` por worker + fase de *merge*. Lo medimos a
  propósito con `-race` para mostrar que la versión sin mutex tampoco tiene
  carreras.

---

## 6. Memoización vs caching (slides 36, 56–63, 78–83)

El enunciado (requisito 5) exige diferenciarlos. El deck los separa así:

| | Memoización (slides 56–63) | Caching inteligente (slides 78–83) |
|---|---|---|
| Qué guarda | resultado de un **cálculo** según sus argumentos | información / resultados de **acceso frecuente** |
| Clave | los argumentos de la función | definida por la lógica de negocio |
| Invalidación | normalmente no hay (función pura) | **sí**: LRU, TTL, o "los datos cambiaron" |
| Los 3 elementos (slide 60) | función + clave + resultado almacenado | + política de expiración |
| Riesgo (slide 63) | consumo de memoria; bugs si hay estado compartido en concurrencia | servir información obsoleta |

Nuestra aplicación:
- **Memoización**: los agregados completos (los maps de conteo) serializados a
  disco. Clave = `hash(ruta + tamaño + mtime)` del log. Segunda corrida sobre el
  mismo archivo → se lee el resultado en vez de re-parsear. Función "pura":
  mismo archivo → mismos agregados.
- **Caching**: la respuesta Top-N ya calculada por `(métrica, N)`. Se invalida
  cuando entra un lote nuevo de líneas (marca de "sucio"). Acá sí hay política
  de invalidación, que es lo que lo distingue de la memoización.

---

## 7. Profiling (slides 235–245, 264–265)

El deck usa `cProfile` + `snakeviz` (visual), `memory_profiler`, `timeit`.
Equivalencias en Go (ya en `PLAN.md`, sección 3): `testing.B`, `pprof`,
`-benchmem`, `go tool trace`. Ventaja: viene todo en el toolchain.

Preguntas que el perfilado tiene que responder (enunciado, punto 7):
- ¿Dónde está el cuello de botella? (esperado en el baseline: la regex)
- ¿Qué optimización se hizo y qué impacto tuvo?
- ¿Se sostiene al crecer n? (correr 1M / 5M / 20M)
- ¿Qué trade-off se pagó? (ej.: parseo manual = menos legible; concurrencia =
  más memoria por los maps parciales)

Flujo (slide 236, 265): perfilar → identificar → optimizar solo eso → re-medir
→ testear que no se rompió nada.

---

## 8. Optimización prematura (slides 266–282)

La regla de Knuth (slide 267) y el enfoque recomendado (slide 269, 281):
1. **Primero que funcione**: código claro y correcto = nuestro **baseline v0**
   (regex + sort + secuencial). No arrancamos optimizado.
2. **Medir** con profiling para encontrar el cuello de botella real.
3. **Optimizar dirigido**: solo lo que el perfil marca, una cosa por vez, y
   re-medir el impacto.
4. **Testear** que la optimización no rompió el resultado.

Esto es literalmente el plan de fases (2 → 9). El baseline no es descartable:
es la línea base contra la que se miden todas las mejoras (requisito 1 del
enunciado) y el material del slide 10 de la presentación ("qué decisión no
produjo la mejora esperada").

Nota del slide 274: el propio deck usa como ejemplo de optimización prematura
"paralelizar `encontrar_maximo` con threads". Moraleja para nosotros: la
concurrencia **solo** se justifica si el perfil muestra que el parseo+conteo
domina y hay archivo suficientemente grande. En archivos chicos va a perder por
overhead, y eso hay que mostrarlo, no esconderlo.

---

## Mapa: concepto del deck → dónde se cumple en el parcial

| Requisito del enunciado | Concepto del deck | Fase | Archivo |
|---|---|---|---|
| 1. Implementación inicial (baseline) | slide 269 "primero que funcione" | 2 | `cmd/analizar` v0 |
| 2. Análisis de complejidad (≥2) | slides 28–33 | 3 | este doc + informe |
| 3. Elección de algoritmo (≥1) | slides 35, 45–48, 107 | 4, 5 | Top-N heap; parseo manual |
| 4. Estructuras de datos (≥2) | slides 95–113 | 2, 4, 8 | map, heap, set, deque |
| 5. Memoización o caching | slides 56–63, 78–83 | 6 | `internal/cache` |
| 6. Concurrencia / paralelismo | slides 114–122, 181 | 7 | map-reduce en `cmd/analizar` |
| 7. Perfilado y medición | slides 235–245 | 9 | `bench/`, `resultados/` |
| Comparación obligatoria | slide 26–27 (escala) | 10 | `resultados/comparativa.csv` |
