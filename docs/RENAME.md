# Rename to dircue

The project changed its name from **auragaze** to **dircue** before public release.
The GitHub repository is [war-and-code/dircue](https://github.com/war-and-code/dircue).
The local `origin` remote points to that repository; no source was pushed.
Existing issue numbers are unchanged. All 13 issues present at the time were checked;
issues #12 and #13 needed name replacements. There were no issue comments.

## Current interfaces

The executable and Go module are `dircue`. Build flags, imports, CLI help/version
output, Docker entrypoint and image examples, archive names, workflow commands,
license attribution, and current documentation use the new name. The CLI flags,
language classification, source-selection rules, and profile JSON schema are
unchanged. Benchmark environment variables use the `DIRCUE_PROFILE_` prefix.

## Historical evidence

The `v0.1.0-rc.1` and `v0.1.0-rc.2` tags and their local distributables are preserved.
Files below existing `tests/**/results/` directories retain the name, commands,
source revisions, output keys, and executable hashes actually measured. The
skill-selection reports under `docs/` also describe the earlier project state.
Historical report keys and compatibility filenames recorded in provenance may
still contain `auragaze`. Current commands use `dircue`.

Renaming a Go module and executable changes binary bytes. The retained
benchmark records identify the pre-rename binaries. Their timing measurements
have not been relabeled as measurements of the renamed executables.

The maintained Enry classifier and generated language/model data are unchanged.
The updater's names and source checksum were updated together. No language data
was refreshed during the rename.

## Validation

The root `go vet ./...` and `go test -race ./...` checks passed, as did all five
packaging regression tests. The renamed executable reports `dircue 0.1.0` and
its legacy and extended JSON matched the preserved RC2 executable byte-for-byte
on a mixed Go/C# project fixture. The complete CLI matrix also passed:
404 exact matches, 16 verified expected
differences, and zero failures. All five renamed archives passed integrity and
payload checks. Native Linux arm64 Docker and emulated Linux amd64 smoke checks
passed. See the rename validation (archived in
[evidence-archive-1](https://github.com/war-and-code/dircue/releases/tag/evidence-archive-1);
restore with `make fetch-receipts`) for raw outputs and source/binary identities. Local archives and extracted
executables are under `dist/dircue-rc3/`; the ordinary local build is `bin/dircue`.

The rename did not push source or tags, or publish a GitHub Release.
