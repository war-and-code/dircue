# Syft Oracle

Independent oracle that validates the `packages` coverage binding produced by
`dircue map --attach syft-json=<report>`.

## Oracle role

This oracle validates the *packages* question binding end-to-end:

- Every Syft artifact whose location is inside the scanned directory must appear
  in the dircue map as a `package` node, matched by PURL.
- Artifacts whose location path is inside a dircue-detected component must have
  a `packaged_in` edge to that component.
- The `packages` coverage question must transition from `unknown` (no attachment)
  to a bound status (`partial` or `complete`) with attachment.
- The coverage ledger must record the Syft run with `ran: true`.
- No Syft descriptor, schema, distro, or other non-package fields may appear in
  the dircue map output.

## Oracle matrix

| Oracle        | What it validates               | Where it runs                        |
|---------------|---------------------------------|--------------------------------------|
| Linguist 9.7.0 | language-detection fidelity    | CI: `linguist-conformance` job       |
| scc 4.1.0     | line-count metrics differential | CI: `metrics-conformance` job        |
| Syft 1.52.0   | package-coverage binding        | `make syft-oracle` / `workflow_dispatch` |

## Fixtures

Fixtures are committed real source trees. Reports are committed byte-for-byte
as Syft wrote them.

### `fixtures/go-single/` + `fixtures/go-single.syft.json`

A minimal Go module with one declared dependency (`github.com/google/uuid v1.6.0`).
Syft finds 1 artifact; dircue must attribute it to the Go module component.

### `fixtures/multi/` + `fixtures/multi.syft.json`

Two sub-directories:
- `frontend/` — npm project with `chalk@5.3.0` (package-lock.json v3)
- `backend/` — Python project with `requests@2.31.0` and `certifi@2024.2.2`
  (requirements.txt)

Syft finds 4 artifacts. The oracle checks that:
- `chalk` and `frontend` (npm) have `packaged_in` edges to the `frontend`
  component that dircue detects from `frontend/package.json`.
- `requests` and `certifi` appear as package nodes (no `packaged_in` edge is
  required when no component owns the backend directory).

## Regenerating reports

Requires Docker. Run once with a network connection to pull the image, then
offline with `--network none`:

```bash
SYFT_IMAGE="anchore/syft@sha256:500e2d872ac019436926e8322b4fc1f39441d94d21f6f4046c6ff29b30e8cb02"

docker run --rm --network none \
  -v "$PWD/tests/syft-oracle/fixtures/go-single:/src:ro" \
  "$SYFT_IMAGE" scan dir:/src -o syft-json -q \
  > tests/syft-oracle/fixtures/go-single.syft.json

docker run --rm --network none \
  -v "$PWD/tests/syft-oracle/fixtures/multi:/src:ro" \
  "$SYFT_IMAGE" scan dir:/src -o syft-json -q \
  > tests/syft-oracle/fixtures/multi.syft.json
```

Commit the new files unmodified.

## Report provenance

| Report file            | Syft version | Image digest (sha256)                                                     | Generated     |
|------------------------|-------------|----------------------------------------------------------------------------|---------------|
| `go-single.syft.json`  | 1.52.0      | `500e2d872ac019436926e8322b4fc1f39441d94d21f6f4046c6ff29b30e8cb02`        | 2026-09-23    |
| `multi.syft.json`      | 1.52.0      | `500e2d872ac019436926e8322b4fc1f39441d94d21f6f4046c6ff29b30e8cb02`        | 2026-09-23    |
