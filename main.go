package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"compass/cmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
