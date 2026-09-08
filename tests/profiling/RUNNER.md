# Fresh-process scanner runner

`run.py` launches scanner measurements against existing inputs. It performs no builds and executes no repository content. Run it after other CLI/library timing work ends, inside one Linux container with the scanner test binary and both corpus volumes mounted read-only. Mount a fresh writable artifact parent separately. Network can remain disabled.

Required receipts are small JSON files identifying the scanner test build, host, original Git corpus including object storage, and verified raw-object flat view. Use the flat view's compact summary, which references the digest of its 48 MiB inventory. Input verification must finish before measurement; the runner hashes receipts and the binary but does not preload or reverify source files. Retain the independent reference comparison alongside these receipts. Stable scan digests establish repeatability; the reference comparison checks correctness.

The default scenario order is shuffled within each seeded round: ripgrep and jq directory scans with one worker, and Roslyn Git scans with five workers. Each scenario gets three excluded warmup *processes* followed by twenty measured fresh processes. All calls use `first-scan`; no engine cache persists between processes. Optional repeated `--scenario` selects these or Roslyn Git with one worker and Spring Git with one/five workers. `--gomaxprocs=5` fixes Go scheduler parallelism independently of scanner workers, matching the existing receipt's five online Docker VM CPUs. Verify that receipt still applies before running.

Example inside the prepared container, with paths adapted to actual mounts:

```sh
python3 /harness/profiling/run.py \
  --binary /build/scanner.test --output /results/scanner-baseline \
  --git-root /corpus --flat-root /flat \
  --git-receipt /receipts/git.json --flat-receipt /receipts/flat-summary.json \
  --build-receipt /receipts/scanner-build.json --host-receipt /receipts/host.json
```

The executable must be a normally optimized, unstripped `go test -c` binary. `--time-binary` must point to GNU time; its default `/usr/bin/time` records user/system CPU, coarse wall time, and maximum RSS in KiB. Python also records process elapsed wall time around spawn and blocking `os.wait4`, with a watchdog Timer for timeout enforcement. There is no timeout backoff polling. Those metrics include package initialization, benchmark setup, validation, receipt hashing and test runtime shutdown. The fingerprint's `measured_elapsed_ns` isolates the first `Scan` plus its pprof label wrapper. Neither metric implies cold filesystem caches. GOGC is fixed at 100, GOMEMLIMIT at off; inherited GODEBUG, kernel, architecture, Python version, affinity, timestamps and before/after load averages are recorded. All artifacts and commands are retained; failures leave `partial.json` and terminate the current process group on timeout.

Baseline summaries retain every sample and report medians, empirical nearest-rank p95, minimum/maximum, population coefficient of variation and cumulative measured seconds. They separately flag scan and process totals below ten seconds; that is an advisory, not grounds to drop observations or claim an adequate duration. Use an explicitly selected `--runs=100` for small scenarios if longer observation is warranted. Every selected scenario receives the same configured count. Results apply only to the recorded scenarios and environment, with no universal speed claim.

After a complete baseline, use the same arguments and unchanged receipts/binary, a new `--output`, `--stage cpu` (then separately `mem` and `mutex`), and `--baseline /results/scanner-baseline/result.json`. Each profiling stage uses one fresh process per selected scenario and enforces baseline digests. Profiles never mix with accepted baseline timing samples. Mutex uses sampling fraction 1; memory uses Go's default allocation sampling. Short first-scan CPU profiles can have few samples: report that limit and obtain additional independent profiles or a separately specified warm scenario before ranking uncertain costs.

For CPU inspection, verify labels with `go tool pprof -tags`, then use `-tagfocus='phase=scan'`. Allocation profiles are cumulative and do not support phase filtering: distinguish retained model memory, package/setup allocations, final JSON receipt/digest construction and scanner allocation stacks. A first-scan profile may include `sync.Once` classifier loading; a warm scan removes that lazy initialization only if warmup actually reaches the classifier. Git storage/cache objects are rebuilt on every Scan even in warm mode. Mutex profiles can distinguish classifier initialization waits from the Git packed-object mutex; do not remove either lock based solely on a high cumulative wait count.

`BenchmarkCountLines` in `internal/cli/file_benchmark_test.go` is a separate standalone-file metadata experiment. Repository Scan never calls `countLines`, so scanner profiles cannot establish its contribution to repository latency. Its fixed LF/CRLF/CR inputs cover 16 KiB, 128 KiB, and 1 MiB; they are microbenchmarks, not public-corpus end-to-end measurements.
