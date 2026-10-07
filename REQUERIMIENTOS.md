# Requerimientos — Programación Eficiente

Requerimientos acumulados de los dos parciales. El segundo parcial **se construye sobre el
primero**: todo lo pedido en la parte 1 sigue vigente y la parte 2 agrega temas nuevos.

Lenguaje elegido: **Go** · Opción elegida: **3 — Analizador de grandes volúmenes de logs**.

---

## Parte 1 — Primer parcial

### Objetivo general

Desarrollar una aplicación que permita analizar y demostrar la aplicación de técnicas de
**programación eficiente**, implementada en Python, Java o Go.

La evaluación se centra no solamente en que la aplicación funcione correctamente, sino
fundamentalmente en la capacidad de:

- Analizar la **complejidad algorítmica**.
- Seleccionar el **algoritmo más adecuado**.
- Elegir **estructuras de datos eficientes**.
- Aplicar **memoización y/o caching inteligente**.
- Incorporar **concurrencia y/o paralelismo** cuando resulte apropiado.
- Realizar **perfilado del código**.
- Medir, comparar y justificar las optimizaciones realizadas.

### Opción 3 — Analizador de grandes volúmenes de logs

Aplicación capaz de procesar archivos de logs de gran tamaño y obtener información relevante:

- Errores más frecuentes.
- Usuarios con mayor actividad.
- Endpoints más utilizados.
- Horas con mayor cantidad de solicitudes.
- Detección de patrones anómalos.

| Concepto | Qué se pide |
|---|---|
| Complejidad | Analizar la complejidad del procesamiento. |
| Estructuras de datos | HashMap para conteos · Sets para elementos únicos · Heaps para los eventos más frecuentes. |
| Algoritmos | Comparar estrategias para ordenar y obtener los Top N eventos. |
| Caching | Cachear resultados de consultas repetidas. |
| Concurrencia | Dividir archivos grandes en bloques y procesarlos concurrentemente. |
| Paralelismo | Procesar archivos o bloques usando múltiples núcleos cuando corresponda. |
| Perfilado | Detectar si el cuello de botella está en CPU, I/O o memoria. |

**Desafío:** procesar un volumen creciente de información y construir una tabla comparativa
de rendimiento.

### Requisitos comunes

1. **Implementación inicial.** Primera versión funcional que sirve como **línea base**
   (baseline) para las mediciones.
2. **Análisis de complejidad.** Al menos **dos algoritmos u operaciones relevantes** con su
   complejidad temporal. No alcanza con la notación: hay que explicar **cómo se obtiene y por
   qué es relevante** para el problema.
3. **Elección de algoritmo.** Justificar al menos una decisión de diseño sobre la selección
   de un algoritmo (p. ej. se reemplazó una estrategia por otra que escala mejor para el
   volumen esperado).
4. **Estructuras de datos.** Usar y justificar al menos **dos** (lista, map, set, queue,
   stack, heap, grafo…), explicando por qué son adecuadas para las operaciones principales.
5. **Memoización o caching inteligente.** Al menos una estrategia de reutilización,
   explicando qué se almacena, por qué conviene, cuándo se reutiliza, cómo se evita devolver
   información obsoleta y qué mejora se obtiene. Diferenciar:
   - **Memoización:** almacenar resultados de cálculos para reutilizarlos.
   - **Caching:** almacenar información de acceso frecuente con una estrategia de
     reutilización e invalidación.
6. **Concurrencia o paralelismo.** Identificar una parte del problema que se beneficie y
   justificar qué tareas corren simultáneamente, por qué son independientes, qué mecanismo
   usa el lenguaje (en Go: goroutines, channels, worker pools) y qué problemas pueden
   aparecer. No se valora "usar threads" sin **justificación técnica**.
