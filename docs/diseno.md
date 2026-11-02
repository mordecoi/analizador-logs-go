# Diseño técnico

Esta página resume las decisiones. La derivación de cada cota está en [`COMPLEJIDAD.md`](https://github.com/mordecoi/analizador-logs-go/blob/main/COMPLEJIDAD.md) y el detalle de cada optimización en [`SEGUNDO-PARCIAL.md`](https://github.com/mordecoi/analizador-logs-go/blob/main/SEGUNDO-PARCIAL.md).

## Estructuras de datos

| Estructura | Dónde | Por qué |
|---|---|---|
| `Contador`: mapa clave → índice más slice de conteos | `agregado` | Un hash por línea en vez de dos, y clona la clave solo al insertar |
| Min-heap de tamaño k | `agregado.TopN` | Top-N en O(u log k) tiempo y O(k) espacio, contra O(u log u) y O(u) de ordenar todo |
| Particiones por hash de la clave | `agregado.FusionarClaves` | Son disjuntas, así que se fusionan en paralelo sin locks |
| Ventanas deslizantes y sets | `anomalias` | Detección en O(n) con memoria acotada por la ventana |
| Pool de buffers en un canal | `pipeline.DesdeLector` | Limita la memoria a 2·N lotes sin importar el tamaño de la entrada |

## Algoritmos importantes

| Operación | Complejidad temporal | Espacio | Nota |
|---|---|---|---|
| Parseo de una línea | O(L) | O(1), sin asignar | Escaneo manual con `strings.IndexByte` |
| Conteo total | O(n) | O(u) | u crece casi como n para las IPs: el límite real es la memoria |
| Top-N | O(u log k) | O(k) | El sort gana cuando k se acerca a u, por constantes y caché |
| Lectura | O(n·L) | O(B), con B el tamaño del lote | Piso de rendimiento del proceso |
| Parseo y conteo concurrentes | O(n·L / P) de CPU | O(u·P) | Sin estado compartido |
| Detector de anomalías | O(n) | Acotado por la ventana | Pasada secuencial |

n = líneas, L = largo de línea, u = claves distintas, k = tamaño del ranking, P = workers.

## Decisiones de implementación

### Parser manual en lugar de regex

La regex consumía cerca del 70 % del CPU. El parser manual mantiene la clase O(n·L), pero no asigna memoria y es el principal salto de la primera etapa: 4,9× en el pipeline. A cambio, queda acoplado al formato exacto. El fuzzing diferencial exige que ambos parsers devuelvan lo mismo.

### Lectura por lotes con vistas sin copia

El 64 % de lo asignado era un string nuevo por línea. Ahora cada worker lee bloques de 1 MiB a un buffer reutilizado y entrega cada línea como una vista con `unsafe.String`.

**Contrato:** los strings de una `Entrada` solo valen durante el callback, y quien los retiene debe clonarlos. Es el único `unsafe` del proyecto, y está auditado en [Seguridad](seguridad.md#excepciones-y-falsos-positivos).

### Fusión paralela

Una traza de Jaeger mostró que la fusión secuencial de los parciales tardaba más que todo el trabajo paralelo: la ley de Amdahl a la vista. Particionar las claves por hash en origen permite fusionar sin locks, y bajó de 1 747 a 133 ms en 5 M líneas. A cambio, el heap retenido sube entre un 12 y un 17 %.

### Memoización en disco

Los agregados se guardan en `<log>.agregados.gob` y se reutilizan si el log no cambió, según su tamaño, fecha de modificación y versión del formato. Cuando el análisis se aceleró 3,4×, cargar el cache llegó a tardar más que recalcular. Se resolvió serializando slices paralelos y armando el índice en paralelo.

### PGO

`cmd/analizador/default.pgo` permite al compilador inlinear las funciones calientes: −7,5 % en el análisis completo (p = 0,000). Hay que regenerarlo cuando cambia el código caliente: un perfil viejo no rompe nada, pero deja de ayudar.

### Detector de picos estacional

Un umbral global no veía un pico nocturno que cruzaba una hora en punto. El detector compara cada franja con la misma hora de otros días, une franjas consecutivas en un episodio y exige un tamaño de efecto de 2×, además de los 3σ, para evitar falsos positivos.
