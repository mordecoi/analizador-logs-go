# Rendimiento

Las cifras de esta página, salvo las de los [controles del CI](#controles-en-el-ci), se midieron durante el segundo parcial en un AMD Ryzen 7 7730U (8 núcleos, 16 hilos) con Windows 11 y Go 1.26.5, con corridas intercaladas. **No son comparables con otra máquina.** El detalle está en [`SEGUNDO-PARCIAL.md`](https://github.com/mordecoi/analizador-logs-go/blob/main/SEGUNDO-PARCIAL.md) y [`PERFILADO.md`](https://github.com/mordecoi/analizador-logs-go/blob/main/PERFILADO.md).

## Escalabilidad

Versión final, 16 workers, mediana de 5 corridas (3 a 20 M):

| Líneas | Tiempo | Con memoización | Memoria asignada | Heap retenido | Ciclos de GC |
|---|---:|---:|---:|---:|---:|
| 1 M (184 MB) | 127 ms | 65 ms | 76 MB | 23,4 MB | 5 |
| 5 M (919 MB) | 814 ms | 413 ms | 451 MB | 155 MB | 8 |
| 20 M (3,6 GB) | 3 986 ms | 1 905 ms | 1 770 MB | 612 MB | 11 |

El tiempo crece de forma lineal, como predice el análisis de complejidad. Contra la línea base (regex, ordenamiento completo y secuencial), la versión final es 57× más rápida a 1 M y 37× a 20 M. El gráfico está en [`escalabilidad.svg`](https://github.com/mordecoi/analizador-logs-go/blob/main/resultados/segundo-parcial/escalabilidad.svg).

## Benchmarks

| Benchmark | Comparación | Resultado |
|---|---|---|
| Lectura (200 k líneas) | `ReadString` contra lotes | 266 → 204 ms; 281 k → 81 k asignaciones |
| Formateo de una línea | `fmt.Fprintf` contra `strconv.Append*` | 3,80 → 2,12 µs; 11 → 0 asignaciones |
| Parser | Sin PGO contra con PGO | 436 → 361 ns (−17,2 %, p = 0,000) |
| Análisis de 5 M líneas | Sin PGO contra con PGO | 764 → 707 ms (−7,5 %, p = 0,000) |

## Profiling

El cuello de botella cambió con cada versión, por eso se perfila después de cada optimización:

| Versión | Principal consumo | Herramienta | Respuesta |
|---|---|---|---|
| Regex | Motor de regex, cerca del 70 % del CPU | `pprof` | Parser manual |
| Parser manual | Mapas y GC, por el string de cada línea | `pprof` `alloc_space` | Lectura por lotes |
| Concurrente | Fusión secuencial de los parciales | Traza en Jaeger | Fusión paralela |

Los perfiles de cada versión están en [`resultados/perfiles/`](https://github.com/mordecoi/analizador-logs-go/tree/main/resultados/perfiles). Cada ejecución del CI genera perfiles nuevos.

## Complejidad temporal

La tabla está en [Diseño técnico](diseno.md#algoritmos-importantes). El procesamiento completo es O(n·L + u log k).

El límite al crecer es la memoria, no el tiempo: el mapa de IPs crece O(n), porque casi cada línea puede aportar una IP nueva.

## Consumo de memoria

| Indicador (5 M líneas) | Primer parcial | Segundo parcial |
|---|---:|---:|
| Memoria asignada | 1 429 MB | 451 MB |
| Asignaciones | 7,04 M | 2,04 M |
| Ciclos de GC | 20 | 8 |
| Heap retenido | 137 MB | 155 MB |

El heap retenido sube un 13 %: es el costo del `Contador` y de las particiones preasignadas. A cambio, se asigna menos de un tercio de la memoria.

## Controles en el CI

Los benchmarks varían según la máquina, por lo que **no se comparan contra números guardados**. Cada ejecución corre la versión base y la actual en el mismo job, intercaladas 6 veces, y las compara con `benchstat`.

| Medición | Se rechaza si | Por qué ese criterio |
|---|---|---|
| Tiempo de benchmarks | Sube más de 20 % con p < 0,05 | Los runners compartidos tienen ruido |
| `B/op` y `allocs/op` | Suben más de 5 % con p < 0,05 | Casi no varían entre corridas |
| Heap retenido a 1 M líneas | Supera el límite en `controles.sh` | Medido con semilla y workers fijos |
| Memoria asignada a 1 M líneas | Supera el límite en `controles.sh` | Medido con semilla y workers fijos |
| Validación contra el *ground truth* | Cualquier diferencia | Es correctitud, no una medición |

Se mide con estos benchmarks: `BenchmarkParsear`, `BenchmarkLectura`, `BenchmarkFormatoLinea` y un caso de `BenchmarkTopN`. Quedan afuera `BenchmarkEscrituraBuffer` (ruido del disco), `BenchmarkTamLote` (exploratorio) y el resto de los casos de `BenchmarkTopN`, que tardan minutos.

El tiempo total del análisis se informa, pero no rechaza: una sola corrida en un runner compartido no alcanza para distinguir una regresión del ruido.
