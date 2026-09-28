# Releasing dircue

This document describes how a maintainer cuts a versioned release.

## Prerequisites

- You have write access to `war-and-code/dircue`.
- The commit you intend to release is on `main` and has an annotated tag
  (e.g. `v1.0.0`). The release validator checks that it is annotated and
  points to the exact commit; it does not verify a local GPG/SSH signature.
  Asset attestations and the Sigstore-signed checksum manifest provide the
  release's workflow provenance. The tag must exist in the remote before
  dispatching the release workflow.
- Committed Markdown release notes exist at a path you control (e.g.
  `docs/releases/1.0.0.md`).

## Version injection

The version string is injected at build time via Go ldflags:

```
-X github.com/war-and-code/dircue/internal/cli.Version=<version>
```

The variable is `Version` in `internal/cli/cli.go`. `scripts/release.py`
applies this flag for every platform binary it builds.

## Pre-tag checklist

Before creating a release tag, finalize the version entry in `CHANGELOG.md`
(remove the `(unreleased)` marker only as part of the release), review
`docs/releases/1.0.0.md` for release-ready wording, and confirm its claims and
receipts describe the exact commit to tag. The release notes are copied into
the draft GitHub Release as written.

## How to cut a release

### Step 1: Tag the commit

```sh
git tag -a v1.0.0 -m "Release 1.0.0" <commit-sha>
git push origin v1.0.0
```

### Step 2: Dispatch the release workflow

Go to **Actions → Prepare draft release → Run workflow** and fill in:

| Input | Example |
|---|---|
| Version | `1.0.0` (no leading `v`) |
| Commit | the exact 40-character SHA of the tagged commit |
| Notes path | `docs/releases/1.0.0.md` (relative to repo root) |

The workflow:
1. Validates the tag, commit, and release notes path.
2. Builds `CGO_ENABLED=0 -trimpath` Go binaries for all five platforms
   (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64)
   via `scripts/release.py`.
3. Packages platform archives, wheels, and the native Rust structural worker.
4. Runs per-platform smoke tests (binary execution, `--version`, map output).
5. Assembles SHA256SUMS and per-platform provenance.
6. Creates a **draft** GitHub Release. It is never auto-published.

### Step 3: Review the draft release

Download and inspect the draft assets. Run:

```sh
# Download all draft assets (requires GH_TOKEN with read access)
gh release download v1.0.0 --repo war-and-code/dircue --dir /tmp/dircue-review

cd /tmp/dircue-review

# Verify the SLSA build-provenance attestation for every asset
for f in *.tar.gz *.zip *.whl SHA256SUMS; do
  gh attestation verify "$f" \
    --repo war-and-code/dircue \
    --signer-workflow war-and-code/dircue/.github/workflows/release-candidate.yml \
    --source-ref refs/heads/main
done

# Verify the cosign signature on SHA256SUMS
cosign verify-blob \
  --bundle SHA256SUMS.sigstore.json \
  --certificate-identity 'https://github.com/war-and-code/dircue/.github/workflows/release-candidate.yml@refs/heads/main' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  SHA256SUMS

# Verify archive checksums
sha256sum --check SHA256SUMS

# Verify the version strings on Linux amd64 after checking its archive
# (the tar contains both dircue and a dirq -> dircue symlink)
tar -xzf dircue_1.0.0_linux_amd64.tar.gz
./dircue --version
./dirq --version
```

On another host, use that host's matching archive and executable. For example,
macOS uses `dircue` from the Darwin archive and Windows uses `dircue.exe` from
the Windows archive.

`cosign` can be installed via `brew install cosign`, `go install github.com/sigstore/cosign/v3/cmd/cosign@latest`, or from the [Sigstore release page](https://docs.sigstore.dev/cosign/system_config/installation).

### Step 4: Publish

When satisfied, publish the draft release through the GitHub UI.
Publishing is always a deliberate manual step; the workflow never does it.

## Build reproducibility

The release workflow enforces a clean committed checkout with
`scripts/draft_release.py validate`. Builds use `CGO_ENABLED=0 -trimpath
-mod=readonly -buildvcs=false` and pinned Go and Rust toolchains.
`GOENV=off GOFLAGS='' GOEXPERIMENT='' GOAMD64=v1 GOARM64=v8.0` are set
to eliminate host-environment variation.

## No Dependabot or scheduled automation

Dependency updates must be reviewed manually. The CI policy prohibits
`schedule:` triggers and automated dependency PRs (Dependabot/Renovate) until
the project is publicly open and the owner explicitly enables them.
The Enry and go-git forks require canonical regeneration and provenance
checks on version bumps. An automated bump that skips those checks is a
correctness defect.
