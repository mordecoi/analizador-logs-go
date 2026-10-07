// Package cli define la interfaz de línea de comandos del analizador con el
// framework Cobra.
//
// Reemplaza a los tres programas del primer parcial (cmd/generar, cmd/analizar
// y cmd/grafico), que repetían el mismo manejo de flags, de errores y de
// salida, por un único binario con subcomandos:
//
//	analizador generar   genera un log sintético y su ground truth
//	analizador analizar  analiza un log (archivo, .gz o stdin)
//	analizador grafico   dibuja la curva de escalabilidad desde un CSV
//
// Cobra aporta el parseo de flags POSIX (--flag, -f), la ayuda y el uso
// generados (--help), la validación de argumentos, flags persistentes que
// heredan todos los subcomandos y el autocompletado para la shell.
package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"parcial1/internal/telemetria"
)

// Version es la versión del analizador; se puede fijar al compilar con
// -ldflags "-X parcial1/internal/cli.Version=...".
var Version = "2.0.0"

// ErrValidacion indica que el análisis terminó bien pero los resultados no
// coinciden con el ground truth. El programa sale con código 2 (no 1), para
// que un script distinga "resultados incorrectos" de "no se pudo analizar".
var ErrValidacion = errors.New("los resultados no coinciden con el ground truth")

// NuevoComando arma el árbol de comandos. Devuelve un comando nuevo en cada
// llamada (sin estado global), así los tests pueden ejecutarlo varias veces.
func NuevoComando() *cobra.Command {
	var endpointOTLP string
	apagar := telemetria.Apagar(func(context.Context) error { return nil })

	raiz := &cobra.Command{
		Use:   "analizador",
		Short: "Analizador de grandes volúmenes de logs (Apache Combined)",
		Long: `Analizador de grandes volúmenes de logs en formato Apache Combined.

Extrae errores más frecuentes, endpoints más solicitados, usuarios e IPs más
activos, horas pico y patrones anómalos, midiendo tiempo y memoria de cada
corrida. Trabajo de Programación Eficiente, primer y segundo parcial.`,
		Version:       Version,
		SilenceUsage:  true, // ante un error de ejecución, no repetir la ayuda
		SilenceErrors: true, // el error lo imprime main, una sola vez
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			var err error
			apagar, err = telemetria.Iniciar(cmd.Context(), endpointOTLP, Version)
			return err
		},
		PersistentPostRunE: func(cmd *cobra.Command, _ []string) error {
			return apagar(cmd.Context())
		},
	}
	raiz.PersistentFlags().StringVar(&endpointOTLP, "otlp", "",
		"exportar trazas OpenTelemetry a este colector OTLP/HTTP (p. ej. localhost:4318)")

	raiz.AddCommand(comandoGenerar(), comandoAnalizar(), comandoGrafico())
	return raiz
}

// Ejecutar corre la CLI con los argumentos del proceso.
func Ejecutar(ctx context.Context) error {
	return NuevoComando().ExecuteContext(ctx)
}
