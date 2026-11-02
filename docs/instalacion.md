# Instalación y ejecución

## Dependencias

| Herramienta | Versión | Para qué |
|---|---|---|
| Go | 1.26.10 o superior | Compilar y ejecutar; las dependencias de Go se descargan solas |
| Docker | Cualquiera reciente | Opcional: Jaeger, para ver las trazas |

Las herramientas de los controles de calidad se detallan en [Mantenimiento](mantenimiento.md#ejecutar-los-controles-localmente).

## Compilar

```bash
git clone https://github.com/mordecoi/analizador-logs-go
cd analizador-logs-go
go build -o analizador ./cmd/analizador
./analizador --help
```

`go build` usa automáticamente `cmd/analizador/default.pgo` para optimizar con el perfil.

## Ejecutar

```bash
# 1. Generar datos de prueba (reproducible): logfiles.log (~184 MB) y ground-truth.json
./analizador generar -n 1000000

# 2. Analizar
./analizador analizar -a logfiles.log

# 3. Validar contra el ground truth y detectar anomalías
./analizador analizar -a logfiles.log --verificacion ground-truth.json --anomalias

# Entradas comprimidas o por stdin
./analizador analizar -a logs.gz
zcat logs.gz | ./analizador analizar -a -
```

## Configuración

Toda la configuración se pasa por flags: no hay archivos de configuración ni variables de entorno propias. La lista completa está en [CLI y contratos](cli.md).

## Ver las trazas

```bash
docker compose up -d
./analizador --otlp localhost:4318 analizar -a logfiles.log
```

Después, abrir <http://localhost:16686> y elegir el servicio `analizador-logs`.

!!! warning "Sin TLS"
    El exportador OTLP envía las trazas sin cifrar. Usalo solo con un colector local (ver [Seguridad](seguridad.md#riesgos-identificados)).

## Archivos que genera

| Archivo | Lo crea | ¿Se versiona? |
|---|---|---|
| `logfiles.log`, `ground-truth.json` | `generar` | No |
| `<log>.agregados.gob` | `analizar --cache` | No |
| `reportes/` | `scripts/controles.sh` | No |
| `site/` | `mkdocs build` | No |
