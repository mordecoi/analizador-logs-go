# Pruebas

## Estrategia

`go test` cubre las pruebas sin dependencias externas. Se priorizan las pruebas que **comparan dos caminos** (regex contra manual, secuencial contra concurrente, resultado contra *ground truth*), porque fueron las que encontraron los errores más sutiles.

| Tipo | Qué verifica | Dónde |
|---|---|---|
| Tests de tabla | Comportamiento de cada paquete | `internal/*/*_test.go` |
| Fuzzing diferencial | El parser manual devuelve lo mismo que la regex | `internal/parse/fuzz_test.go` |
| De punta a punta | El comando Cobra real: generar, analizar y validar, con todas las combinaciones de parser, modo y Top-N, gzip, stdin, cache y códigos de salida | `internal/cli/cli_test.go` |
| Contrato de vistas | Nadie retiene strings del buffer reutilizado | `TestAnomaliasSobreLotes`, `TestContadorNoRetieneLaClave` |
| Bordes del lector | Lotes de 1 byte, líneas más largas que el buffer, CRLF, sin salto final | `internal/pipeline/lotes_test.go` |
| Integración OTLP | Los spans llegan a un receptor falso | `internal/telemetria/telemetria_test.go` |
| Ejemplos ejecutables | La documentación coincide con la salida real | Funciones `Example…` |
| Detector de carreras | No hay accesos concurrentes sin sincronizar | `go test -race`, en cada ejecución del CI |
| Validación de anomalías | Recall y precisión con semillas distintas | Barrido manual, ver resultados |

## Casos relevantes

| Caso | Por qué existe |
|---|---|
| `FuzzManualIgualQueRegex` y su corpus en `testdata/fuzz/` | Regresión de los casos que encontró el fuzzing, incluidos mes en otra capitalización y comillas en el request |
| `TestPicoNocturnoQueCruzaLaHora` | Reproduce el pico que el detector anterior no veía; falla con ese detector |
| `TestSortHeapEquivalentes` | Las dos estrategias de Top-N devuelven lo mismo |
| `TestFlagsInvalidos` | Un flag mal escrito es un error, no un valor por defecto silencioso |
| `TestAgregarLineaIgualQueFprintf` | La escritura optimizada produce los mismos bytes |

## Resultados

| Indicador | Resultado | Evidencia |
|---|---|---|
| Paquetes con tests | 11 de 11 aprobados | [Reporte de `go test`, en los artefactos del CI](mantenimiento.md#reportes) |
| Cobertura total | 92,6 % | `reportes/cobertura.html` |
| Detector de carreras | Sin hallazgos | [`race-detector.txt`](https://github.com/mordecoi/analizador-logs-go/blob/main/resultados/calidad/race-detector.txt) |
| Fuzzing | 45 M de ejecuciones en 5 minutos, sin divergencias tras las correcciones | [`SEGUNDO-PARCIAL.md`](https://github.com/mordecoi/analizador-logs-go/blob/main/SEGUNDO-PARCIAL.md) |
| Anomalías | 3/3 y 0 falsos positivos a 1 M, 5 M y 20 M, y en otras 15 semillas | [`anomalias-semillas.txt`](https://github.com/mordecoi/analizador-logs-go/blob/main/resultados/segundo-parcial/anomalias-semillas.txt) |

`cmd/analizador` figura con 0 % de cobertura porque no tiene lógica propia y su `main` no se prueba directamente. Ya está incluido en el total.

Las pruebas encontraron 8 errores reales, descritos en la sección 9 de `SEGUNDO-PARCIAL.md`. Entre ellos, un desborde de enteros en el parser y claves de `map` que quedaban apuntando a un buffer reutilizado.

## Ejecutar

```bash
scripts/controles.sh pruebas                                # igual que el CI
go test ./internal/parse -run TestManual                    # un paquete y un filtro
go test ./internal/parse -run '^$' -fuzz Fuzz -fuzztime 60s # fuzzing
```

El fuzzing no corre en el CI porque su duración es arbitraria. Sí corren los casos guardados en `testdata/fuzz/`, como parte de los tests comunes.
