# Import capacity experiment

This candidate was rejected. On the 20,000-package input it reduced cumulative
allocation by 5.55%, but a hard-limit malformed package report repeatedly used
more peak memory: a median 73.72 MiB before versus 84.17 MiB after (+14.18%).
The five candidate RSS samples did not overlap the five baseline samples. The
1.39% median latency difference on valid input remains inside the timing-noise
envelope and is not a speedup claim.

`results/summary.json` records the decision and source/binary hashes. Compressed
receipts retain every valid, malformed, cancellation and repeated RSS sample.
The rejected helper, append-site patch, fuzz log and validation logs remain in
`results/` so the experiment can be reviewed without adopting its code.

This harness compares the native Syft JSON importer with an earlier revision. It reads existing reports and generated bounded JSON. It never runs Syft, a package manager, or repository code.

The candidate changes only allocation of the final package and relationship slices. It grows them after each row has passed the existing validation and only when the current slice is full. Capacity doubles, capped at the validated native row count. It does not reserve the whole advertised count before processing rows.

`cases.py` generates invalid-first, invalid-second, invalid-middle, invalid-last and growth-boundary cases at default and hard package/relationship count limits, plus tiny malformed rows, duplicate relationships, cancellation, reader errors, and existing valid reports. The location supplied with `--root` must also contain the retained synthetic 1,000/20,000-package inputs from `tests/packageevidence/benchmark.py --prepare`.

```sh
python3 tests/packageevidence/import-performance/cases.py \
  --root /path/to/dircue \
  --output .cache/import-capacity/cases

CGO_ENABLED=0 GOMAXPROCS=2 go build -mod=readonly -trimpath -buildvcs=false \
  -o .cache/import-capacity/after tests/packageevidence/import-performance/contract.go
```

Build the same `contract.go` against the chosen earlier source revision to obtain `before`. Keep the helper byte-identical. The build tag excludes this profiling program from normal package builds.

```sh
python3 tests/packageevidence/import-performance/compare.py \
  --before /path/to/before --after .cache/import-capacity/after \
  --cases .cache/import-capacity/cases/cases.json \
  --output .cache/import-capacity/contracts --mode contracts

python3 tests/packageevidence/import-performance/compare.py \
  --before /path/to/before --after .cache/import-capacity/after \
  --cases .cache/import-capacity/cases/cases.json \
  --output .cache/import-capacity/timings --mode timings --pairs 20
```

Timings require a quiet machine. The helper times only `Import` and records its cumulative allocated bytes. `/usr/bin/time -l` records process RSS on macOS, including input reading and output serialization. A single contract comparison does not establish a latency result. Timings run three warmups and 20 shuffled alternating pairs for each valid case. Every pair checks complete serialized-report SHA256, error identity/text, report absence on failure, and context-check count.

Differential fuzzing uses a temporary copy of the earlier package, read with `git show` from an explicit local revision. It adds no baseline implementation to production code:

```sh
python3 tests/packageevidence/import-performance/prepare_fuzz.py --baseline BASELINE_REVISION
GOMAXPROCS=2 go test -overlay .cache/import-capacity/overlay.json \
  ./pkg/packageevidence -run '^$' -fuzz '^FuzzCapacityDifferential$' \
  -fuzztime=60s -parallel=2
```

The fuzz corpus exercises arbitrary report bytes and cancellation at deterministic context checkpoints. Existing package tests remain responsible for the broader importer and attribution contracts. The capacity tests assert preserved prefixes, nested value identity, bounded capacities, and empty-array serialization.

A doubled capacity can retain more unused space than Go's usual growth factor at some intermediate prefixes. That is the specific negative-input tradeoff this experiment measures; lower cumulative allocation on a valid large report alone is insufficient for acceptance.
