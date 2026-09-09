# Fase 3 — Análisis de complejidad

Análisis temporal y espacial de las operaciones del analizador de logs. Para cada
una: cómo se deriva la cota y por qué importa para *este* problema (no basta la
notación — requisito 2 del enunciado).

Método de análisis (deck, slides 28 y 30): contar los bucles y ver de qué
dependen, identificar la recursión y cómo se achica el problema, descartar
constantes, conservar el término dominante.

---

## 0. Parámetros del problema

| Símbolo | Qué es | Valor típico |
|---|---|---|
| `n` | cantidad de líneas del log | 10⁶ – 2·10⁷ |
| `L` | largo de una línea en bytes | ~190 (acotado, ~260 máx.) |
| `uₑ` | endpoints distintos | ~40 (acotado por el catálogo) |
| `uₛ` | status distintos | ~15 (acotado) |
| `u_ip` | IPs distintas | **crece con `n`**: 0,3·n – 0,6·n |
| `u_usr` | usuarios distintos | ~30 (acotado) |
| `k` | tamaño del Top-N pedido | 10 |
| `P` | núcleos disponibles | 4 – 16 |

La distinción clave: `uₑ`, `uₛ`, `u_usr` están **acotados por el dominio** (hay un
catálogo finito de endpoints, de códigos HTTP, de usuarios). `u_ip` **no**: cada
visitante nuevo es una IP nueva, así que `u_ip = Θ(n)`. Esto decide qué
estructuras y qué algoritmos sirven.

Escala (deck, slides 26–27): con `n = 10⁶`, un algoritmo O(n) hace ~10⁶
operaciones (milisegundos); uno O(n²) hace ~10¹² (≈ días). Con `n = 2·10⁷`, O(n²)
es ~4·10¹⁴. **Toda operación por línea tiene que ser O(1), y toda agregación
O(u) o O(u log k).** Ese es el corsé de diseño.

---

## 1. Parseo de una línea

Convierte `190.12.44.3 - - [15/Mar/2019:14:22:07 -0300] "GET /x HTTP/1.1" 200 …`
en un struct con 11 campos. Se hace `n` veces.

### 1a. Baseline: expresión regular con submatches

`regexp.FindStringSubmatch` sobre un patrón con 11 grupos de captura.

**Derivación.** El paquete `regexp` de Go implementa **RE2**: garantiza tiempo
lineal en el largo de la entrada, sin el backtracking exponencial de PCRE. Tiene
varios motores y elige según el caso; para líneas cortas con submatches como las
nuestras usa el **bitstate backtracker**, que hace backtracking *acotado*: lleva
un bitmap de (posición × estado) ya visitados y nunca revisita, con un
presupuesto fijo (~256 KB); si lo excede, cae al motor NFA. En ambos casos:

```
costo por línea = O(L · s) = O(L)          (s = tamaño del autómata, constante)
costo total     = O(n · L)
```

La documentación de `regexp` lo garantiza: *"linear in the size of the input"*.
Medido (Fase 5, perfil de v1): `regexp.tryBacktrack` + `bitState.shouldVisit` +
`MatchRune` suman ~70 % del CPU. Lineal, pero con una constante enorme.

**Por qué RE2 y no un motor con backtracking.** Los motores de PCRE / `re` de
Python / `java.util.regex` prueban alternativas y retroceden; sobre patrones
patológicos su costo es O(2^L) (*catastrophic backtracking*). Los ejemplos del
deck están en Python con `re`, que tiene ese riesgo. RE2 lo cambia por la
garantía lineal a cambio de no soportar backreferences.

**El costo que la notación O esconde.** Asintóticamente O(n·L), pero:
- `FindStringSubmatch` asigna un `[]string` de 12 entradas por línea.
- `time.Parse` resuelve el offset `-0300` genéricamente en cada llamada.
- El propio `bufio.Scanner.Text()` copia la línea a un string nuevo.

Medido sobre 10⁶ líneas: **584 MB asignados** para 98 MB de datos retenidos, y
30 ciclos de GC. El factor constante y la presión de GC —invisibles en la
notación— son el verdadero problema, y sólo se ven midiendo (deck, slide 277:
"no optimizar antes de medir").

### 1b. Optimización (Fase 5): escaneo manual

Un recorrido hacia adelante con `strings.IndexByte`, cortando por las posiciones
de los delimitadores (` `, `[`, `]`, `"`).

**Derivación.** Cada byte de la línea se mira O(1) veces (unos pocos `IndexByte`
que en conjunto recorren la línea una vez). Los campos son sub-slices del string
original: **cero copias, cero asignaciones**. La fecha se arma por posiciones
fijas + `time.Date` (sin `time.Parse`), con las zonas horarias precalculadas.

```
costo por línea = O(L)
costo total     = O(n · L)      ← misma clase que la regex
asignaciones    = 0 por línea (medido: 0 B/op, 0 allocs/op)
```

