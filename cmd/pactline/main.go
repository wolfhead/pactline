package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/wolfhead/pactline/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(cli.Execute(ctx, os.Stdin, os.Stdout, os.Stderr))
}
