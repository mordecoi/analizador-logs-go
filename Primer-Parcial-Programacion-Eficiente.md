# **Primer Parcial – Programación Eficiente** 

## **Objetivo general** 

Cada grupo deberá desarrollar una aplicación que permita analizar y demostrar la aplicación de técnicas de **programación eficiente** . 

El trabajo deberá implementarse en **Python, Java o Go** , a elección del grupo. 

La evaluación se centrará no solamente en que la aplicación funcione correctamente, sino fundamentalmente en la capacidad de los estudiantes para: 

- Analizar la **complejidad algorítmica** . 

- Seleccionar el **algoritmo más adecuado** . 

- Elegir estructuras de datos eficientes. 

- Aplicar **memoización y/o caching inteligente** . 

- Incorporar **concurrencia y/o paralelismo** cuando resulte apropiado. 

- Realizar . **perfilado del código** 

- Medir, comparar y justificar las optimizaciones realizadas. 

# **Opción 1 – Planificador inteligente de** 

# **rutas** 

Desarrollar una aplicación que permita encontrar rutas óptimas dentro de una red de ciudades, estaciones, depósitos o puntos de entrega. 

### **Funcionalidades posibles** 

- Representación de una red mediante un grafo. 

- Búsqueda de la ruta más corta entre dos puntos. 

- Comparación entre diferentes algoritmos. 

- Cálculo de múltiples rutas. 

- Procesamiento simultáneo de consultas de rutas. 

- Simulación de cambios en el mapa. 

### **Conceptos de Programación Eficiente** 

#### **Complejidad algorítmica** 

- Analizar la complejidad de los algoritmos utilizados. 

- Comparar, por ejemplo, BFS, Dijkstra o A* según el problema planteado. 

#### **Estructuras de datos** 

- Grafos mediante listas de adyacencia. 

- HashMap / dict / map. 

- Priority Queue / Heap. 

#### **Caching** 

- Almacenar temporalmente rutas calculadas para evitar repetir cálculos. 

#### **Concurrencia o paralelismo** 

- Resolver múltiples consultas de rutas simultáneamente. 

#### **Perfilado** 

- Comparar el rendimiento de las distintas soluciones. 

### **Desafío** 

Demostrar con mediciones qué algoritmo resulta más conveniente según el tamaño y características del grafo. 

# **Opción 2 – Motor de recomendaciones eficiente** 

Desarrollar un sistema que recomiende productos, películas, cursos, libros u otro tipo de elementos a partir de preferencias e historial de usuarios. 

### **Funcionalidades posibles** 

- Registrar usuarios y preferencias. 

- Procesar historiales de interacción. 

- Calcular similitudes. 

- Generar recomendaciones. 

- Obtener los Top N resultados. 

### **Conceptos de Programación Eficiente** 

#### **Complejidad** 

- Analizar el coste de comparar usuarios o elementos. 

- Evitar soluciones ingenuas de complejidad excesiva. 

#### **Algoritmos** 

- Comparar diferentes estrategias de búsqueda de elementos similares. 

- Optimizar la obtención del Top N. 

#### **Estructuras de datos** 

- Hash tables. 

- Sets para intersecciones. 

- Heaps para mantener los mejores resultados. 

#### **Caching inteligente** 

- Cachear recomendaciones frecuentes. 

- Definir una estrategia de invalidación cuando cambian las preferencias. 

#### **Concurrencia** 

- Calcular recomendaciones para varios usuarios simultáneamente. 

#### **Perfilado** 

- Medir qué operaciones representan el mayor coste. 

### **Desafío** 

Comparar una implementación inicial simple con una versión optimizada y demostrar la mejora obtenida. 

# **Opción 3 – Analizador de grandes volúmenes de logs** 

Desarrollar una aplicación capaz de procesar archivos de logs de gran tamaño y obtener información relevante. 

Por ejemplo: 

- Errores más frecuentes. 

- Usuarios con mayor actividad. 

- Endpoints más utilizados. 

- Horas con mayor cantidad de solicitudes. 

- Detección de patrones anómalos. 

### **Conceptos de Programación Eficiente** 

#### **Complejidad** 

- Analizar la complejidad del procesamiento. 

#### **Estructuras de datos** 

- HashMap / Dictionary para conteos. 

- Sets para elementos únicos. 

- Heaps para obtener los eventos más frecuentes. 

