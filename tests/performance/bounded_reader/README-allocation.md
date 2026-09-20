# Bounded-reader allocation attribution

`profile_allocations.py` compares an instrumented 0.6.0 source baseline with the bounded-reader candidate. It builds the CLI twice and runs three fresh XML profiling processes per variant. **These runs attribute allocated bytes and live heap; they do not measure production latency or process RSS.**

Run from the repository checkout after stopping source writers and other heavy local jobs:

```sh
python3 tests/performance/bounded_reader/profile_allocations.py \
  --xml-root /path/to/verified-xml-fixture \
  --output .cache/bounded-reader/allocation-raw \
  --public-output .cache/bounded-reader/allocation-export \
  --environment-note 'Describe hardware and remaining background activity.'
```

Both output directories must be new. Pass `--go-binary /path/to/installed/go` if the default `go` launcher is older than the version in go.mod; automatic toolchain downloads are disabled. The fixture must contain at least 1 GiB of regular XML files, without symlinks or other content. Its complete file hashes are checked before and after the experiment; actual CLI runs read bounded prefixes.

The baseline defaults to `bf211aeb0ba272bf5819dd326c805b68011dac7a`, the 0.6.0 release commit. Its overlay restores `pkg/scanner/scanner.go` and `git.go`, omits the new `read.go`, and replaces only `main.go` with the instrumentation template. The candidate overlays only `main.go`. Effective source differences must be exactly those three reader files. The script checks baseline repository compilation inputs against the selected revision, hashes effective Go/embedded/native source inputs and module metadata before and after building, and rechecks after profiling. Toolchain executables, the harness and the instrumentation template are hashed before execution and checked again at completion. Selected build settings are recorded. If the reader change has since been committed or other runtime changes exist, use a checkout containing only the baseline and that change; do not weaken the identity checks.

Both binaries report 0.6.0 for byte-exact comparison. They retain symbols and use `CGO_ENABLED=0`, `GOWORK=off`, `-trimpath`, and `-buildvcs=false`. Module selection is read-only, with `GOPROXY=off`, `GOTOOLCHAIN=local`, and `GOSUMDB=off`; the run uses only the installed toolchain and already available module files. They are instrumentation artifacts, not release binaries.

`profile_main.go.txt` sets the memory sampling rate after package initialization, captures counters immediately around `cli.Execute`, then forces GC and writes allocation/live-heap profiles. CLI stdout, stderr and exit status must match across all six runs. Instrumentation errors fail the run. The profiler omits production signal-handler setup equally for both versions.

The portable export contains raw per-process numeric samples, source/tool/input hashes, medians and allocation/heap stack tables. Full reports, absolute paths, overlays, executables and binary pprof files remain in the ignored raw directory. Three samples per lane cannot establish universal allocation or memory savings. Pair these results with separately measured, uninstrumented whole-workflow compatibility and performance evidence.

`profile_idle_gc.py` runs a separate retention diagnostic with the same arguments and fresh output directories. It records live heap after one and then two consecutive forced garbage collections, without intervening profile writes. Its heap stack tables describe the second collection; both sets of counters remain in each sample. This helps distinguish idle pool retention from long-lived allocations. Forced collections and full allocation sampling alter runtime behavior, so this diagnostic does not measure normal peak RSS or justify changing the application's GC policy. It preserves the original single-GC captures.

The helpers were hardened after these captures to check the complete pinned release tree before asking Go to select packages. Every release-tracked file must remain present in the effective baseline view; all non-test Go and native compilation-source files, including files excluded by platform or build tags, must match the release. Module files are checked too. Only the two restored reader call sites and removal of the newly added reader are permitted baseline overlays. The instrumentation `main.go` exception requires its separately supplied expected SHA-256. Selected embedded-file contents still undergo the package-input checks, while complete-tree presence checks catch deleted embedded blobs even when a wildcard would otherwise stop selecting them.

This strengthens future collection rather than retroactively changing the historical receipts. Their original harness hashes remain intact. Run the lightweight deletion, build-tag, embedded-file and overlay regressions without a Go compiler:

```sh
python3 tests/performance/bounded_reader/test_release_guard.py
```
