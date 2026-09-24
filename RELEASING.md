# Releasing dircue

This document describes how a maintainer cuts a versioned release.

## Prerequisites

- You have write access to `war-and-code/dircue`.
- The commit you intend to release is on `main` and has a signed, annotated tag
  (e.g. `v1.0.0`). The tag must exist in the remote before dispatching the
  release workflow.
- Committed Markdown release notes exist at a path you control (e.g.
  `docs/releases/v1.0.0.md`).

## Version injection

The version string is injected at build time via Go ldflags:

```
-X dircue/internal/cli.Version=<version>
```

The variable lives at `internal/cli/cli.go:25` (`var Version = "1.0.0-dev"`).
`scripts/release.py` applies this flag for every platform binary it builds.

> **TODO (#96):** When the module path changes from `dircue` to
> `github.com/war-and-code/dircue`, the ldflags path must be updated to
> `-X github.com/war-and-code/dircue/internal/cli.Version=<version>` in
> `scripts/release.py`.

## How to cut a release

### Step 1 — Tag the commit

```sh
git tag -a v1.0.0 -m "Release 1.0.0" <commit-sha>
git push origin v1.0.0
```

### Step 2 — Dispatch the release workflow

Go to **Actions → Prepare draft release → Run workflow** and fill in:

| Input | Example |
|---|---|
| Version | `1.0.0` (no leading `v`) |
| Commit | the exact 40-character SHA of the tagged commit |
| Notes path | `docs/releases/v1.0.0.md` (relative to repo root) |

The workflow:
1. Validates the tag, commit, and release notes path.
2. Builds `CGO_ENABLED=0 -trimpath` Go binaries for all five platforms
   (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64)
   via `scripts/release.py`.
3. Packages platform archives, wheels, and the native Rust structural worker.
4. Runs per-platform smoke tests (binary execution, `--version`, map output).
5. Assembles SHA256SUMS and per-platform provenance.
6. Creates a **draft** GitHub Release — it is never auto-published.

### Step 3 — Review the draft release

Download and inspect the draft assets. Run:

```sh
# Verify archive checksums
sha256sum --check SHA256SUMS

# Verify the version string
./dircue-linux-amd64 --version
```

### Step 4 — Publish

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
checks on version bumps — an automated bump that skips that is a correctness
defect.