#### **Algoritmos** 

- Comparar diferentes estrategias para ordenar y obtener los Top N eventos. 

#### **Caching** 

- Cachear resultados de consultas repetidas. 

#### **Concurrencia** 

- Dividir grandes archivos en bloques y procesarlos concurrentemente. 

#### **Paralelismo** 

- Procesar diferentes archivos o bloques utilizando múltiples núcleos cuando corresponda. 

#### **Perfilado** 

- Detectar si el cuello de botella se encuentra en CPU, I/O o memoria. 

### **Desafío** 

Procesar un volumen creciente de información y construir una tabla comparativa de rendimiento. 

# **Opción 4 – Buscador eficiente de información** 

Desarrollar un pequeño motor de búsqueda sobre una colección de documentos. 

El sistema deberá permitir realizar consultas por palabras o combinaciones de palabras. 

### **Ejemplo** 

Consulta: "java concurrencia" 

Resultado: 

1. documento_15.txt 

2. documento_48.txt 

3. documento_107.txt 

### **Conceptos de Programación Eficiente** 

#### **Primera versión** 

Realizar una búsqueda secuencial recorriendo todos los documentos. 

#### **Optimización** 

Construir un **índice invertido** . 

Por ejemplo: 

"java" → [Documento 1, Documento 5, Documento 20] 

"python" → [Documento 2, Documento 5, Documento 10] 

#### **Estructuras de datos** 

- HashMap. 

- Sets. 

- Listas. 

- Heap para ranking de resultados. 

#### **Complejidad** 

Comparar la búsqueda secuencial con la búsqueda utilizando índices. 

#### **Caching** 

- Cachear consultas frecuentes. 

#### **Concurrencia** 

- Construir índices o procesar documentos en paralelo. 

#### **Perfilado** 

- Medir dónde se concentra el tiempo de ejecución. 

### **Desafío** 

Demostrar cómo una estructura de datos adecuada puede cambiar significativamente el rendimiento de una aplicación. 

# **Opción 5 – Sistema de detección de transacciones sospechosas** 

Desarrollar una aplicación que procese una gran cantidad de transacciones y detecte comportamientos potencialmente anómalos. 

Por ejemplo: 

- Muchas operaciones en poco tiempo. 

- Importes inusualmente altos. 

- Repetición excesiva de operaciones. 

- Actividad simultánea desde diferentes ubicaciones simuladas. 

### **Conceptos de Programación Eficiente** 

#### **Algoritmos** 

- Definir algoritmos para detectar patrones. 

- Comparar estrategias de análisis. 

#### **Complejidad** 

- Analizar cómo escala el sistema al aumentar la cantidad de transacciones. 

#### **Estructuras de datos** 

- HashMap para agrupar por usuario. 

- Queue / Deque para mantener ventanas temporales. 

- Sets para detectar eventos repetidos. 

#### **Caching** 

- Cachear información de usuarios frecuentemente consultados. 

#### **Concurrencia** 

- Procesar diferentes grupos de transacciones simultáneamente. 

#### **Perfilado** 

- Identificar los cuellos de botella del análisis. 

### **Desafío** 

Procesar un flujo simulado de transacciones y comparar una implementación secuencial contra una concurrente o paralela. 

# **Opción 6 – Optimizador de inventario y pedidos** 

Desarrollar un sistema que gestione productos y permita optimizar la preparación de pedidos. 

### **Funcionalidades posibles** 

- Búsqueda rápida de productos. 

- Agrupación de pedidos. 

- Identificación de productos más solicitados. 

- Procesamiento de múltiples pedidos. 

- Cálculo de combinaciones o alternativas para completar pedidos. 

### **Conceptos de Programación Eficiente** 

#### **Complejidad** 

- Analizar las operaciones principales. 

#### **Elección de estructuras** 

Comparar, por ejemplo: 

Lista → búsqueda O(n) HashMap → búsqueda promedio O(1) 

#### **Algoritmos** 

- Optimizar la selección de productos. 

- Resolver problemas de combinación cuando corresponda. 

#### **Memoización** 

Aplicarla a cálculos repetitivos, por ejemplo, combinaciones de productos o cálculos de disponibilidad. 

#### **Caching** 

- Mantener información frecuentemente consultada. 

#### **Concurrencia** 

- Procesar múltiples pedidos simultáneamente. 

#### **Perfilado** 

