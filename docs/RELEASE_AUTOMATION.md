# Preparing a draft release

The **Prepare draft release** workflow builds and validates release assets, then creates a GitHub **draft**. It runs only by manual dispatch from `main`. It does not create tags, publish a release, upload to PyPI or change repository visibility.

The workflow reuses `scripts/release.py`, `scripts/structural_worker_release.py` and `scripts/wheels.py`. These already encode the project's committed-source builds, platform requirements, dependency sources and license payloads. Introducing GoReleaser would duplicate those contracts without removing the native worker validation.

Wheels are standard GitHub Release attachments, independent of PyPI publication.
Assembly requires all seven platform wheels and rejects missing or mismatched
files. Linux's glibc and musl wheels carry the same static core executable;
the hosted installation tests execute on glibc, not musl.

## Local packaging

From a clean committed checkout, prepare all five core archives and seven wheels
with one command:

```sh
make release VERSION=0.9.0
```

Pass a version explicitly: `X.Y.Z` or `X.Y.Z-(alpha|beta|rc).N`. The default
development version is rejected before compilation because it is not a wheel
release version.

Archives, core provenance and archive checksums go into `dist/`. Wheels, their
provenance and a separate checksum file go into `dist/wheels/`. To keep different
candidates separately, choose fresh output directories:

```sh
make release VERSION=0.9.0 \
  RELEASE_DIR=dist/release-0.9.0 WHEEL_DIR=dist/wheels-0.9.0
```

