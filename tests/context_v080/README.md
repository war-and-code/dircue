# 0.8 context evidence harness

This directory contains source-bound validation drivers for the 0.8 environment, offline planning, capability, and saved-report comparison work. No retained result is implied by the harness source. Each runner writes a fresh receipt that identifies the exact candidate, build inputs, fixtures, commands, and raw outputs it observed.

The evidence layers are deliberately separate:

- `broad.py` preserves the inherited 278 raw CLI cases against the pinned 0.7.0 release.
- `targeted.py` preserves 18 released focus, availability, and explanation cases against the same release.
- `scripts/context_release_smoke.py` is the packaged-release gate for 13 explicit 0.8 contracts on every platform.
- existing Go package and schema tests exercise internal bounds, validation, cancellation, source semantics, and comparison qualification.
- `performance.py` measures named workflows without treating their different outputs as equivalent.

Build a candidate at fresh, source-bound paths:

```sh
python3 tests/context_v080/build.py \
  --candidate .cache/context-v080/dircue \
  --build-receipt .cache/context-v080/build.json
```

Run inherited compatibility after supplying the verified release worker required by the inherited native cases:

```sh
python3 tests/context_v080/broad.py \
  --baseline .cache/release070/installed/dircue \
  --candidate .cache/context-v080/dircue \
  --build-receipt .cache/context-v080/build.json \
  --worker /verified/dircue-structural-worker \
  --worker-sha256 VERIFIED_SHA256 \
  --output .cache/context-v080/broad.json.gz

python3 tests/context_v080/targeted.py \
  --baseline .cache/release070/installed/dircue \
  --candidate .cache/context-v080/dircue \
  --build-receipt .cache/context-v080/build.json \
  --output .cache/context-v080/targeted.json.gz
```

Run performance measurements only on a quiet host. The ASP.NET Core corpus must already exist and is fully content-hashed before and after measurement:

```sh
python3 tests/context_v080/performance.py \
  --candidate .cache/context-v080/dircue \
  --build-receipt .cache/context-v080/build.json \
  --corpus .cache/corpus/aspnetcore \
  --environment-note 'Quiet local host; ordinary desktop services remained active.' \
  --output .cache/context-v080/performance.json.gz
```

The performance driver performs one warmup and at least five measured samples per lane, reverses lane order on alternating repetitions, records wall time and peak RSS, and retains exact commands and output hashes. Its authored large-input case materializes 2,048 valid XML files of exactly 1 MiB each (2 GiB total) beside a tiny project; fixture creation and full content hashing occur outside timed regions. Staged discovery → saved-plan → declarations and unconditional all/declarations/metrics produce different evidence. Their ratio is observational and is never labeled an equivalent-output saving.

The pinned baseline SHA-256 is `7bbc81abfc2d9abec4a997c21933492d39959f444560a6d6d631121f499933ad`. The candidate build receipt binds all local Go compilation inputs plus worktree commit, tree, status, and diff digests.
