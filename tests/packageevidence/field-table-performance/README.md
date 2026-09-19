# Importer field-table experiment

This experiment reuses the native Syft importer's eight canonical field-name tables. It does not change output slice growth, JSON validation, matching, or the ordinary directory scan.

The clean importer CPU profile attributed 6.98% of sampled CPU to `canonicalFields`. Its repeated tag splitting allocated about 6.18 MiB per 20,000-package import. The isolated opportunity score was impact 2 × confidence 5 ÷ effort 2 = 5. Measurements and acceptance are recorded separately from this hypothesis.

The tables start empty and initialize on their first use. Each derives its names from the corresponding private Go struct, then becomes immutable. `strings.EqualFold` remains the alias predicate. The check still visits fields in struct order and rejects at the same validation stage. Named replacements for private anonymous structs preserve every field's name, order, type, and JSON tag; `shape_proof.go` compares the baseline and candidate Go ASTs recursively.

`strings.Cut(tag, ",")` returns the same prefix as the previous `strings.Split(tag, ",")[0]`. Unknown fields retain their previous handling. Reflection and allocation are absent from the package initialization path. `sync.Once` publishes each fixed table safely to concurrent imports. A rejected first alias can initialize more field names than it previously inspected, so cold malformed inputs are measured as well as warmed valid reports.

The contract helper compares complete serialized output hashes, error identity and text, nil reports on failure, and deterministic cancellation checkpoint counts. The corpus includes the original native Syft fixture bytes, empty reports, field aliases including Unicode case folding, unknown fields, malformed JSON, input and count limits, early and late invalid packages and relationships, reader errors, and cancellation. The AST proof, race tests, and differential fuzzing supplement these comparisons.

`compare.py` runs the cold helper in independent processes. `warm_compare.py` uses the unchanged benchmark command in `tests/packageevidence/bench`; each process performs one untimed validation import and one timed import. The warm helper checks complete status and package/relationship counts on each timed import. Full serialized-report equality is checked separately by the contract helper on the same importer source and inputs; it is not part of the warm timed loop. The cold helper serializes the full report after timing, while the warm helper serializes only measurement counters. Both retain three excluded warmup pairs followed by deterministic alternating pairs. All measurements use `GOMAXPROCS=2`. Import allocation counters exclude reading input and output serialization. Whole-process peak RSS includes these operations and, for the warm benchmark, both imports.

The rejected capacity experiment remains in `../import-performance/`. None of its production edits are part of this candidate.

## Result

Independent source, artifact, contract, and measurement checks accepted this change for its small allocation reduction. Twenty warmed pairs on macOS arm64 reduced median cumulative allocation from 408,029,856 to 401,309,496 bytes per 20,000-package import: **6.41 MiB, or 1.65%**. The median allocation count fell 4.15%. The local latency change (953.15 to 908.89 ms, 4.64%) remains within the experiment's 10% noise envelope, so this is not a demonstrated speedup claim.

All 45 contract cases matched complete output hashes, exact errors, and cancellation checkpoints. The 60-second differential fuzz campaign completed 206,182 comparisons without divergence. Unit tests, race tests, vet, concurrent first-use tests, and the eight-shape AST proof passed.

Cold malformed inputs disclose the initialization cost: the first document alias allocated 88 more bytes and the Unicode source alias allocated 32 more bytes. Other sampled tiny cases allocated less. Tiny latency differences were under 4% with overlapping distributions.

Initial single-sample adversarial RSS increases were followed with five pairs each, retaining the original samples. The repeated median RSS changes were -7.73% for the last invalid package, +0.054% for the first invalid relationship, and +0.270% for the last invalid relationship. Distributions overlapped. The candidate's last-relationship case nevertheless reached 227.66 MiB in one sample, versus the baseline's maximum 205.95 MiB. These results neither establish a reproducible RSS regression nor prove an RSS ceiling or reduction.

`results/summary.json` contains the decision and counts. Compressed receipts retain every measured sample, exact input hashes, binary hashes, and source hashes. Original CPU/heap profiles remain under `.cache/next-sprint/import-profile/` in the main checkout; use the clean `large-no-poll-cpu.pb.gz` capture. The earlier CPU capture was perturbed by heap sampling and is excluded from attribution.

## Reproduction

Build the contract helper and the existing warm benchmark command against the baseline and candidate importers separately, with the same Go toolchain and flags:

```sh
CGO_ENABLED=0 GOMAXPROCS=2 go build -mod=readonly -trimpath -buildvcs=false \
  -o /tmp/import-contract tests/packageevidence/field-table-performance/contract.go
CGO_ENABLED=0 GOMAXPROCS=2 go build -mod=readonly -trimpath -buildvcs=false \
  -o /tmp/import-warm ./tests/packageevidence/bench
```

`results/manifest.json` identifies the baseline commit and candidate source. `results/cases.json` records all contract inputs, including paths used for this run; recreate synthetic reports with the retained capacity experiment's `cases.py` and update paths without changing their hashes. The tiny input bytes are retained directly. Native and synthetic large fixture hashes refer to the unchanged earlier importer corpus.

Run `compare.py --help` for the cold contract/timing runner and `warm_compare.py --help` for the warm runner. Pass `--all-timing-cases` with a cases file containing only the tiny cases to measure cold initialization. The retained before binaries were reused only after verifying their exact importer source hashes. Receipt times describe the coordinated quiet windows; no repository code or Syft scanner was executed.
