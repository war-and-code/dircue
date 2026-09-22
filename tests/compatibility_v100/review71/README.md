# PR #71 follow-up review

This evidence covers the integrated runtime at `2c093c26caad6c8f42ec947bb525da1454f4f481`, including the wheel packaging preparation merged from `f94bf3cd170a6c05c3940ea09a17478b9fc3d940`. It is preparation for review, not a release approval. Source hashes and build commands are in [build.json](build.json); the comparison executable deliberately reports version 0.8.0 and must not be distributed.

## Kept and corrected

The review retains broader explicit `--on-error continue` support, CLI spelling and value hints, saved-report guidance, SIGPIPE documentation, relevant .NET `global.json` selection, JSONC diagnostics, seven new fuzz targets, and installation and compatibility documentation.

Follow-up fixes preserve fatal cancellation, distinguish worker startup failures from actual process crashes, cache failed environment selections consistently, avoid including worker stderr in successful partial reports, and deduplicate omission warnings with constant-time path lookups. Regression tests cover each distinction. Public contribution guidance again accepts Issues but declines outside pull requests; security guidance contains no promised response times or invented contact address.

## Removed

- The single-pass directory proposal changed tree-limit error precedence and could start expensive content analysis before rejecting an oversized tree. The existing preflight remains.
- Removing pre-open `Lstat` let a replacement symlink to an in-root regular file be read on macOS. Descriptor type checks had masked this in the proposed FIFO test. The prior check remains, with a portable regression; this is not a claim of atomic snapshot isolation.
- Rejecting nonpositive `--tree-size` broke Linguist-compatible behavior. The prior clamp and single-file behavior remain, without new behavior-change exceptions in the compatibility harness.
- Adding `views` to default capabilities JSON violated its closed schema. Default JSON remains unchanged; explicit guide, CLI catalog and schema views provide discoverability.

## Validation

- Full `go test -race ./...`, `go vet ./...`, maintained Enry tests and the affected go-git race suites passed. `third_party/update_go_git.py --check` verified 248 tracked fork files against the pinned upstream and patch.
- [Compatibility results](compatibility.json.gz): 278 cases against the checksum-verified released 0.8.0 executable; 266 exact and 12 previously documented diagnostic improvements, with no unused allowances.
- [Context results](context.json.gz): 41 cases; 39 exact and the two previously documented partial-focus saved-report fixes. Combined: 319 cases, 305 exact.
- Python release checks: 73 total, 71 passed and two artifact-dependent skips. CI contract checks: 13 passed. All eleven ergonomics audit regressions passed. Seven added fuzz targets each received a short three-second follow-up smoke run; this is not exhaustive fuzz coverage.
- [Focused Linguist results](linguist-edge.json): all 47 single-file and nested tree-size cases passed, including all three cases that failed on the incoming PR. The full local reference run exited 137 before returning its JSON, so it supplied no candidate comparison. Full hosted conformance must pass before merge.

## Directory performance regression check

[Raw samples](directory-ab.json.gz) compare the pre-review preparation at `f94bf3c` with the reviewed runtime. [Baseline build receipt](baseline-build.json) and candidate receipt identify both binaries. The [harness](../../performance/v100_preparation/directory_compare.py) uses two warmup pairs, ten measured pairs, alternating execution order, and requires identical stdout, stderr and exit status for every invocation. Inputs are synthetic Java, C# and Go trees, scanned with 16 workers and a tree limit above the fixture size.

| Files | Baseline median | Reviewed median | Baseline peak RSS | Reviewed peak RSS |
| ---: | ---: | ---: | ---: | ---: |
| 1,000 | 48.90 ms | 49.55 ms | 48.07 MiB | 47.55 MiB |
| 10,000 | 246.00 ms | 242.34 ms | 63.05 MiB | 63.66 MiB |
| 50,000 | 1.705 s | 1.746 s | 72.39 MiB | 71.86 MiB |

Peak RSS values are medians of process maxima. These warm-cache measurements on one shared macOS host show small differences, not an additional speedup. They do not measure optional analyzers, Git, Windows, Linux, or cold-cache behavior. The rejected directory optimization's claimed 32–37% improvement is not a release claim. The separate accepted bounded Git cache experiment and its qualified 20–43% elapsed-time improvement across five selected Git workloads remain unchanged; see [that experiment](../../performance/v100_preparation/OPTIMIZATION.md).

## Final environment-coverage correction

A subsequent check found that discarding upstream declaration partial status could hide requirements lost from malformed or omitted manifests. Commit `048c1de2561c0eda42469c40af34fcf491a6e2c5` retains partial environment coverage unless the upstream gap consists solely of strict JSON rejection of selected global.json files that are independently reparsed successfully. Omitted files or diagnostics, unexplained partial states, other incomplete project records, and unvalidated global.json files remain partial. JSONC containing `msbuild-sdks` also remains partial: SDK selection does not reconstruct those declaration-only requirements.

A scanner integration regression combines a malformed Python manifest, a valid C# project and valid JSONC SDK selection: the selection survives, but missing Python requirements keep environment coverage partial. Focused API tests cover the other distinctions. Ordinary valid strict-JSON files do not incur the additional declaration-field recheck.

The [final build receipt](coverage-final/build.json) and [compatibility](coverage-final/compatibility.json.gz) / [context](coverage-final/context.json.gz) receipts bind this runtime to the same released 0.8.0 baseline. All 319 cases again passed with 305 exact results and only the same 14 previously documented differences. The full [race suite](coverage-final/race.log), vet, and all eleven ergonomics regressions passed again. Earlier directory timing receipts remain historical evidence for `2c093c2`; this final correction changes only environment analysis and its tests, not the measured language-only directory path. Hosted checks on the final PR head remain the merge gate.