- Comparar diferentes implementaciones. 

### **Desafío** 

Demostrar experimentalmente la diferencia entre utilizar una estructura de datos inadecuada y una adecuada. 

# **Opción 7 – Planificador de tareas y asignación de recursos** 

Desarrollar un sistema que reciba un conjunto de tareas con: 

- Duración. 

- Prioridad. 

- Dependencias. 

- Recursos necesarios. 

El sistema deberá generar un plan eficiente de ejecución. 

### **Ejemplo** 

Tarea A → 2 segundos Tarea B → depende de A Tarea C → independiente Tarea D → depende de B y C 

### **Conceptos de Programación Eficiente** 

#### **Algoritmos** 

- Ordenamiento topológico. 

- Planificación basada en prioridades. 

- Detección de dependencias. 

#### **Complejidad** 

Analizar el coste del algoritmo según la cantidad de tareas y dependencias. 

#### **Estructuras de datos** 

- Grafos. 

- Priority Queue. 

- HashMap. 

- Sets. 

#### **Caching** 

- Guardar resultados de planificaciones que no hayan cambiado. 

#### **Concurrencia** 

Las tareas independientes podrán ejecutarse simultáneamente. 

Ejemplo: 

┌── Tarea B ──┐ Tarea A ──┤ ├── Tarea D └── Tarea C ──┘ 

B y C podrían procesarse concurrentemente. 

#### **Perfilado** 

- Comparar ejecución secuencial y concurrente. 

### **Desafío** 

Demostrar que la concurrencia mejora el rendimiento solamente cuando existen tareas independientes y suficientes recursos disponibles. 

# **Opción 8 – Sistema inteligente de procesamiento de imágenes o archivos** 

Desarrollar una aplicación que procese un conjunto importante de archivos o imágenes. 

Las operaciones podrían incluir: 

- Lectura de archivos. 

- Cálculo de estadísticas. 

- Búsqueda de duplicados. 

- Generación de hashes. 

- Compresión simulada. 

- Transformaciones sobre datos. 

No es necesario desarrollar algoritmos complejos de procesamiento de imágenes; el foco debe estar en la . **eficiencia del procesamiento** 

### **Conceptos de Programación Eficiente** 

#### **Complejidad** 

- Analizar el coste de cada estrategia de procesamiento. 

#### **Algoritmos** 

Por ejemplo, comparar: 

- Comparación directa de archivos. 

- Uso de hashes para detectar duplicados. 

#### **Estructuras de datos** 

- HashMap para indexar hashes. 

- Sets para detección rápida. 

- Queues para gestionar trabajos pendientes. 

#### **Caching** 

- Evitar procesar nuevamente archivos que no hayan cambiado. 

#### **Memoización** 

- Reutilizar resultados de transformaciones o cálculos repetitivos. 

#### **Concurrencia** 

- Procesar varios archivos simultáneamente. 

#### **Paralelismo** 

- Utilizar múltiples núcleos para operaciones CPU-intensive. 

#### **Perfilado** 

- Determinar si la mejora depende del tipo de tarea: CPU-bound o I/O-bound. 

### **Desafío** 

Comparar el procesamiento secuencial, concurrente y paralelo. 

# **Requisitos comunes para todas las opciones** 

## **1. Implementación inicial** 

El grupo deberá presentar una primera implementación funcional. 

Esta versión servirá como **línea base o baseline** para las mediciones. 

## **2. Análisis de complejidad** 

Deberán identificar al menos **dos algoritmos u operaciones relevantes** y analizar su complejidad temporal. 

Por ejemplo: 

Búsqueda secuencial: O(n) Búsqueda con HashMap: O(1) promedio 

Ordenamiento: O(n log n) Comparación de todos contra todos: O(n²) 

No alcanza con indicar la notación: deberán explicar **cómo se obtiene y por qué resulta relevante para el problema** . 

## **3. Elección de algoritmo** 

Deberán justificar al menos una decisión de diseño relacionada con la selección de un algoritmo. 

Por ejemplo: 

Se reemplazó un algoritmo de búsqueda/comparación por otra estrategia porque, para el volumen esperado de datos, presenta una mejor escalabilidad. 

## **4. Estructuras de datos** 

**dos estructuras de datos** . Deberán utilizar y justificar al menos 

Por ejemplo: 

- Lista. 

- HashMap / Dictionary / Map. 

