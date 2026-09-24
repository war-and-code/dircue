package cli

import (
	"fmt"
	"os"
	"runtime/pprof"
)

// startCPUProfile starts CPU profiling and writes the profile to path when
// the returned stop function is called. If path is empty, it is a no-op.
// The stop function is always safe to defer; it does nothing when no profile
// is active.
func startCPUProfile(path string) (stop func(), err error) {
	if path == "" {
		return func() {}, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return func() {}, fmt.Errorf("--cpuprofile: create %s: %w", path, err)
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		return func() {}, fmt.Errorf("--cpuprofile: start: %w", err)
	}
	return func() {
		pprof.StopCPUProfile()
		f.Close()
	}, nil
}

// writeHeapProfile writes a heap profile to path. If path is empty, it is a
// no-op. The profile is written at the call site, not at program exit, so the
// caller should defer this after the work is complete.
func writeHeapProfile(path string) error {
	if path == "" {
		return nil
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("--memprofile: create %s: %w", path, err)
	}
	defer f.Close()
	if err := pprof.WriteHeapProfile(f); err != nil {
		return fmt.Errorf("--memprofile: write %s: %w", path, err)
	}
	return nil
}
