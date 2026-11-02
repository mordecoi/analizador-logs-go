#!/usr/bin/env bash
# Controles de calidad y eficiencia del proyecto. El CI ejecuta este mismo
# script, así que lo que pasa acá pasa allá.
#
#   scripts/controles.sh estatico|pruebas|seguridad|docs|rendimiento|todo
#
# Los reportes quedan en reportes/. Ver docs/mantenimiento.md.
set -euo pipefail

cd "$(dirname "$0")/.."

# Versiones fijadas de las herramientas: se ejecutan con `go run` para no sumar
# dependencias al go.mod del proyecto.
GOLANGCI=github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
GOCYCLO=github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0
GOSEC=github.com/securego/gosec/v2/cmd/gosec@v2.30.0
GOVULNCHECK=golang.org/x/vuln/cmd/govulncheck@v1.9.0
GITLEAKS=github.com/zricethezav/gitleaks/v8@v8.30.1
BENCHSTAT=golang.org/x/perf/cmd/benchstat@v0.0.0-20261009192801-be2c69fb417e
GOMARKDOC=github.com/princjef/gomarkdoc/cmd/gomarkdoc@v1.1.0
MARKDOWNLINT=markdownlint-cli2@0.24.0

# Criterios de aprobación. Justificados en docs/mantenimiento.md y
# docs/rendimiento.md.
COBERTURA_MIN=85
GOSEC_EXCLUIR=G304,G404,G115,G306,G104 # las mismas que .golangci.yml
REGRESION_TIEMPO_PCT=20
REGRESION_ASIGNACION_PCT=5
REPETICIONES_BENCH=6
LINEAS_E2E=1000000
SEMILLA_E2E=42
WORKERS_E2E=4
HEAP_MAX_MB=29
ASIGNADO_MAX_MB=82

REPORTES=$PWD/reportes
mkdir -p "$REPORTES"

titulo() { printf '\n== %s\n' "$1"; }

estatico() {
	titulo "go.mod ordenado y verificado"
	go mod tidy -diff
	go mod verify

	titulo "compilación y go vet (tipado)"
	go build ./...
	go vet ./...

	titulo "golangci-lint (análisis estático y límites de complejidad)"
	go run "$GOLANGCI" run ./... | tee "$REPORTES/lint.txt"

	titulo "las 10 funciones más complejas (informativo)"
	go run "$GOCYCLO" -top 10 -avg -ignore '_test' cmd internal | tee "$REPORTES/complejidad.txt"
}

pruebas() {
	local carrera=()
	# En Windows, Bitdefender bloquea el binario de internal/pipeline compilado con
	# -race (falso positivo probable, ver docs/mantenimiento.md). El CI corre en
	# Linux y siempre lo ejecuta.
	if [ "$(go env GOOS)" != windows ]; then
		export CGO_ENABLED=1
		carrera=(-race)
	else
		echo "AVISO: en Windows no corre el detector de carreras; el CI sí lo ejecuta"
	fi

	titulo "tests ${carrera[*]:-} y cobertura"
	go test "${carrera[@]}" -covermode=atomic -coverprofile="$REPORTES/cobertura.out" ./... |
		tee "$REPORTES/pruebas.txt"
	go tool cover -html="$REPORTES/cobertura.out" -o "$REPORTES/cobertura.html"

	local total
	total=$(go tool cover -func="$REPORTES/cobertura.out" | awk '/^total:/ { sub(/%/, "", $3); print $3 }')
	echo "cobertura total: $total % (mínimo: $COBERTURA_MIN %)" | tee "$REPORTES/cobertura.txt"
	if ! awk -v t="$total" -v m="$COBERTURA_MIN" 'BEGIN { exit !(t + 0 >= m + 0) }'; then
		echo "RECHAZADO: la cobertura está por debajo del mínimo"
		return 1
	fi
}

