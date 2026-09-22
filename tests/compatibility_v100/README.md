# 1.0.0 preparation compatibility checks

These checks compare the candidate against the published **0.8.0 darwin-arm64**
executable, identified by a fixed SHA-256. They supplement the Go tests and the
historical Linguist conformance corpus; they are not a new comparison with a
different Linguist release.

`build.py` creates an optimized, unstripped comparison binary and a receipt of
its compilation inputs. Its explicit **0.8.0 version override** isolates
behavior from version metadata. This binary is for comparison, not release
distribution. The build fails if compilation inputs change while it runs.

`run.py` inherits 278 cases from the earlier compatibility corpus, including
the separate native worker. Successful outputs are compared byte for byte.
Twelve intentional diagnostic improvements are recorded by exact case ID,
old stderr, new stderr and reason in `expected-diagnostics.json`; all retain
exit 1 and empty stdout. An unused exception fails the check.

`context.py` adds 41 focus, environment, planner, capability and saved-report
cases. It expects 39 exact matches and two explicit correctness fixes: the
released binary rejects its own partial .NET/Python focused metrics reports,
while the candidate must explain the requested project successfully. The Go
schema tests separately verify complete producer/validator/reader round trips
and reject malformed markers. These fixes are not counted as byte equivalence.

Example commands, from the repository root, with separately verified release
binaries supplied by the caller:

```sh
python3 tests/compatibility_v100/build.py \
  --candidate .cache/v100/check/dircue \
  --build-receipt .cache/v100/check/build.json

python3 tests/compatibility_v100/run.py \
  --baseline /path/to/verified-0.8.0/dircue \
  --candidate .cache/v100/check/dircue \
  --build-receipt .cache/v100/check/build.json \
  --worker /path/to/verified-0.8.0/dircue-structural-worker \
  --worker-sha256 73cc5ed95a5b81dcc97c91d57b99a0db4e71ff45bdb74ce8a8193e60e3d53740 \
  --expected-diagnostics tests/compatibility_v100/expected-diagnostics.json \
  --output .cache/v100/check/compatibility.json.gz

python3 tests/compatibility_v100/context.py \
  --baseline /path/to/verified-0.8.0/dircue \
  --candidate .cache/v100/check/dircue \
  --build-receipt .cache/v100/check/build.json \
  --output .cache/v100/check/context.json.gz
```

Raw stdout, stderr and status are retained in the result receipts. The fixed
baseline checksum intentionally makes this a platform-specific release
comparison, not a portable test that silently substitutes another binary.

The retained run in `results/` passes all 319 cases: 305 exact matches, 12
reviewed diagnostic improvements and two saved-report fixes. `checksums.json`
binds the receipts. `full-race-final.log` records the passing local race suite;
`results/local-validation.json` identifies the separate `1.0.0-dev` review
binary and the remaining multi-platform release gates.

`integration_timing.py` checks the full candidate against the isolated
cache-only candidate, not against a differently configured release binary.
For that experiment, build with `--cgo-enabled 1` to match the isolated
profiling build; the harness requires matching Go build settings. Three warmup
pairs and 20 measured pairs on Spring language analysis and XML language/optional
analysis yielded paired time ratios 0.995, 1.008 and 0.994. Median peak RSS was
149.17/50.56/78.05 MiB versus 150.61/51.21/80.52 MiB respectively. These controls
show no material timing regression from integration in this window. They do
not establish zero overhead or a general memory improvement.

The integration timing receipt identifies commit `2bcd134`. Later schema-only
review corrections constrain invalid language-statistics values; they do not
change scanner execution. The final compatibility build is recorded separately.
