// Package main is the entrypoint for the background worker daemon.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"example.com/nexora/backend"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := backend.RunWorker(ctx); err != nil {
		slog.Error("NEXORA Worker daemon failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
