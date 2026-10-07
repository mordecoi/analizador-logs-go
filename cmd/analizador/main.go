// Comando analizador: punto de entrada de la CLI (ver internal/cli).
//
//	go run ./cmd/analizador --help
//	go run ./cmd/analizador generar --lineas 1000000
//	go run ./cmd/analizador analizar --archivo logfiles.log --verificacion ground-truth.json
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"parcial1/internal/cli"
)

func main() {
	// Ctrl+C cancela el contexto: el lector deja de producir lotes y el
	// programa termina limpio, cerrando perfiles y trazas.
	ctx, cancelar := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancelar()

	if err := cli.Ejecutar(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		cancelar()
		if errors.Is(err, cli.ErrValidacion) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
