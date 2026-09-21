# Post-review 0.8 evidence

Final candidate source: `11a04cab3700f4f98162874144b500ad5a7041bb`. Binary SHA-256: `0d85c9beb82a46791d24a1b2d87bc1f46dc8d6e98395434aed1399180f524fd9`. Platform: darwin-arm64. The Go build manifest records the clean source state and compilation inputs.

`broad.json.gz` and `targeted.json.gz` contain 278 and 18 exact raw-output comparisons against the published 0.7.0 binary. `context.json`, `metrics.json`, and `native-local.json` cover the packaged context gate, six scc checks, and 21 structural fixtures across 20 languages. `linguist.json.gz` records 426 cases: 408 passed, 18 narrowly verified documented differences, zero unexpected failures. `go-race.txt` records the passing full race suite.

`performance.json.gz` retains all final samples, commands, content manifests and measured outputs. `preceding-performance.json.gz` and `preceding-build.json` retain the earlier review measurement before the versioned-SDK correction; they describe a different candidate and are not merged into the final sample population. See [validation](../../../../docs/releases/0.8.0-validation.md#post-review-candidate) for both outcomes and limits.

`verification.json` is a strict integrity check of the final build, raw compatibility records and performance summaries. It does not replay scans or authenticate execution. The published baseline and worker hashes are documented in the [harness instructions](../../README.md). `SHA256SUMS` detects changed retained files; it is not a signature. Cross-platform CI is recorded on [PR #64](https://github.com/war-and-code/dircue/pull/64).
