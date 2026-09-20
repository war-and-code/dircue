# 0.5 candidate performance checks

This harness compares released 0.4.0 with a locally built candidate on the existing language-only and default aggregate commands. It separately measures the candidate with optional declaration analysis. It does not claim that the new analysis is free, or that any measured improvement holds across all repositories and machines.

Each corpus runs six lanes:

- Released and candidate `--json` language reports.
- Released and candidate `analyze all --json` reports, with no new modules enabled.
- Candidate `analyze all --declarations --json`.
- Candidate `analyze declarations --json`.

Existing-command output must match byte for byte. The combined declaration report must preserve the ordinary aggregate observations, apart from adding `declarations` and selecting its schema version. Standalone and combined declaration observations must match. Every repeated execution must produce identical output. A partial declaration report is retained with its coverage; it does not become a complete report merely because timing succeeded.

## Build identity

Run the build step while other agents and editors have paused source changes. It builds only dircue, using the release optimization flags, `CGO_ENABLED=0`, and a reported version of `0.4.0` for compatibility comparison. This is a measurement executable, not the release artifact.

```sh
python3 tests/performance/v050_candidate/benchmark.py build \
  --candidate .cache/v050-performance/dircue \
  --build-receipt .cache/v050-performance/build.json
```

The receipt binds the executable SHA256 to the command, Go build information, HEAD commit/tree, dirty-worktree fingerprints, and hashes of the local Go/module/embedded inputs. Input paths, contents, and worktree state are checked again after compilation; changes invalidate the build. The later measurement reads this historical receipt and verifies the executable hash. It does not relabel an older executable with the source state at measurement time.

## Corpus and measurement

Prepare the public checkouts using the existing [corpus tooling](../README.md). `--corpus-root` selects pinned Cobra, Express, Flask, ripgrep, Roslyn, and Spring Framework checkouts, covering Go, npm, Python, Cargo, .NET, and Java. All six run against their committed Git trees; Roslyn also runs in directory mode. The directories must be clean at preparation time. No corpus build, installation, or repository script is invoked.

The optional `--xml-root` selects an existing directory containing at least 1 GiB of XML file bytes and no supported declaration manifests. The existing `.cache/staged-analysis/fixtures/xml-only` fixture contains 2 GiB of written XML. The harness hashes actual file bytes before and after measurement; it does not create sparse substitutes. The declaration report must record zero candidate and parsed manifests. The current declaration schema has no content-byte counter, so the receipt does not claim a measured I/O byte total. This measures enumeration of a data-heavy directory, not parsing its XML content.

```sh
python3 tests/performance/v050_candidate/benchmark.py measure \
  --baseline .cache/v050-sprint/baseline/dircue \
  --candidate .cache/v050-performance/dircue \
  --build-receipt .cache/v050-performance/build.json \
  --corpus-root .cache/corpus \
  --xml-root .cache/staged-analysis/fixtures/xml-only \
  --output .cache/v050-performance/results \
  --environment-note 'Record actual host activity and resource constraints.'
```

Additional or smaller selections use repeatable `--case NAME:SOURCE:PATH`, where `SOURCE` is `git` or `directory`. For example, `--case uv-oracle:directory:pkg/declarations/testdata/python/uv-0.12.17/basic` checks the synthetic Python workspace separately from Flask. Explicit cases record their actual inventory identity; they do not implicitly claim a public-project pin. An output directory must not exist, preventing accidental replacement of historical evidence.

The default baseline hash is the downloaded macOS ARM64 0.4.0 binary, `0d4fd9167d11b590c32713a3ec2da2b3628dc72d12bc8fd58fd6873624af5d91`. Other platforms require `--baseline-sha256` with the independently verified release binary hash. The harness supports macOS `/usr/bin/time -l` and Linux GNU `/usr/bin/time`; Linux KiB values are converted to bytes. It does not offer Windows RSS measurement.

Each lane receives one warmup and five measured repetitions by default. Lane order reverses every round, keeping each baseline/candidate pair adjacent. All lanes use the same explicit source mode, eight workers, tree limit, and warmed filesystem caches. `--workers`, `--warmups`, `--repetitions`, and `--timeout` are recorded; fewer than five repetitions or one warmup are rejected. Timed-out process groups are terminated.

Wall time includes process launch and the `time` wrapper. It excludes report verification, JSON decoding, receipt compression, and corpus hashing. Peak RSS is the CLI process measurement, not Python harness memory. The harness does not evict caches or impose CPU/RAM limits. Pause concurrent builds, tests, and benchmarks before running, and describe remaining host activity accurately.

The fresh output directory retains compressed outputs, every timing/resource sample, execution order, per-case receipts, medians/ranges, paired changes, declaration coverage, and the final `receipt.json`. Input and executable identities are rechecked at the end. A failed run may leave diagnostic per-case artifacts but never a final receipt with `passed: true`.

Results should distinguish existing-path regressions, optional combined-mode overhead, and standalone declaration cost. A faster standalone declaration command performs different work from language profiling and is not a replacement benchmark. One host and a small corpus provide bounded evidence; broader speed or memory claims need additional measurements.
