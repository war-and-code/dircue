# Compatibility with dircue 0.3.0

This harness compares an actual released 0.3.0 executable with a candidate. Each pair runs against the same fixture paths and environment. Exit status, stdout and stderr must match byte for byte; the harness does not remove paths, reorder JSON or normalize newlines.

The initial macOS ARM64 run matched **209 of 209 cases**. It includes the existing 118-case CLI matrix, project declarations and limits introduced in 0.3.0, and the released structural worker across all 20 supported languages. This is evidence for the exercised cases, not a guarantee for every input, platform or future candidate.

The [integrated follow-up receipt](results/integrated-macos-arm64.json.gz) also
matched all 209 cases after Syft-import hardening and the new command-help and
validation changes. Each receipt identifies its own candidate binary.

From the repository root:

```sh
python3 tests/compatibility_next/run.py \
  --baseline /path/to/released-v0.3.0/dircue \
  --candidate /path/to/candidate/dircue \
  --worker /path/to/released-v0.3.0/dircue-structural-worker \
  --worker-sha256 dac0c16d4cd5d030d4fbb7b710e61f4ef3dc84e5cf9d9f14753cdc50a44323ba \
  --require-worker \
  --output tests/compatibility_next/results/fresh-run.json.gz

python3 tests/compatibility_next/verify.py \
  tests/compatibility_next/results/fresh-run.json.gz
python3 -m unittest discover -s tests/compatibility_next -p 'test_*.py'
```

The default baseline hash and worker hash shown above are for the published macOS ARM64 assets. On another platform, pass `--baseline-sha256` and the matching released worker hash after checking those artifacts. Python 3.10+, Git and compatible executables are required. No network, build tools, package restoration or repository code execution occurs during fixture analysis. Git is used to construct local test snapshots.

Omitting `--worker` runs 179 cases and records native parsing as untested. Use `--require-worker` when that omission should fail. Each invocation has a 60-second timeout; a timeout or failed reference assertion aborts the run and does not produce a passing receipt. Existing receipt files are never overwritten.

Receipts retain exact reference output and any differing candidate output, fixture hashes, executable hashes, source metadata and coverage counts. Identical candidate captures are represented by an explicit reference to their baseline capture. The verifier checks capture digests and recomputes match totals; it does not independently prove that a subprocess ran. No candidate output is accepted as a replacement golden.

See [coverage](COVERAGE.md), [differences](DISCREPANCIES.md) and [provenance](PROVENANCE.md). Help, version and newly available commands are captured separately and require review; they are not included among the 209 equality checks.
