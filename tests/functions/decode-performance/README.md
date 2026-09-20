# Function-response decoding profile

Scenario: validate and decode a real BCA worker response containing up to 128
retained function-space entries. Inputs were generated once with the pinned
worker; its binary hash and every input hash are recorded in `inputs.json`.
The source fixtures contain 1, 10, 50, 100, 128, 500 or 1,000 straight-line Python
functions. Responses above 128 explicitly report the omitted population.

The benchmark measures the Go response boundary only. It excludes subprocess
startup, tree-sitter/BCA execution, filesystem traversal and CLI output formatting.
The default and opt-in variants use their respective actual wire responses.
Loading/decompressing inputs occurs outside the timed loop. Every response is
validated before measuring, and an error in any measured decode fails the run.

Success means preserving accepted and rejected wire contracts and identical
decoded values while reducing measured CPU time or allocations on a demonstrated
hot path. No runtime optimization is justified merely by a large report size.

Build an optimized, symbolized benchmark binary:

```sh
go test -c -o /tmp/structure-profile.test ./pkg/structure
/tmp/structure-profile.test -test.run '^$' -test.bench '^BenchmarkFunctionResponse$' \
  -test.benchmem -test.benchtime 100ms -test.count 20 -test.cpu 1
```

Run the test binary from `pkg/structure`, so it can find the retained inputs.
`BenchmarkFunctionStages` separately measures the envelope validation and
standalone function-block decoder. Stage costs overlap the full boundary; do not
add a stage value to the full-boundary value.

CPU and allocation attribution use Go's sampling profiler on the optimized
128-entry benchmark. Profiles, raw benchmark outputs, host/toolchain/source
fingerprints and an explicit hypothesis ledger are retained. No kernel, power
or governor settings are changed. Benchmark percentiles describe repeated
batch-average measurements, not production request-latency percentiles. The
sample count is too small to estimate extreme tails.