seguridad() {
	titulo "gosec: reporte completo"
	go run "$GOSEC" -quiet -no-fail -exclude="$GOSEC_EXCLUIR" \
		-fmt=sarif -out="$REPORTES/gosec.sarif" -stdout -verbose=text ./... |
		tee "$REPORTES/gosec.txt"

	titulo "gosec: rechaza severidad alta con confianza media o mayor"
	go run "$GOSEC" -quiet -severity=high -confidence=medium -exclude="$GOSEC_EXCLUIR" ./...

	titulo "govulncheck: rechaza vulnerabilidades alcanzables desde el código"
	go run "$GOVULNCHECK" -format=sarif ./... >"$REPORTES/govulncheck.sarif"
	go run "$GOVULNCHECK" ./... | tee "$REPORTES/govulncheck.txt"

	# Solo el historial de esta versión: por defecto revisa todas las ramas, y un
	# secreto en una rama haría fallar el CI de todas las demás.
	titulo "gitleaks: secretos en el historial de esta versión"
	go run "$GITLEAKS" git --redact --no-banner --log-opts=HEAD \
		--report-format=sarif --report-path="$REPORTES/gitleaks.sarif"
}

docs() {
	titulo "sitio de documentación"
	python -m mkdocs build --strict --quiet

	titulo "la API generada está al día"
	# Se genera aparte y se compara: el código de salida de gomarkdoc --check no es
	# confiable (casi siempre da 0 aunque haya diferencias).
	local generada
	generada=$(mktemp -d)
	go run "$GOMARKDOC" --output "$generada/{{.Dir}}.md" ./internal/...
	if ! diff -r --strip-trailing-cr "$generada/internal" docs/api/internal; then
		rm -rf "$generada"
		echo "RECHAZADO: docs/api está desactualizada; regenerarla (ver docs/mantenimiento.md)"
		return 1
	fi
	rm -rf "$generada"

	titulo "markdownlint"
	npx --yes "$MARKDOWNLINT"

	titulo "enlaces de los Markdown versionados"
	local archivos
	# docs/api queda afuera: sus enlaces al código los genera gomarkdoc, que ya se
	# verifica arriba, y son cientos de pedidos a GitHub que a veces dan 504.
	mapfile -t archivos < <(git ls-files '*.md' ':!docs/api/internal')
	# Se excluye lo que no se puede verificar desde el CI: artifacts que piden
	# sesión, el sitio y el badge. Un error temporal del servidor (429, 502, 503,
	# 504) no prueba que el enlace esté roto.
	lychee --no-progress --exclude-loopback --accept '200..=299,429,502..=504' \
		--exclude '^https://claude\.ai/' \
		--exclude '^https://mordecoi\.github\.io/' \
		--exclude '^https://github\.com/mordecoi/analizador-logs-go/actions' \
		--format markdown --output "$REPORTES/enlaces.md" "${archivos[@]}" ||
		{
			cat "$REPORTES/enlaces.md"
			return 1
		}
}

# refBase elige contra qué versión comparar: BASE_REF si se indicó; si no, donde
# la rama actual se separó de main, o el commit anterior si ya estamos en main.
refBase() {
	if [ -n "${BASE_REF:-}" ]; then
		echo "$BASE_REF"
		return
	fi
	local base
	base=$(git merge-base origin/main HEAD)
	if [ "$base" = "$(git rev-parse HEAD)" ]; then
		base=$(git rev-parse HEAD~1)
	fi
	echo "$base"
}

# correrBenchmarks corre, en el módulo del directorio actual, los benchmarks
# representativos del camino caliente. Quedan afuera BenchmarkEscrituraBuffer
# (ruido del disco), BenchmarkTamLote (exploratorio) y el resto de los casos de
# BenchmarkTopN (tardan minutos).
correrBenchmarks() {
	go test ./internal/parse ./internal/pipeline ./internal/loggen -run '^$' \
		-bench '^(BenchmarkParsear|BenchmarkLectura|BenchmarkFormatoLinea)$' \
		-benchtime=0.3s -benchmem -count=1
	go test ./internal/agregado -run '^$' \
		-bench '^BenchmarkTopN$/^heap$/^u=1200000$/^k=100$' \
		-benchtime=0.3s -benchmem -count=1
}