- Set. 

- Queue. 

- Stack. 

- Heap / Priority Queue. 

- Grafo. 

Se deberá explicar por qué esa estructura resulta adecuada considerando las operaciones principales. 

## **5. Memoización o caching inteligente** 

El grupo deberá implementar al menos una estrategia de reutilización de resultados. 

Deberá explicar: 

- Qué se almacena. 

- Por qué es conveniente almacenarlo. 

- Cuándo se reutiliza. 

- Cómo se evita devolver información obsoleta, si corresponde. 

- Qué mejora se obtiene. 

Es importante diferenciar: 

**Memoización:** almacenar resultados de cálculos para reutilizarlos. 

**Caching:** almacenar información o resultados de acceso frecuente, definiendo una estrategia de reutilización e invalidación. 

## **6. Concurrencia o paralelismo** 

Deberán identificar una parte del problema que pueda beneficiarse de procesamiento concurrente o paralelo. 

Se deberá justificar: 

- Qué tareas se ejecutan simultáneamente. 

- Por qué son independientes. 

- Qué mecanismo utiliza el lenguaje. 

- Qué problemas pueden aparecer. 

Por ejemplo: 

#### **Java** 

- ExecutorService. 

- CompletableFuture. 

- Parallel Streams. 

#### **Python** 

- threading. 

- multiprocessing. 

- concurrent.futures. 

- asyncio, cuando sea apropiado. 

#### **Go** 

- Goroutines. 

- Channels. 

- Worker Pools. 

No se valorará simplemente "usar threads". Deberán demostrar que existe una . **justificación técnica** 

## **7. Perfilado y medición** 

El proyecto deberá incluir mediciones antes y después de las optimizaciones. 

Cada grupo deberá utilizar herramientas compatibles con el lenguaje seleccionado. 

### **Python** 

- . 

- cProfile 

- . 

- line_profiler 

- . 

- memory_profiler 

- Scalene. 

- py-spy. 

### **Java** 

- Java Flight Recorder. 

- Java Mission Control. 

- VisualVM. 

### **Go** 

- pprof. 

- go tool trace. 

- Benchmarks integrados con testing. 

Las mediciones deberán responder preguntas como: 

- ¿Dónde está el principal cuello de botella? 

- ¿Qué optimización se realizó? 

- ¿Qué impacto tuvo? 

- ¿La mejora se mantiene al aumentar el volumen de datos? 

- ¿Qué trade-off se produjo? 

# **Comparación obligatoria** 

**Versión Tiempo Memoria Observación** Implementación inicial X ms X MB Baseline Mejora de Estructura optimizada X ms X MB búsqueda Algoritmo optimizado X ms X MB Mejor escalabilidad Concurrencia/ X ms X MB Evaluar overhead paralelismo 

Versión final 

Resultado final 

X ms 

X MB 

# **Propuesta para la presentación oral** 

Cada grupo podría disponer de aproximadamente **10 a 15 minutos** . 

## **Estructura sugerida del PowerPoint** 

### **1. El problema** 

- ¿Qué problema resolvieron? 

- ¿Por qué resulta interesante desde el punto de vista del rendimiento? 

### **2. Diseño inicial** 

- Arquitectura general. 

- Primera solución implementada. 

### **3. Complejidad algorítmica** 

- Operaciones críticas. 

- Complejidad temporal. 

- Posibles problemas de escalabilidad. 

### **4. Algoritmos y estructuras de datos** 

- Alternativas consideradas. 

- Decisiones tomadas. 

- Justificación. 

### **5. Memoización y caching** 

- Qué se almacenó. 

- Estrategia utilizada. 

- Impacto. 

### **6. Concurrencia y paralelismo** 

- Qué parte se paralelizó. 

- Implementación. 

- Problemas y trade-offs. 

### **7. Perfilado** 

- Herramientas utilizadas. 

- Resultados obtenidos. 

- Cuellos de botella detectados. 

### **8. Comparación de resultados** 

- Antes y después. 

- Gráficos y tablas. 

- Escalabilidad. 

### **9. Demostración del código** 

El grupo deberá mostrar las partes más relevantes del código y ejecutar o demostrar al menos una funcionalidad. 

### **10. Conclusiones** 

- Qué optimización produjo mayor impacto. 

- Qué decisión no produjo la mejora esperada. 

- Qué harían diferente en una nueva versión. 

