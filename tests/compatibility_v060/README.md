# 0.5.0 CLI compatibility

This harness compares a verified, published 0.5.0 executable with a candidate on identical temporary paths. It retains the 241 earlier compatibility cases and adds 37 declaration and saved-report comparison cases. Raw stdout, stderr and exit codes must match; help and version outputs are recorded separately.

The new cases cover standalone and aggregate declarations, concurrency and resource options, directory and local Git snapshots, identical and changed saved reports, partial coverage, and invalid input. Saved input reports are produced by the released reference, then passed unchanged to both executables. Repository scripts and package managers are never run.

Build a candidate with only its reported version overridden to `0.5.0`, and record compilation-input hashes before and after building. The harness requires that build receipt and verifies its executable hash. The receipt schema is `dircue-v060-benchmark-build-1`, retaining the earlier build-receipt fields `source_at_build`, `files`, and `candidate_sha256`.

```sh
python3 tests/compatibility_v060/run.py \
  --baseline /verified/v0.5.0/dircue \
  --candidate /candidate/dircue \
  --build-receipt /candidate/build.json \
  --worker /verified/v0.5.0/dircue-structural-worker \
  --worker-sha256 VERIFIED_WORKER_SHA256 \
  --output /fresh/path/compatibility.json.gz
python3 tests/compatibility_v060/verify.py /fresh/path/compatibility.json.gz
```

The default baseline digest identifies the published macOS ARM64 binary. For another platform, supply `--baseline-sha256` after independently verifying its release asset. Receipts retain source and fixture identities, raw captures, differences, and any untested native coverage. Existing outputs are never overwritten. Passing establishes compatibility for the recorded cases and binaries, not all possible inputs or later revisions.
