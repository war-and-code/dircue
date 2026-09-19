# Preparing a draft release

The **Prepare draft release** workflow builds and validates release assets, then creates a GitHub **draft**. It runs only by manual dispatch from `main`. It does not create tags, publish a release, upload to PyPI or change repository visibility.

The workflow reuses `scripts/release.py`, `scripts/structural_worker_release.py` and `scripts/wheels.py`. These already encode the project's committed-source builds, platform requirements, dependency sources and license payloads. Introducing GoReleaser would duplicate those contracts without removing the native worker validation.

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

The final job requires all five platform artifacts. It verifies:

- Core version, commit, Git tree, archive and executable hashes, plus committed README and license bytes.
- Worker version, commit, clean source state, toolchain, target and current source-input hashes; staged worker sources; every packaged Cargo dependency's lockfile checksum and declared license; internal and external archive checksums.
- All seven wheels' identities, hashes and provenance. Their uncompressed entries, including `RECORD`, notices and executable bytes, must match a fresh deterministic package of the verified core. ZIP compression bytes may differ across runner zlib versions.
- Each native smoke receipt's executable hashes, fixture count, language count and test-source hashes.

The draft contains five core archives, five worker archives, seven wheels, per-platform core/wheel provenance and native smoke receipts, an aggregate `release-candidate.json`, and `SHA256SUMS`. The aggregate records the assembly script/workflow hashes and release-note digest. These are build receipts and checksum checks, not signed attestations.

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
actionlint .github/workflows/release-candidate.yml
```

The new orchestration has been checked locally with packaging-contract tests and actionlint. It has **not been dispatched or tested end to end on GitHub**. A deliberate five-runner rehearsal is still required before relying on it for a release. The native runner labels and existing packagers come from the already exercised structural-worker workflow; that does not prove this new orchestration has run.

The uploader requires GitHub.com Actions URLs and fixes every gh subprocess to `GH_HOST=github.com`. It does not promise GitHub Enterprise compatibility, cryptographic build attestations, automatic tag management or idempotent replacement of drafts. Wheel validation deliberately retains the existing reviewed Go 1.26.6 requirement; changing the compiler needs a coordinated packager update and validation. These limitations fail explicitly rather than silently broadening support.

References: [manual workflow dispatch](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_dispatch), [GitHub CLI release creation](https://cli.github.com/manual/gh_release_create), and [GitHub CLI release editing](https://cli.github.com/manual/gh_release_edit).
