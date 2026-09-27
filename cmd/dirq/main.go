package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/war-and-code/dircue/internal/cli"
)

func main() {
	// Same GOGC tuning as the dircue binary. See main.go for rationale.
	if _, set := os.LookupEnv("GOGC"); !set {
		debug.SetGCPercent(30)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cli.ExecuteAs(ctx, "dirq", os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