# hayRegresion lee el CSV de benchstat y devuelve 0 si alguna métrica empeoró de
# forma significativa (p < 0,05) más allá de su tolerancia.
hayRegresion() {
	awk -F, -v tiempo="$REGRESION_TIEMPO_PCT" -v asignacion="$REGRESION_ASIGNACION_PCT" '
		$3 == "CI" {
			limite = ""
			if ($2 == "sec/op") limite = tiempo
			else if ($2 == "B/op" || $2 == "allocs/op") limite = asignacion
			unidad = $2
			next
		}
		limite == "" || $1 == "" || $1 == "geomean" || $6 !~ /^\+/ { next }
		{
			p = $7
			sub(/^p=/, "", p)
			sub(/ .*/, "", p)
			if (p + 0 >= 0.05) next
			delta = $6
			gsub(/[+%]/, "", delta)
			if (delta == "Inf" || delta + 0 > limite + 0) {
				print "regresión: " $1 " " unidad " " $6 " (" $7 ")"
				hallada = 1
			}
		}
		END { exit !hallada }
	' "$1"
}

rendimiento() {
	local tmp base rechazado=0
	tmp=$(mktemp -d)
	# shellcheck disable=SC2064 # tmp se expande ahora a propósito
	trap "git worktree remove --force '$tmp/base' 2>/dev/null; rm -rf '$tmp'" RETURN

	base=$(refBase)
	titulo "benchmarks: base $(git rev-parse --short "$base") contra la versión actual"
	git worktree add --quiet --detach "$tmp/base" "$base"
	# Corridas intercaladas: el ruido de la máquina afecta a las dos por igual.
	local i
	for ((i = 1; i <= REPETICIONES_BENCH; i++)); do
		(cd "$tmp/base" && correrBenchmarks) >>"$tmp/base.txt"
		correrBenchmarks >>"$tmp/actual.txt"
	done
	(
		cd "$tmp"
		go run "$BENCHSTAT" base.txt actual.txt
	) | tee "$REPORTES/benchstat.txt"
	(
		cd "$tmp"
		go run "$BENCHSTAT" -format csv base.txt actual.txt 2>/dev/null
	) >"$REPORTES/benchstat.csv"
	if hayRegresion "$REPORTES/benchstat.csv"; then
		rechazado=1
	fi

	titulo "análisis completo de $LINEAS_E2E líneas: validación y memoria"
	local exe=$tmp/analizador$(go env GOEXE)
	go build -o "$exe" ./cmd/analizador
	"$exe" generar -n "$LINEAS_E2E" --semilla "$SEMILLA_E2E" -o "$tmp/e2e.log" --verificacion "$tmp/e2e-gt.json" >/dev/null
	rm -f "$REPORTES/rendimiento.csv"
	# Un resultado distinto del ground truth termina con código 2 y rechaza
	# siempre: es un error de correctitud, no una medición con ruido.
	"$exe" analizar -a "$tmp/e2e.log" -w "$WORKERS_E2E" --verificacion "$tmp/e2e-gt.json" --anomalias \
		--csv "$REPORTES/rendimiento.csv" --version-etiqueta ci \
		--cpuprofile "$REPORTES/cpu.pprof" --memprofile "$REPORTES/mem.pprof" >"$REPORTES/analisis.txt"
	cat "$REPORTES/rendimiento.csv"

	titulo "perfiles de CPU y memoria (informativo)"
	go tool pprof -top -nodecount=15 "$exe" "$REPORTES/cpu.pprof" | tee "$REPORTES/perfil-cpu.txt"
	go tool pprof -top -nodecount=15 -sample_index=alloc_space "$exe" "$REPORTES/mem.pprof" |
		tee "$REPORTES/perfil-memoria.txt"

	if ! awk -F, -v heap="$HEAP_MAX_MB" -v asignado="$ASIGNADO_MAX_MB" '
		NR == 2 {
			print "heap retenido: " $6 " MB (máximo: " heap ")"
			print "memoria asignada: " $7 " MB (máximo: " asignado ")"
			exit !($6 + 0 <= heap + 0 && $7 + 0 <= asignado + 0)
		}' "$REPORTES/rendimiento.csv"; then
		echo "regresión: se superó un límite de memoria"
		rechazado=1
	fi

	if [ "$rechazado" = 1 ]; then
		echo "RECHAZADO: regresión de rendimiento o de memoria"
		return 1
	fi
}

case "${1:-}" in
estatico | pruebas | seguridad | docs | rendimiento) "$1" ;;
todo)
	estatico
	pruebas
	seguridad
	docs
	rendimiento
	;;
*)
	echo "uso: $0 estatico|pruebas|seguridad|docs|rendimiento|todo" >&2
	exit 64
	;;
esac
