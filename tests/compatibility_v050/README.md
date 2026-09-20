# 0.4.0 CLI compatibility

The harness runs the actual released 0.4.0 executable and a candidate against the same temporary inputs. It checks raw stdout, stderr and process exits, including retained 0.2/0.3 cases and 0.4 discovery, registry, rule, graph, package-import and function options. Help and version differences are recorded separately. Native structural checks require the released worker and its independently verified digest.

Build the candidate with the [0.5 performance harness](../performance/v050_candidate/README.md). Its build command records the actual source inputs before and after compilation and binds them to the executable hash. Only the reported version is set to 0.4.0 to permit raw-output comparison.

```sh
python3 tests/compatibility_v050/run.py \
  --baseline /verified/v0.4.0/dircue \
  --candidate /candidate/dircue \
  --build-receipt /candidate/build.json \
  --worker /verified/v0.4.0/dircue-structural-worker \
  --worker-sha256 VERIFIED_WORKER_SHA256 \
  --output /fresh/path/compatibility.json.gz
```

The default baseline digest is for the released macOS ARM64 executable. Supply `--baseline-sha256` for a different independently verified platform asset. A receipt contains fixture identities, exact outputs or their binary representation, executable hashes, the historical build receipt, and any untested native coverage. It refuses existing output paths and checks binaries and fixtures again after execution.

Passing this matrix establishes compatibility on its recorded cases. It is not proof of every possible invocation, input, platform or later source revision.