7. **Perfilado y medición.** Mediciones antes y después de las optimizaciones con
   herramientas del lenguaje (en Go: `pprof`, `go tool trace`, benchmarks de `testing`). Deben
   responder:
   - ¿Dónde está el principal cuello de botella?
   - ¿Qué optimización se realizó?
   - ¿Qué impacto tuvo?
   - ¿La mejora se mantiene al aumentar el volumen de datos?
   - ¿Qué trade-off se produjo?

### Comparación obligatoria

| Versión | Tiempo | Memoria | Observación |
|---|---|---|---|
| Implementación inicial | X ms | X MB | Baseline |
| Estructura optimizada | X ms | X MB | Mejora de búsqueda |
| Algoritmo optimizado | X ms | X MB | Mejor escalabilidad |
| Concurrencia / paralelismo | X ms | X MB | Evaluar overhead |
| Versión final | X ms | X MB | Resultado final |

---

## Parte 2 — Segundo parcial

Sobre el proyecto del primer parcial, agregar:

| # | Tema | Qué se pide | Herramienta / enfoque en Go |
|---|---|---|---|
| 1 | **Eliminar código redundante** | Detectar y quitar código duplicado o muerto. | `deadcode`, linter `dupl` |
| 2 | **Compiladores y herramientas de optimización** | Usar una de acuerdo al lenguaje (Python: Scalene, py-spy, SnakeViz · Java: Java Flight Recorder, Java Mission Control). | Compilador `gc`: PGO (`-pgo`), análisis de escape e inlining (`-gcflags=-m`), eliminación de bounds checks; `pprof`, `benchstat` |
| 3 | **Herramientas generales de monitoreo** | Aplicar una (YourKit, New Relic, Elastic APM o cualquier otra). | OpenTelemetry + Jaeger (el mismo protocolo OTLP que aceptan New Relic y Elastic APM) |
| 4 | **Escritura y lectura de archivos grandes** | Batching, buffers, procesamiento en paralelo. | Lectura por lotes con buffer reutilizable, `bufio`, workers en paralelo |
| 5 | **Legibilidad y mantenibilidad** | Nombres de variables, comentarios y docstrings, organización y formato del código (PEP 8 en Python). | `gofmt`/`goimports` (el equivalente de PEP 8), doc comments, Effective Go |
| 6 | **Generadores de documentación** | Sphinx (o cualquier otra herramienta). | `go doc` / `pkgsite`, `gomarkdoc`, funciones `Example` |
| 7 | **Herramientas de análisis de calidad** | SonarQube (o cualquier otra herramienta). | `golangci-lint` (staticcheck, revive, gocyclo, gosec, …) |
| 8 | **Refactorización** | Mejorar la estructura del código sin cambiar su comportamiento. | Guiada por los hallazgos del análisis de calidad y verificada con tests y benchmarks |
| 9 | **Testing** | Pruebas del sistema. | `go test`: tests de tabla, fuzzing, benchmarks, cobertura |
| 10 | **Uso de algún framework** | Incorporar un framework. | Cobra (framework de CLI) |

---

## Presentación oral (10 a 15 minutos)

Estructura sugerida:

1. **El problema:** qué se resolvió y por qué es interesante desde el rendimiento.
2. **Diseño inicial:** arquitectura general y primera solución.
3. **Complejidad algorítmica:** operaciones críticas, complejidad temporal, problemas de escalabilidad.
4. **Algoritmos y estructuras de datos:** alternativas consideradas, decisiones y justificación.
5. **Memoización y caching:** qué se almacenó, estrategia e impacto.
6. **Concurrencia y paralelismo:** qué se paralelizó, implementación, problemas y trade-offs.
7. **Perfilado:** herramientas, resultados y cuellos de botella.
8. **Comparación de resultados:** antes y después, gráficos y tablas, escalabilidad.
9. **Demostración del código:** partes relevantes y ejecución de al menos una funcionalidad.
10. **Conclusiones:** qué optimización tuvo más impacto, cuál no dio la mejora esperada y qué
    harían diferente.

Para el segundo parcial, la presentación suma los temas de la Parte 2.
