package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"parcial1/internal/grafico"
)

func comandoGrafico() *cobra.Command {
	var csv, salida, titulo string
	cmd := &cobra.Command{
		Use:     "grafico",
		Short:   "Dibuja la curva de escalabilidad (SVG log-log) desde el CSV de mediciones",
		Example: "  analizador grafico --csv resultados/escalabilidad.csv --salida resultados/escalabilidad.svg",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			series, err := grafico.Leer(csv)
			if err != nil {
				return err
			}
			if err := os.WriteFile(salida, []byte(grafico.Dibujar(series, titulo)), 0o600); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "gráfico escrito en %s (%d series)\n", salida, len(series.Orden))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&csv, "csv", "resultados/escalabilidad.csv", "CSV de mediciones")
	f.StringVarP(&salida, "salida", "o", "resultados/escalabilidad.svg", "SVG de salida")
	f.StringVar(&titulo, "titulo", "Escalabilidad: tiempo vs líneas (log-log)", "título del gráfico")
	return cmd
}
