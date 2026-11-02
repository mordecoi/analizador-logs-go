# CLI y contratos

El proyecto no expone una API de red. Su interfaz pública es la línea de comandos. La documentación de los paquetes internos se [genera desde el código](api/README.md) con gomarkdoc.

## `analizador analizar`

Analiza un log y muestra rankings, hora pico y, opcionalmente, anomalías.

| Flag | Valores | Por defecto | Efecto |
|---|---|---|---|
| `-a`, `--archivo` | ruta, `.gz` o `-` (stdin) | `logfiles.log` | Entrada |
| `-n`, `--topn` | entero | `10` | Cantidad de resultados por ranking |
| `--topn-metodo` | `heap`, `sort` | `heap` | Estrategia de Top-N |
| `--parser` | `manual`, `regex` | `manual` | Parser de líneas |
| `--modo` | `conc`, `seq`, `conc-mutex` | `conc` | Modo de procesamiento |
| `-w`, `--workers` | entero | `0` (GOMAXPROCS) | Workers del modo concurrente |
| `--cache` | — | apagado | Memoiza los agregados en disco |
| `--anomalias` | — | apagado | Pasada extra de detección |
| `--verificacion` | ruta JSON | — | Valida contra el *ground truth* |
| `--csv`, `--version-etiqueta` | ruta, texto | —, `final` | Agrega una fila de medición |
| `--cpuprofile`, `--memprofile`, `--trace` | ruta | — | Perfilado |

## `analizador generar`

Genera un log sintético y su *ground truth*.

| Flag | Por defecto | Efecto |
|---|---|---|
| `-n`, `--lineas` | `1000000` | Cantidad de líneas |
| `--semilla` | `0` (fija reproducible) | Semilla del generador |
| `-o`, `--salida` | `logfiles.log` | Log de salida |
| `--verificacion` | `ground-truth.json` | JSON con los agregados exactos |
| `--desde` | `2024-01-01` | Fecha de la primera solicitud |
| `--tasa` | `1.65` | Solicitudes por segundo promedio |
| `--heavy` | `200` | IPs de alta actividad |
| `--buffer` | `1048576` | Bytes del buffer de escritura (`-1` = sin buffer) |

## `analizador grafico`

Dibuja la curva de escalabilidad en SVG a partir de un CSV de mediciones. Los flags se consultan con `analizador grafico --help`.

## Flag global

| Flag | Efecto |
|---|---|
| `--otlp host:puerto` | Exporta trazas OpenTelemetry por OTLP/HTTP, sin TLS |

## Entradas

Líneas en formato **Apache Combined**:

```text
192.0.2.10 - ana [10/Oct/2024:13:55:36 -0300] "GET /api/users HTTP/1.1" 200 2326 "-" "curl/8.0"
```

Una línea que no respeta el formato no detiene el análisis: se cuenta como error y aparece en el reporte.

## Salidas

| Salida | Contenido |
|---|---|
| Salida estándar | Reporte de texto con rankings, hora pico y anomalías |
| `--csv` | Columnas `version,archivo,lineas,ms,lineas_por_seg,heap_mb,total_alloc_mb,allocs_millones,num_gc` |
| `--verificacion` en `generar` | JSON con los agregados exactos y las anomalías inyectadas |

## Códigos de salida y errores

Go no usa excepciones: los errores se devuelven como valores, `main` los imprime en la salida de error y los traduce a un código.

| Código | Significado | Ejemplo |
|---|---|---|
| `0` | Correcto | Análisis terminado y, si se pidió, validado |
| `1` | Error de ejecución o de uso | Archivo inexistente, `--modo paralelo` |
| `2` | Los resultados no coinciden con el *ground truth* (`cli.ErrValidacion`) | `--verificacion` con un JSON de otro log |

## Ejemplos de uso

```bash
# Comparar la versión inicial con la final
./analizador analizar -a logfiles.log --parser regex --modo seq --topn-metodo sort
./analizador analizar -a logfiles.log

# Usar el resultado de la validación en un script
./analizador analizar -a logfiles.log --verificacion ground-truth.json
if [ $? -eq 2 ]; then echo "resultados incorrectos"; fi

# Registrar una medición
./analizador analizar -a logfiles.log --csv mediciones.csv --version-etiqueta prueba
```
