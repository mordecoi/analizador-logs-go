# Seguridad

## Riesgos identificados

El analizador es una herramienta local: no abre puertos y no maneja credenciales ni datos de usuarios propios. Sí procesa archivos que pueden venir de terceros.

| Riesgo | Impacto | Medida |
|---|---|---|
| Líneas de log malformadas o maliciosas | Fallo o resultados incorrectos | Fuzzing diferencial; control de desborde en `atoiSimple`; las líneas inválidas se cuentan y no detienen el análisis |
| Vistas `unsafe` sobre un buffer reutilizado | Datos corruptos si alguien retiene un string | Contrato documentado y tests que lo verifican (ver excepciones) |
| Trazas OTLP sin TLS (`WithInsecure`) | Las trazas viajan sin cifrar si `--otlp` apunta a un host remoto | **Riesgo aceptado:** pensado para un colector local. Documentado en la instalación |
| Cache `gob` leído de disco | Un `.agregados.gob` manipulado podría alterar los resultados o consumir memoria al decodificarse | **Riesgo aceptado:** solo se usa con `--cache`, y el archivo lo escribe el mismo usuario. No usar `--cache` en directorios de terceros |
| Logs muy grandes | Agotamiento de memoria, porque el mapa de IPs crece O(n) | Límite de memoria medido en el CI; riesgo operativo documentado en [Rendimiento](rendimiento.md#complejidad-temporal) |
| Dependencias vulnerables | Código de terceros con fallas conocidas | `govulncheck` y Dependabot |
| Cadena de suministro del CI | Una acción de terceros modificada | Acciones fijadas por SHA y permisos mínimos |
| Secretos en el repositorio | Filtración de credenciales | `gitleaks` sobre todo el historial de la versión probada; hoy sin hallazgos |

## Controles

| Requisito | Herramienta | Reporte | Rechaza si |
|---|---|---|---|
| Código fuente | `gosec` | Texto y SARIF | Severidad alta con confianza media o mayor |
| Código fuente (semántico) | CodeQL | Pestaña Security | No rechaza; se revisa a mano |
| Dependencias | `govulncheck` | Texto y SARIF | Una vulnerabilidad alcanzable desde el código |
| Actualizaciones | Dependabot (semanal) | PR y alertas | No rechaza; cada PR pasa por el CI |
| Secretos | `gitleaks` | SARIF, con los valores ocultos | Cualquier hallazgo no documentado como falso positivo |

## Política de bloqueo

| Severidad | Acción |
|---|---|
| Crítica confirmada, o cualquier vulnerabilidad alcanzable | **Bloquea** la integración |
| Secreto detectado | **Bloquea**. Además, hay que rotar la credencial: borrarla del código no alcanza, porque queda en el historial |
| Alta | `gosec` **bloquea**. Una alerta alta de CodeQL se revisa antes de integrar |
| Vulnerabilidad en una dependencia, pero no alcanzable | No bloquea; se actualiza en el siguiente PR de Dependabot |
| Media y baja | No bloquea; queda en el reporte |

Una alerta falsa se resuelve con una entrada en la tabla siguiente, con su motivo, en el mismo PR que la silencia.

## Excepciones y falsos positivos

**Reglas de `gosec` excluidas.** Son las mismas en `.golangci.yml` y en `scripts/controles.sh`:

| Regla | Motivo | Revisión |
|---|---|---|
| G304: lectura de archivo por ruta variable | Es la función del CLI: el usuario elige el archivo | 2026-11-02 |
| G404: `math/rand` | Lo usa el generador sintético; no es criptografía | 2026-11-02 |
| G115: conversiones entre enteros | Conversiones `int` e `int64` sobre tamaños de archivo acotados | 2026-11-02 |
| G306: permisos 0644 | Son reportes de salida, no secretos | 2026-11-02 |
| G104: errores sin chequear | Ya lo cubre `errcheck` | 2026-11-02 |

**Hallazgos de severidad baja que se conservan en el reporte.** No bloquean:

| Hallazgo | Ubicación | Análisis | Revisión |
|---|---|---|---|
| G103: uso de `unsafe` | `internal/pipeline/lotes.go:82` (`vista`) | Auditado: los únicos consumidores que retienen strings los clonan, y `TestAnomaliasSobreLotes` lo verifica | 2026-11-02 |
| G602: límites de slice | `internal/loggen/loggen.go:443` | Falso positivo: `elegida` solo toma el índice de una lista cuyo largo ya se comprobó que es mayor que 0 | 2026-11-02 |

## Medidas correctivas

Hallazgos de la primera ejecución de los controles, el 2026-11-02:

| Hallazgo | Herramienta | Corrección |
|---|---|---|
| 5 vulnerabilidades alcanzables de HTTP/2 en `golang.org/x/net` v0.59.0 (GO-2026-6617, 6612, 6611, 6610 y 6603), y 2 no alcanzables (GO-2026-6866 y 6860) | `govulncheck` | Actualización a v0.62.0 |
| Vulnerabilidades alcanzables de la biblioteca estándar en Go 1.26.5, en `net/http`, `crypto/tls`, `crypto/x509`, `net/url` y otros paquetes | `govulncheck` | `go.mod` pasa a Go 1.26.10 |

Después de ambas correcciones, `govulncheck` informa `No vulnerabilities found`.