Existing outputs are refused. Packaging builds committed source with the pinned
Go compiler, then wraps those exact executable bytes in wheels. It does not
create a tag, upload assets or publish a release. `make release-archives` retains
the archive-only path; the underlying Python commands remain available in the
[distribution guide](DISTRIBUTION.md#prepare-archives-and-wheels-locally).

Cross-compilation prepares every core target but does not test them on their
native operating systems. Test the installed wheel on the current host with,
for example, this Apple Silicon command:

```sh
python3 scripts/wheel_release_smoke.py \
  --release-dir dist --wheel-dir dist/wheels \
  --platform darwin-arm64 --version 0.9.0 --output dist/wheel-launcher.json
```

Use the matching platform name on other hosts: `darwin-amd64`, `linux-amd64`,
`linux-arm64` or `windows-amd64`. The helper installs offline into an isolated
Python environment and checks the packaged console command. A compatible local
Python installation with `venv` and `ensurepip` is required.

The optional structural worker is packaged separately using
`scripts/structural_worker_release.py`; see the
[worker packaging instructions](DISTRIBUTION.md#optional-structural-worker).
For a complete release with native validation on all five platforms, use the
hosted workflow below. Its final assembly and download checks cover archives,
wheels, worker packages and their validation receipts together.

## Before dispatch

A maintainer must:

1. Merge the intended release state, including a reviewed Markdown release description, into `main`.
2. Create and push a tag such as `v1.2.3` pointing to that exact commit. This workflow never creates or moves it.
3. Confirm there is no draft or published release for the version. Existing releases are not overwritten.
4. Dispatch from `main` with the version **without** `v`, its full 40-character commit SHA, and the committed notes file's relative path.

For example, after completing those steps:

```sh
gh workflow run release-candidate.yml --ref main \
  -f version=1.2.3 \
  -f commit=FULL_40_CHARACTER_COMMIT_SHA \
  -f notes=docs/releases/1.2.3.md
```

The example version and file are placeholders. The selected commit must equal the `main` revision at dispatch. A tag on an earlier commit is rejected. An ignored local release draft cannot be used: the workflow reads the notes' exact UTF-8 bytes from the committed Git blob.

GitHub requires write access to dispatch a workflow. Builds have read-only repository permissions; only the final draft job receives `contents: write`. Checkout credentials are not persisted. There is no pull-request, tag-push or `workflow_run` trigger. Input values enter commands through environment variables and are checked before building.

## What the workflow checks

Five native runners build Linux AMD64/ARM64, macOS AMD64/ARM64 and Windows AMD64 packages. Go uses the exact patch version in `go.mod`; the worker uses Rust 1.94.0 and its locked dependencies. All actions are pinned to commits.

Each runner builds one core archive and its matching wheels, tests the native Rust worker, and packages the worker with dependency source archives. The Linux AMD64 job also runs the Go race suite and vet. The final smoke check extracts the **packaged** core and worker, verifies the core version, and runs the existing 21-fixture/20-language structural harness. That harness includes direct worker comparison, one/eight-worker determinism, combined projects/metrics/structure, parser recovery and explicit omissions.

Candidates starting with 0.4.0 (including alpha, beta and release candidates) must also pass `scripts/function_release_smoke.py` using those same extracted executables. It compares explicit `--functions` output across one/eight workers and against direct native results for all 21 fixtures, including exact source SHA-256 values, one parse per file and unchanged default report fields. Separate Java, C# and Python fixtures check hand-marked spans and selected cyclomatic values. Recovery, long names and the 128-function per-file cap must preserve partial coverage and population counts. Generated files remain excluded with visible `outside_scope` omissions; this ordinary scope exclusion does not make otherwise complete structural or function coverage partial. A separate malformed-Python case checks that incomplete parsing propagates from the parent report. Function entries must remain absent from the ordinary per-file records. These are parser and integration checks, not a code-quality assessment.

The function smoke receipt records the executable and test-input hashes. Assembly requires it for every platform on 0.4.0 and later, and rejects missing checks or mismatched identities. Prior-version artifact checks retain the default structural path; they do not require a flag absent from 0.3.0. The standalone helper can additionally check clear refusal with an explicitly supplied older worker via `--baseline-worker`; the workflow does not download an older worker or require that optional check.

Candidates starting with 0.5.0 must also pass `scripts/declarations_release_smoke.py` against the extracted core on every native platform. Twelve small manifests cover npm, Go, Python/uv, Cargo, .NET and Maven: identities, requirements, workspace/project relationships, and named interfaces. The helper checks one/eight-worker determinism, unchanged default language and aggregate output, standalone/combined declaration parity, and omission of raw script bodies. It saves reports, deletes the source directory, and then checks offline comparison, unchanged reports, malformed/duplicate-key JSON rejection, and qualified absence when coverage is partial. These checks use no worker, Git command, package manager, network access or repository code execution.

Assembly requires all five declaration receipts for 0.5.0 and later. Each binds the core executable hash, version, helper and fixture hashes, expected facts, output hashes and complete check inventory. Missing or altered evidence prevents assembly. Earlier versions retain their existing gates; they do not acquire a dependency on declaration or comparison commands.

The separate **Structural worker** workflow also runs the function smoke on all five native platforms, without requiring a release tag. It gives the core and worker the same CI-only fixture version, `0.4.0-rc.1`; this label neither selects nor creates a release. After the existing native tests and structural breadth check, it verifies the worker archive's provenance, checksums and source inputs, extracts its executable, and runs the function helper against that packaged worker and the newly built core. The existing per-platform Actions artifact retains `functions-<platform>.json` alongside the breadth receipt and worker archive for seven days. Draft pull requests remain skipped unless the workflow is manually dispatched. This workflow has read-only repository permissions and creates no tags or releases.

The final job requires all five platform artifacts. It verifies:

- Core version, commit, Git tree, archive and executable hashes, plus committed README and license bytes.
- Worker version, commit, clean source state, toolchain, target and current source-input hashes; staged worker sources; every packaged Cargo dependency's lockfile checksum and declared license; internal and external archive checksums.
- All seven wheels' identities, hashes and provenance. Their uncompressed entries, including `RECORD`, notices and executable bytes, must match a fresh deterministic package of the verified core. ZIP compression bytes may differ across runner zlib versions.
- Each native smoke receipt's executable hashes, fixture count, language count and test-source hashes, plus the required function-evidence receipt for 0.4.0 and later declaration/comparison receipt for 0.5.0 and later, targeted profiling receipt for 0.7.0 and later, and environment/planning/comparison receipt for 0.8.0 and later.

The draft contains five core archives, five worker archives, seven wheels, per-platform core/wheel provenance and native smoke receipts (including function receipts for 0.4.0 and later, declaration receipts for 0.5.0 and later, targeted profiling receipts for 0.7.0 and later, and context receipts for 0.8.0 and later), an aggregate `release-candidate.json`, `SHA256SUMS`, and `SHA256SUMS.sigstore.json` (the Sigstore cosign bundle for `SHA256SUMS`). The aggregate records the assembly script/workflow hashes and release-note digest. The complete asset count is 39 for 0.4 and 44 for 0.5–0.6 with the five declaration receipts, 49 for 0.7 with five targeted profiling receipts, and 54 from 0.8 with five context receipts, plus one additional `SHA256SUMS.sigstore.json` bundle from 1.0 onward.

From 1.0.0, the final job additionally:

1. Creates a **GitHub SLSA build-provenance attestation** for every assembled asset using `actions/attest-build-provenance`. Attestations are stored in GitHub's trust store and verified by `gh attestation verify <file> --repo war-and-code/dircue`.
2. Signs `SHA256SUMS` with **keyless Sigstore cosign** (no long-lived key), producing `SHA256SUMS.sigstore.json`. The identity is bound to the workflow URL via GitHub's OIDC issuer.
3. Verifies both the attestation and the cosign signature before the job succeeds. The run fails if either check fails.

The `draft` job requests only the minimum additional permissions required for signing: `id-token: write` (OIDC token for cosign) and `attestations: write` (store provenance). The top-level workflow permissions remain `contents: read`.

After uploading, the workflow downloads every attached asset into a fresh directory, compares its filename and SHA-256 with the assembled files, and checks that the release remains a draft. A separate Actions artifact retains the download-verification receipt.

Temporary Actions artifacts expire after seven days. Release assets persist with the draft. Cargo caching retains crate archives because the worker distribution includes those sources; a cache miss still performs the same builds and checks.

## Review and publish separately

Inspect the draft description, assets, receipts, workflow logs and checksums. Publishing remains a separate maintainer decision through the GitHub release page or an explicit command such as:

```sh
gh release edit v1.2.3 --draft=false
```

This command publishes the GitHub release to the repository's existing audience; it does not publish wheels to PyPI. PyPI publishing, signing/attestation policy and package-manager notifications are outside this workflow.

The workflow serializes preparation for a version and rechecks remote release/tag state immediately before draft creation. Repository rules should prevent another actor moving the tag during release preparation. An independently performed concurrent GitHub mutation is not covered by a transactional lock.

If uploading fails partway through, inspect the resulting draft. The workflow will refuse to replace it on a rerun; remove an incomplete draft deliberately after review, or choose a new version. It never deletes remote state to recover automatically.

## Local verification and current limits

```sh
python3 -m unittest discover -s tests/release -p 'test_*.py'
python3 -m unittest discover -s tests/ci -p 'test_*.py'
# actionlint validates workflow YAML syntax; run it if already installed:
actionlint .github/workflows/release-candidate.yml
actionlint .github/workflows/structural-worker.yml
```

The hosted orchestration completed all seven jobs in both the [0.4 release-candidate rehearsal](https://github.com/war-and-code/dircue/actions/runs/35478130617) and the [0.4.0 release preparation](https://github.com/war-and-code/dircue/actions/runs/35478628605). The final run verified five native platforms, assembled and downloaded all 39 assets, and preserved the exact committed notes. After separate review, [0.4.0 was published](https://github.com/war-and-code/dircue/releases/tag/v0.4.0); a subsequent download check confirmed that the published assets matched the verified draft assets.

The 0.5 declaration gate has local real-core smoke coverage and receipt/assembly contract tests, including missing checks and mismatched versions, executable hashes, helper hashes and fixtures. That local coverage does not establish a successful five-platform 0.5 release run. Its native checks must pass on the actual packaged artifacts before publication.

Local Darwin arm64 archives built from clean commit `58ed625` with the validation version `0.4.0-rc.1` passed both packaged smoke checks. The function check covered all 21 fixtures across 20 languages and the 11 counterexamples described above. An actual v0.3.0 worker rejected the new opt-in request with an update instruction and empty stdout; the helper's prior-version path also passed against the v0.3.0 archives. No tag or release was created for this local check.

The uploader requires GitHub.com Actions URLs and fixes every gh subprocess to `GH_HOST=github.com`. It does not promise GitHub Enterprise compatibility, cryptographic build attestations, automatic tag management or idempotent replacement of drafts. Wheel validation deliberately retains the existing reviewed Go 1.26.6 requirement; changing the compiler needs a coordinated packager update and validation. These limitations fail explicitly rather than silently broadening support.

References: [manual workflow dispatch](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_dispatch), [GitHub CLI release creation](https://cli.github.com/manual/gh_release_create), and [GitHub CLI release editing](https://cli.github.com/manual/gh_release_edit).

## Toolchain identity

Release archives and wheel provenance require exactly Go 1.26.6. A newer compatible Go toolchain can build the CLI locally, but changing the reviewed release toolchain requires updating its image pin and provenance checks together. The structural worker crate has an internal development version of `0.0.0`; its distribution version, exact source commit, and binary digest are recorded in the release archive provenance. Do not infer the distribution version from Cargo metadata or a strings search.