Medido (Fase 5): **175 ns/op vs 2,9 µs/op** de la regex — 16× — y el pipeline
completo de 10⁶ líneas pasa de 3,2 s a 0,70 s. Con el regex fuera del perfil,
el CPU se reparte entre la inserción en los mapas, el GC del `Text()` y las
syscalls de lectura.

**Conclusión importante.** Regex y parseo manual son **la misma complejidad
asintótica**: O(n·L). La mejora no es de orden, es de **factor constante y de
memoria**. Es un caso donde "elegir mejor algoritmo" significa bajar la constante
y las asignaciones, no cambiar el exponente — y por eso hay que justificarlo con
mediciones, no con la notación (requisito 3).

---

## 2. Conteo de frecuencias

`contador[clave]++` para status, endpoint, IP y usuario. Se hace `n` veces.

### 2a. Lo que hacemos: tabla hash (`map` de Go)

**Derivación del O(1) amortizado.**
- Calcular el hash de la clave es O(largo de la clave). Las claves están acotadas
  (IP ≤ 15 chars, endpoint ≤ ~40), así que es O(1).
- El `map` de Go es una tabla hash con buckets de 8 entradas y *rehashing*
  incremental. Una inserción normal es O(1).
- Cuando el factor de carga supera ~6,5, la tabla duplica su tamaño y hay que
  recolocar las entradas: reubicar `j` entradas cuesta O(j), pero eso ocurre
  recién tras `j` inserciones. El costo se reparte: **O(1) amortizado**.

```
costo por línea (4 mapas) = O(1)
costo total               = O(n)
```

### 2b. Alternativas peores (por qué el map)

| Estructura | Búsqueda/insert | Total para `n` líneas | Comentario |
|---|---|---|---|
| **slice + scan lineal** (deck slide 279) | O(u) | **O(n·u) → O(n²)** para IPs | con `u_ip ≈ n`: ~10¹² ops. Inabordable. |
| slice ordenado + búsqueda binaria | O(log u) buscar, O(u) insertar (shift) | O(n·u) | la inserción manda |
| árbol balanceado (`TreeMap`) | O(log u) | O(n log u) | correcto, pero peor constante y localidad de caché que el hash |
| **tabla hash (`map`)** | O(1) amort. | **O(n)** | lo que usamos |

El salto de O(n²) a O(n) por elegir la estructura correcta es el ejemplo central
del deck (slide 35) y el "desafío" de la Opción 3.

### 2c. Complejidad espacial — el límite real

```
espacio = O(uₑ + uₛ + u_usr + u_ip) = O(u_ip) = O(n)
```

Los tres primeros están acotados; el mapa de IPs domina y **crece linealmente con
`n`**.

**Trampa medida (Fase 6): aliasing de claves.** Insertar en un map una clave que
es sub-slice de la línea no copia los bytes: el mapa guarda el header del string
y **retiene viva la línea entera** (~190 B por clave única en vez de ~15). Con
10⁶ líneas: 98,6 MB retenidos vs 19,9 MB clonando la clave al insertar
(`agregado.incrementar`). La complejidad espacial sigue siendo O(u_ip), pero la
constante cae 5×.

Opciones si aun así no entra en memoria:
- **Count-Min Sketch**: conteo aproximado en espacio *fijo* O(1), con error
  acotado. Sirve si sólo se necesita el Top-N y una cota del conteo.
- Particionar por hash de la clave y procesar/mergear por partes (external).

---

## 3. Top-N (los k más frecuentes)

Dado un mapa de `u` pares `(clave, conteo)`, devolver los `k` de mayor conteo
ordenados. Se hace una vez por métrica, al final.

### 3a. Baseline: ordenar todo

Volcar el mapa a un slice y `slices.SortFunc`.

**Derivación.**
- Volcar: recorrer el mapa, O(u).
- Ordenar: `slices.SortFunc` usa **pdqsort** (*pattern-defeating quicksort*):
  quicksort con pivote mediana-de-tres, que detecta patrones (ya ordenado,
  invertido, con muchos duplicados) y cae a heapsort si el quicksort se degrada.
  Recurrencia en el caso típico `T(u) = 2·T(u/2) + O(u)` ⇒ **O(u log u)**; el
  fallback a heapsort garantiza O(u log u) también en el peor caso.
- Tomar los primeros `k`: O(k).

```
total  = O(u log u)
espacio = O(u)   (el slice volcado)
```

### 3b. Optimización (Fase 4): min-heap de tamaño k

Recorrer las `u` claves manteniendo un `container/heap` con a lo sumo `k`
elementos (la raíz es el menor de los k):

```
para cada una de las u claves:
    si el heap tiene < k elementos:      push        O(log k)
    si no, y conteo > heap.raíz:         pop + push  O(log k)
    si no:                               descartar   O(1)
```

**Derivación.** `u` iteraciones, cada una con a lo sumo una operación de heap
O(log k) (subir/bajar por un árbol binario de altura log k). Al final se extraen
los k en orden: O(k log k).

