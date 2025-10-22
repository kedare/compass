package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/kedare/compass/cmd"
	"github.com/kedare/compass/internal/logger"
)

func main() {
	// Initialize pterm to send all diagnostic output to stderr
	// This ensures stdout remains clean for JSON and structured output
	logger.InitPterm()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
