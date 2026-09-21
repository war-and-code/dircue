# 0.7 focused profiling evidence harness

[Retained results](results/) and the [candidate validation report](../../docs/releases/0.7.0-validation.md) record the measured 0.7.0 candidate. Run `verify.py` on each compressed result to recompute its claims.

This harness tests focused profiling through the public CLI without executing inspected repository code. It separates three claims:

1. selected inherited 0.6.1 commands have identical raw stdout, stderr and exit status;
2. authored .NET and Python/uv fixtures produce the independently specified primary, context and related populations;
3. each focused scc file record equals the same path in a full-root scc report, and focused totals recompute from those file records.

The conformance runner requires the pinned 0.6.1 executable, a candidate built by `build.py`, and a fresh receipt path. Native hotspot compatibility is added only when a separately verified structural worker is supplied.

```sh
python3 tests/focus_v070/build.py \
  --candidate .cache/v070-sprint/harness/dircue \
  --build-receipt .cache/v070-sprint/harness/build.json

python3 tests/focus_v070/run.py \
  --baseline .cache/v070-sprint/baseline/dircue \
  --candidate .cache/v070-sprint/harness/dircue \
  --build-receipt .cache/v070-sprint/harness/build.json \
  --output .cache/v070-sprint/harness/conformance.json.gz

python3 tests/focus_v070/verify.py \
  .cache/v070-sprint/harness/conformance.json.gz
```

The selected focus receipt complements, rather than replaces, the inherited compatibility gate. Run all 278 inherited raw cases with the actual 0.6.1 release and verified release worker:

```sh
python3 tests/focus_v070/broad.py \
  --baseline .cache/v070-sprint/baseline/dircue \
  --candidate .cache/v070-sprint/harness/dircue \
  --build-receipt .cache/v070-sprint/harness/build.json \
  --worker /verified/dircue-structural-worker \
  --worker-sha256 VERIFIED_SHA256 \
  --output .cache/v070-sprint/harness/broad-compatibility.json.gz
```

The build helper records every local Go compilation input, the worktree commit/tree/status/diff digests, the exact command and tool identity. It refuses to retain a candidate if those identities change during the build. The conformance runner refuses to overwrite evidence and hashes fixture contents before and after execution.

For a complete workflow measurement on a meaningful local monorepo:

```sh
python3 tests/focus_v070/performance.py \
  --baseline .cache/v070-sprint/baseline/dircue \
  --candidate .cache/v070-sprint/harness/dircue \
  --build-receipt .cache/v070-sprint/harness/build.json \
  --root .cache/corpus/roslyn \
  --project src/Compilers/CSharp/Portable/Microsoft.CodeAnalysis.CSharp.csproj \
  --environment-note 'Quiet local macOS ARM64 host; ordinary desktop services remained active.' \
  --output .cache/v070-sprint/harness/performance.json.gz
```

Create the exact synthetic uv workspace for a second, bounded workflow measurement with:

```sh
python3 tests/focus_v070/fixture.py \
  --output .cache/v070-sprint/harness/performance-fixtures

python3 tests/focus_v070/performance.py \
  --baseline .cache/v070-sprint/baseline/dircue \
  --candidate .cache/v070-sprint/harness/dircue \
  --build-receipt .cache/v070-sprint/harness/build.json \
  --root .cache/v070-sprint/harness/performance-fixtures/python \
  --project packages/api/pyproject.toml \
  --environment-note 'Quiet local macOS ARM64 host; synthetic bytes are recorded.' \
  --output .cache/v070-sprint/harness/performance-uv.json.gz
```

Performance lanes cover the released and candidate default path, focus planning, focus plus primary metrics, and full-root metrics. Every invocation is a fresh CLI process. Timings include inventory traversal, declaration prepass, selection, metrics where requested, and JSON serialization. Inputs are fully content-hashed before and after the run. Results describe only the recorded machine, executable and input manifest.

Use `verify.py` for conformance or performance receipts. It recomputes capture equality or sample summaries and derived percentages, and rejects receipts produced by different harness source.

Run harness self-tests with:

```sh
python3 -m unittest tests/focus_v070/test_harness.py
```
