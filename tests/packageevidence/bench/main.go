// Command package-import-bench measures only the Go import API over an existing report.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"dircue/pkg/packageevidence"
)

func main() {
	iterations := flag.Int("iterations", 1, "Timed imports after one untimed validation")
	reportPath := flag.String("report", "", "Existing native Syft JSON fixture")
	flag.Parse()
	if *iterations < 1 || *iterations > 10000 || *reportPath == "" {
		fmt.Fprintln(os.Stderr, "report and 1..10000 iterations are required")
		os.Exit(1)
	}
	data, err := os.ReadFile(*reportPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	report, err := packageevidence.Import(context.Background(), bytes.NewReader(data), packageevidence.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	packages, relationships := len(report.Packages), len(report.Relationships)
	if report.Status != "complete" {
		fmt.Fprintln(os.Stderr, "fixture import is incomplete")
		os.Exit(1)
	}
	report = nil
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	started := time.Now()
	for i := 0; i < *iterations; i++ {
		parsed, err := packageevidence.Import(context.Background(), bytes.NewReader(data), packageevidence.Options{})
		if err != nil || parsed.Status != "complete" || len(parsed.Packages) != packages || len(parsed.Relationships) != relationships {
			fmt.Fprintln(os.Stderr, "timed import disagrees with validation")
			os.Exit(1)
		}
		runtime.KeepAlive(parsed)
	}
	elapsed := time.Since(started)
	runtime.ReadMemStats(&after)
	json.NewEncoder(os.Stdout).Encode(map[string]any{"iterations": *iterations, "report_bytes": len(data), "packages": packages, "relationships": relationships, "elapsed_ns": elapsed.Nanoseconds(), "ns_per_import": float64(elapsed.Nanoseconds()) / float64(*iterations), "allocated_bytes_per_import": float64(after.TotalAlloc-before.TotalAlloc) / float64(*iterations), "allocations_per_import": float64(after.Mallocs-before.Mallocs) / float64(*iterations), "garbage_collections": after.NumGC - before.NumGC, "gomaxprocs": runtime.GOMAXPROCS(0), "go_version": runtime.Version()})
	runtime.KeepAlive(data)
}