```
total  = O(u log k)
espacio = O(k)
```

### 3c. Comparación y punto de equilibrio

| | Ordenar todo | Min-heap de k |
|---|---|---|
| Tiempo | O(u log u) | O(u log k) |
| Espacio | O(u) | O(k) |

La ventaja del heap es el factor `log u / log k`:

| Métrica | `u` | `log₂u / log₂k` (k=10) | ¿conviene heap? |
|---|---|---|---|
| endpoints | ~40 | 5,3 / 3,3 ≈ 1,6 | apenas |
| status | ~15 | 3,9 / 3,3 ≈ 1,2 | no |
| usuarios | ~30 | 4,9 / 3,3 ≈ 1,5 | apenas |
| **IPs** | ~1,2·10⁶ | 20,2 / 3,3 ≈ **6,1** | **sí**, y además ahorra ~1,2·10⁶ → 10 en espacio |

**Punto de equilibrio.** Cuando `k → u`, `log k → log u` y las dos cotas
convergen; ahí gana ordenar, porque pdqsort recorre memoria contigua (amigable a
la caché) mientras que el heap salta por un árbol. Y si se necesitan *todas* las
`u` claves ordenadas (no sólo el Top-N), hay que ordenar sí o sí. La decisión
—heap para IPs, sort para el resto— es la decisión de algoritmo del requisito 3.

Lo vamos a medir en `bench/topn_test.go` variando `u` y `k`.

---

## 4. Lectura del archivo y el piso de rendimiento

**Derivación.** Hay que leer los `n·L` bytes del archivo. Con `bufio` (buffer de
tamaño `B`), la cantidad de llamadas al sistema es O(n·L / B); el resto es copiar
bytes del buffer. Costo O(n·L) = O(tamaño del archivo), inevitable.

**Por qué importa.** Una vez que el parseo deja de ser el cuello de botella
(Fase 5), el límite pasa a ser leer el archivo: el programa se vuelve
**I/O-bound**. Por eso la concurrencia (Fase 7) paraleliza el *parseo y el
conteo* (trabajo de CPU), no la lectura: repartir el archivo en `P` bloques y
que cada núcleo parsee el suyo lleva el término de CPU de O(n·L) a O(n·L / P),
pero el término de I/O sigue siendo O(n·L). El perfilado tiene que medir con
caché de disco fría y caliente por separado para ver cuál domina.

---

## 5. Vistazo a fases posteriores

**Merge del map-reduce (Fase 7).** `P` mapas parciales se fusionan sumando
conteos. Peor caso (toda clave en todo mapa parcial): O(u·P). Mejor caso (claves
disjuntas): O(u). Total concurrente: `O(n·L/P + u·P)`.

**Detección de anomalías (Fase 8).**
- Ventana deslizante de timestamps por IP (deque): cada evento entra y sale una
  vez ⇒ O(1) amortizado por evento ⇒ **O(n)** total; espacio O(máximo backlog).
- Escáner (set de endpoints por IP): inserción O(1) por evento ⇒ O(n); la
  cardinalidad del set se mantiene incremental.
- Pico de 5xx por franja horaria: O(1) por evento.

Todo O(n): las anomalías no cambian la clase de complejidad del pipeline.

---

## 6. Tabla resumen

| Operación | Baseline | Optimizada | Espacio | Se ejecuta |
|---|---|---|---|---|
| Parseo de línea | O(n·L) regex + churn de GC | O(n·L) manual, sin asignar | O(1) por línea | `n` veces |
| Conteo | O(n) hash | = | O(u_ip) = O(n) | `n` veces |
| — con slice + scan (evitado) | O(n²) | — | O(u) | — |
| Top-N | O(u log u) sort | O(u log k) heap (para IPs) | O(u) → O(k) | `#métricas` veces |
| Lectura I/O | O(n·L) | = (piso) | O(B) | 1 vez |
| Parseo+conteo concurrente | — | O(n·L / P) CPU | O(u·P) | 1 vez |
| Anomalías (ventana/set) | — | O(n) | O(backlog) | `n` veces |

## 7. Conclusión

- El pipeline completo es **O(n·L)** en tiempo, dominado por leer y parsear.
- La **complejidad espacial O(n)** (por las IPs) es el límite que primero se
  siente al crecer `n`.
- Las dos decisiones de algoritmo con impacto real:
  1. **hash map en vez de slice** para el conteo: O(n) en vez de O(n²). Cambio de
     orden — se justifica con la notación.
  2. **min-heap en vez de sort** para el Top-N de IPs: O(u log k) en vez de
     O(u log u), y O(k) en vez de O(u) de espacio. Mejora de factor (~6×) — se
     justifica midiendo.
- El **parseo manual** no cambia el orden (sigue O(n·L)); mejora la constante y
  elimina la presión de GC. También se justifica midiendo, no con la notación.
