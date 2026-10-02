# Compatibility with dircue 0.3.0

This harness compares an actual released 0.3.0 executable with a candidate. Each pair runs against the same fixture paths and environment. Exit status, stdout and stderr must match byte for byte; the harness does not remove paths, reorder JSON or normalize newlines.

The initial macOS ARM64 run matched **209 of 209 cases**. It includes the existing 118-case CLI matrix, project declarations and limits introduced in 0.3.0, and the released structural worker across all 20 supported languages. This is evidence for the exercised cases, not a guarantee for every input, platform or future candidate.

The [integrated follow-up receipt](results/integrated-macos-arm64.json.gz) also matched all 209 cases after Syft-import hardening and the new command-help and validation changes. The [function-integration receipt](results/functions-macos-arm64.json.gz) matched the same 209 cases after adding optional function metrics and protocol hardening. Each receipt identifies its own candidate binary.

The [rules and decoder-optimization receipt](results/rules-optimized-macos-arm64.json.gz) matched all 209 cases using the released worker. Its [build provenance](results/rules-optimized-build-inputs.json.gz) records 271 local Go and embedded inputs, checked before and after verification. This comparison build deliberately reports version `0.3.0`; its source default is `0.4.0-dev`, and it is not a v0.3.0 release artifact.

A separate [combined-module smoke check](results/rules-optimized-native-smoke.json.gz) uses the updated function-capable worker on three small Java/C#/Python files. Adding rules preserved the other module results, including 22 function spaces and one parse per source. Combined rule results matched standalone analysis. That check covers the stated fixtures, not every combination of optional modules.

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

## Comparing later minor releases

`minor.py` reuses the inherited matrix against an explicitly supplied release binary. Both its SHA-256 and reported release version must match. It checks 245 cases without the native structural worker, or 278 with a separately pinned worker. A missing worker is recorded as untested; `--require-worker` makes that omission an error. Each receipt contains raw stdout/stderr and exit statuses, fixture and executable hashes, and helper hashes; existing receipts cannot be overwritten.

```sh
python3 tests/compatibility_next/minor.py \
  --baseline /path/to/released/dircue \
  --baseline-sha256 VERIFIED_EXECUTABLE_SHA256 \
  --baseline-version 1.0.1 \
  --candidate /path/to/candidate/dircue \
  --output /path/to/new-receipt.json
```

The executable hash is distinct from the downloaded archive hash. Verify the archive against its release checksums before extracting the executable. These checks do not cover every newer map, focus or context interface. Those have separate semantic, schema and integration gates. Help/version text, new option catalog entries and intended improvements in detected evidence require review; passing the matrix alone is not sufficient to approve a release.

### Historical 1.1.0 candidate evidence

The [published-1.0.1 comparison](https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/v110-review-original-v110-released-v101-macos-arm64.json.gz) matches **278 of 278** inherited cases on macOS ARM64, including the native structural worker. The [build receipt](https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/v110-review-original-v110-build-inputs-macos-arm64.json.gz) binds the candidate executable to clean source commit `a205c3bdafd3e41d6214fd94fbd4586c335a8616` and 399 local Go/embedded inputs. These historical receipts predate the adversarial-review fixes; they do not validate the corrected candidate. They are [archived release assets](../receipts/evidence-archive.json), restored to ignored paths by `make fetch-receipts`. Command metadata uses `{workspace}` in place of host workspace prefixes; raw captures and their hashes are unchanged.

The baseline is the executable extracted from the [published 1.0.1 release](https://github.com/war-and-code/dircue/releases/tag/v1.0.1), not a source rebuild or a candidate stamped with an older version. Its archive was verified against the release checksums before extraction.

| Input | SHA-256 |
| --- | --- |
| Published Darwin ARM64 core archive | `f9d4c74db7b6d13027fefe91d49d64597cd045e7b5f634326a1e8a610bac4eab` |
| Extracted core executable | `b8bf24a26a078b5fe073890e6882647254eba482ffd9fb4398e5f38ff5008150` |
| Published Darwin ARM64 worker archive | `8f3f5698c90282c7b00ad819fed42345681ed969088a71f9899a724bcf87e25a` |
| Extracted worker executable | `73cc5ed95a5b81dcc97c91d57b99a0db4e71ff45bdb74ce8a8193e60e3d53740` |
| Candidate executable | `227d5d3540ce470c3eac840865ec385b5b374ec64bfab16a446e7875c283cf83` |

The candidate's source default reports `1.1.0-dev`; version/help text is outside the equality matrix. The case groups are 118 retained CLI cases, 55 project cases, 6 structural validation cases, 30 native structural cases, 32 optional module cases and 37 declaration cases. New map semantics, limits and opt-in exit statuses have separate tests. This result does not establish untested platform compatibility or freeze every byte on newly supported inputs.

The same source commit also passed the [13-check offline native wheel smoke](https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/v110-review-original-v110-native-wheel-macos-arm64.json.gz) and a [nine-case alias comparison](https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/v110-review-original-v110-native-alias-macos-arm64.json.gz) between the archive's `dirq` symlink and the installed wheel's `dirq` launcher, including comparison exits 0, 1 and 2. The [native build provenance](https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/v110-review-original-v110-native-build-macos-arm64.json.gz) records the clean source and release build flags. These locally built artifacts use the test version `1.1.0-rc.1`; no public tag or publication is implied.

### Corrected 1.1.0 candidate validation

The [corrected compatibility receipt](https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/v110-review-corrected-compatibility.json.gz) compares the published 1.0.1 macOS ARM64 binary against the corrected candidate and matches **278 of 278 cases**, including all 30 native-worker cases. It records no untested cases. The candidate executable SHA-256 is `45631f32ca09ccbae18c8c89b115cd1a09375f1fd3308c558bfc393aa6b5d508`; the [build receipt](https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/v110-review-corrected-build.json.gz) binds it to clean source commit `063cf27380bc3bd9bcd35ca5ffdabb76add00e8f` and 399 local Go and embedded compilation inputs. A later [source-binding check](https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/v110-review-corrected-source-binding.json.gz) rebuilt the same executable byte for byte at validation checkout `bd6a7dafb5fd70a31ea3f1f5e18d96c98975658c`; the changes since the measured build were tests, documentation and hand-authored expectation files.

The separate [legacy-corpus receipt](https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/v110-review-corrected-legacy-corpus.json.gz) records **58 of 58 byte-identical results** for `--json` and `analyze all` across 29 cached repositories, with 232 raw stdout/stderr streams. For this privacy-checked run, the harness used relative cache input paths and a `/tmp/dircue-workspace-alias` symlink as `PWD`; it did not rewrite raw output. All 232 archived streams are free of the user's home path. The earlier direct-path capture remains local-only and is not included in the archive. The [raw capture archive](https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/v110-review-corrected-raw-captures.tar.gz) contains the preserved streams.

CI completed in two runs on nearby revisions. [Cross-platform CI and conformance](https://github.com/war-and-code/dircue/actions/runs/37078118324) passed at `bd6a7dafb5fd70a31ea3f1f5e18d96c98975658c`. [Native release smoke](https://github.com/war-and-code/dircue/actions/runs/37076693860) passed core, wheel and worker checks on five platforms at `063cf27380bc3bd9bcd35ca5ffdabb76add00e8f`. These runs validate the production source at those commits; the later changes were limited to tests, docs and expectation data, and the source-binding receipt confirms that they did not alter the built executable's compilation inputs.
