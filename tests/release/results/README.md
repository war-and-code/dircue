# Packaged function checks

These local Darwin arm64 receipts describe actual core and native-worker archives built from clean commit `58ed625`, using the validation version `0.4.0-rc.1`. No tag or release was created. The matching core and worker hashes appear in each receipt.

- `functions-darwin-arm64-breadth.json`: 21 fixtures across 20 languages, using extracted executables.
- `functions-darwin-arm64-functions.json`: optional function metrics, direct native parity, source hashes, known Java/C#/Python spans, scheduling determinism, bounds and scope handling.
- `functions-darwin-arm64-functions-with-old-worker.json`: the same checks plus explicit refusal by the actual 0.3.0 worker, with empty stdout and an update instruction.
- `functions-v030-darwin-arm64.json`: prior-version compatibility using the actual 0.3.0 archives without requiring a new flag.

The initial helper incorrectly treated generated-file scope exclusions as incomplete parsing. Two attempts failed that assertion. Source inspection showed that `outside_scope` omissions can coexist with complete coverage of selected files. The corrected helper checks exclusions separately from a malformed-Python fixture, which does require partial parsing coverage. Production code did not change. The first raw failure log was overwritten; its error text was captured in tool output. The second failure and final local run logs are retained in the ignored working evidence directory.

These receipts are test records, not signed attestations. They do not establish native execution on the other four platforms or a GitHub upload/download rehearsal. `scripts/function_release_smoke.py` documents the executable check; `tests/release/test_function_smoke.py` checks receipt rejection rules separately.
