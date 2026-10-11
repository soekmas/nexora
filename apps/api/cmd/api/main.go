// Package main is the entrypoint for the HTTP API daemon.
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

	if err := backend.RunAPI(ctx); err != nil {
		slog.Error("NEXORA API daemon failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
