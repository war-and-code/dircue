package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
)

// Main runs the command line for a program entry point and exits on error.
// An empty name selects the display name from the invoked program name.
func Main(name string) {
	// Git mode processes tens of thousands of pack objects, allocating and
	// immediately freeing ~3 GB of MemoryObject data for the Linux kernel.
	// The default GOGC=100 lets the heap grow to 2× live objects between GC
	// cycles, causing peak RSS of 850–960 MiB.  GOGC=30 keeps the heap tighter
	// (1.3× live objects), reducing peak RSS by ~35% at the cost of ~3–4% more
	// wall time on kernel-scale repos.  Honour an explicit GOGC env override so
	// users can restore the default behavior or tune further.
	if _, set := os.LookupEnv("GOGC"); !set {
		debug.SetGCPercent(30)
	}
	if name == "" {
		name = detectName(os.Args[0])
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := ExecuteAs(ctx, name, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		var handled *comparisonErrorExit
		if errors.As(err, &handled) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", handled)
			stop()
			os.Exit(3)
		}
		var outcome *comparisonExit
		if errors.As(err, &outcome) {
			stop()
			os.Exit(outcome.code)
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		stop()
		os.Exit(1)
	}
}
