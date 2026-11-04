# Analizador de grandes volúmenes de logs

Trabajo de **Programación Eficiente**, implementado en **Go** por Sol Checa, Facundo Gómez Geneiro y Manu Nicolais.

## Problema que resuelve

Los logs de un servidor crecen hasta gigabytes y responder preguntas simples sobre ellos ("¿qué endpoint es el más usado?") se vuelve lento con herramientas ingenuas. El analizador procesa archivos en formato **Apache Combined** y obtiene esas respuestas en segundos: 20 millones de líneas (3,6 GB) en unos 4 s, con validación automática de los resultados.

El foco del proyecto no es solo que funcione, sino **medir y justificar** cada decisión de diseño.

## Alcance

**Incluye:**

- Errores más frecuentes, endpoints más solicitados, usuarios e IPs más activos y horas pico.
- Detección de patrones anómalos: ráfagas de una IP, picos de errores 5xx y escaneos de rutas.
- Entrada por archivo, `.gz` o `stdin`.
- Generador de logs sintéticos reproducibles, con su *ground truth* para validar el análisis.
- Memoización de agregados en disco, perfilado y exportación de trazas OpenTelemetry.

**No incluye:**

- Formatos de log distintos de Apache Combined.
- Análisis en tiempo real, servicio de red o interfaz gráfica: es una herramienta de línea de comandos.
- Almacenamiento histórico de los resultados.

## Requisitos

| Tipo | Requisito | Cómo se verifica |
|---|---|---|
| Funcional | Rankings correctos de endpoints, estados e IPs, y hora pico | `--verificacion` los compara clave por clave con el *ground truth* |
| Funcional | Detectar las tres anomalías inyectadas | Recall 3/3 y precisión 100 % a 1 M, 5 M y 20 M líneas, y en otras 15 semillas |
| Eficiencia | Procesar volúmenes crecientes en tiempo lineal | [Curva de escalabilidad](rendimiento.md#escalabilidad) |
| Eficiencia | No introducir regresiones de tiempo ni de memoria | [Etapa de rendimiento del CI](rendimiento.md#controles-en-el-ci) |
| Calidad | Código sin errores de análisis estático ni vulnerabilidades alcanzables | [Matriz de controles](mantenimiento.md#matriz-de-controles) |

Los requisitos académicos de los parciales anteriores están en [`REQUERIMIENTOS.md`](https://github.com/mordecoi/analizador-logs-go/blob/main/REQUERIMIENTOS.md).

## Dónde seguir

| Si querés… | Leé |
|---|---|
| Ejecutarlo | [Instalación y ejecución](instalacion.md) |
| Entender cómo está armado | [Arquitectura](arquitectura.md) y [Diseño técnico](diseno.md) |
| Usarlo desde un script | [CLI y contratos](cli.md) |
| Contribuir o arreglar el pipeline | [Mantenimiento](mantenimiento.md) |
| Probar el CI | [Enlace roto intencionalmente](ci-demo-enlace-inexistente.md) |
