# Tool Oracle

Independent oracle that validates dircue's ingestion of real, pinned analyzer
reports and the `dircue map locate` command for SARIF location annotation.

## Oracle roles

| Issue | What it validates |
|-------|-------------------|
| #77   | Ingest deeper-tool facts: Syft, OWASP Noir, SARIF run metadata |
| #78   | Analyzer coverage ledger and blind spots |
| #79   | Route deeper analyzers per component with prerequisites |
| #82   | SARIF locate: three emitters of different kinds |

## Fixture matrix

| Fixture dir                    | Source fixture                                          |
|--------------------------------|---------------------------------------------------------|
| `fixtures/flask-app/`          | Minimal Flask Python app (app.py + requirements.txt)    |
| `fixtures/express-app/`        | Minimal Express.js app (src/index.js + package.json)    |
| `fixtures/spring-app/`         | Minimal Spring MVC controller (Java)                    |
| `fixtures/python-lint-sample/` | Python module with intentional lint findings            |
| `fixtures/semgrep-rules/`      | Local semgrep rules used during fixture generation      |

## Committed reports (oracle fixtures)

All reports are **genuine tool output**, produced by the real tool at the
pinned version, run offline with `--network none` after image pull, over a
read-only bind-mount of the committed fixture. They are committed byte-for-byte.

### OWASP Noir JSON reports

| Report file                        | Noir version | Image digest (sha256)                                                    | Generated  |
|------------------------------------|-------------|--------------------------------------------------------------------------|------------|
| `fixtures/flask-app.noir.json`     | 1.3.1       | `4f39307465326433b281508b5ffc433ec31cd150d7fd8f69167946c8ffb689ab`       | 2026-09-24 |
| `fixtures/express-app.noir.json`   | 1.3.1       | `4f39307465326433b281508b5ffc433ec31cd150d7fd8f69167946c8ffb689ab`       | 2026-09-24 |
| `fixtures/spring-app.noir.json`    | 1.3.1       | `4f39307465326433b281508b5ffc433ec31cd150d7fd8f69167946c8ffb689ab`       | 2026-09-24 |

### OWASP Noir SARIF reports

| Report file                              | Noir version | Image digest (sha256)                                                    | Generated  |
|------------------------------------------|-------------|--------------------------------------------------------------------------|------------|
| `fixtures/flask-app.noir.sarif.json`     | 1.3.1       | `4f39307465326433b281508b5ffc433ec31cd150d7fd8f69167946c8ffb689ab`       | 2026-09-24 |
| `fixtures/express-app.noir.sarif.json`   | 1.3.1       | `4f39307465326433b281508b5ffc433ec31cd150d7fd8f69167946c8ffb689ab`       | 2026-09-24 |
| `fixtures/spring-app.noir.sarif.json`    | 1.3.1       | `4f39307465326433b281508b5ffc433ec31cd150d7fd8f69167946c8ffb689ab`       | 2026-09-24 |

### ruff SARIF reports

ruff runs in the Docker container with working directory `/src`; result URIs
are `file:///src/sample.py`. The oracle passes `--source-uri /src`.

| Report file                                    | ruff version | Image digest (sha256)                                                    | Generated  |
|------------------------------------------------|-------------|--------------------------------------------------------------------------|------------|
| `fixtures/python-lint-sample.ruff.sarif.json`  | 0.11.13     | `45cb2b28f0ad694917b159c65d058f1eeafdda0cb155a30374194c4c4b6c56df`       | 2026-09-24 |

### Semgrep SARIF reports

semgrep runs with `--metrics off` and a local rules file. It emits
`uriBaseId: %SRCROOT%` without defining `originalUriBaseIds`, so resolution
is `unresolvable_uri`. This is a documented Semgrep behavior, not a dircue
defect. The test `test_semgrep_undefined_base_id_is_unresolvable` asserts
that dircue correctly rejects the unresolvable URIs.

| Report file                                        | Semgrep version | Image digest (sha256)                                                    | Generated  |
|----------------------------------------------------|----------------|--------------------------------------------------------------------------|------------|
| `fixtures/python-lint-sample.semgrep.sarif.json`   | 1.117.0        | `f435f06d2332f24d76a93791c8c5bd8c5bef7b426061eb04ff452a9d41e1b596`       | 2026-09-24 |

### ruff SARIF for flask-app

ruff finds 0 findings in the clean flask-app code. The SARIF is still a real
tool run, useful for run-coverage metadata testing.

| Report file                          | ruff version | Image digest (sha256)                                                    | Generated  |
|--------------------------------------|-------------|--------------------------------------------------------------------------|------------|
| `fixtures/flask-app.ruff.sarif.json` | 0.11.13     | `45cb2b28f0ad694917b159c65d058f1eeafdda0cb155a30374194c4c4b6c56df`       | 2026-09-24 |

## SARIF 2.1.0 JSON schema

`testdata/sarif-schema-2.1.0.json` is the OASIS SARIF 2.1.0 JSON Schema,
fetched from:

  https://docs.oasis-open.org/sarif/sarif/v2.1.0/os/schemas/sarif-schema-2.1.0.json

The OASIS SARIF specification is published under the OASIS IPR Policy and is
freely available for implementation and testing use. See:
  https://www.oasis-open.org/policies-guidelines/ipr/

## Bifrost availability

Bifrost is not runnable offline in the hermetic container environment required
by this oracle. The parser is tested with existing synthetic fixtures (labeled
as such) in `pkg/providerjoin/` unit tests. See the CHANGES.md for details.

## Crashed run (executionSuccessful: false)

The exercised ruff 0.11.13 and Semgrep OSS 1.117.0 invocations did not produce
`executionSuccessful: false` for a failing run. Each tool either:
- Sets `executionSuccessful: true` with `toolExecutionNotifications` containing errors, or
- Exits non-zero without producing SARIF output at all.

This oracle has no pinned golangci-lint report fixture.

This case is covered by Go unit tests in `pkg/coverageledger/` and
`pkg/providerjoin/` with a hand-crafted minimal SARIF fixture.

## Usage

```bash
# Fast committed-report checks (no Docker required):
make tool-oracles

# Regenerate all reports with pinned images and diff against committed ones:
make regenerate-tool-fixtures
```

## Regenerating reports

Requires Docker, a network connection for image pull, then runs offline.
All commands use `--network none` after pull.

### OWASP Noir (ghcr.io/owasp-noir/noir@sha256:4f39307...)

```bash
NOIR_IMAGE="ghcr.io/owasp-noir/noir@sha256:4f39307465326433b281508b5ffc433ec31cd150d7fd8f69167946c8ffb689ab"

for fixture in flask-app express-app spring-app; do
  docker run --rm --network none \
    -v "$PWD/tests/tools/fixtures/${fixture}:/app:ro" \
    "$NOIR_IMAGE" noir scan /app -f json 2>/dev/null \
    > "tests/tools/fixtures/${fixture}.noir.json"

  docker run --rm --network none \
    -v "$PWD/tests/tools/fixtures/${fixture}:/app:ro" \
    "$NOIR_IMAGE" noir scan /app -f sarif 2>/dev/null \
    > "tests/tools/fixtures/${fixture}.noir.sarif.json"
done
```

### ruff (ghcr.io/astral-sh/ruff@sha256:45cb2b28...)

```bash
RUFF_IMAGE="ghcr.io/astral-sh/ruff@sha256:45cb2b28f0ad694917b159c65d058f1eeafdda0cb155a30374194c4c4b6c56df"

docker run --rm --network none \
  -v "$PWD/tests/tools/fixtures/python-lint-sample:/src" \
  -w /src "$RUFF_IMAGE" \
  check --output-format sarif . 2>/dev/null \
  > tests/tools/fixtures/python-lint-sample.ruff.sarif.json

docker run --rm --network none \
  -v "$PWD/tests/tools/fixtures/flask-app:/src" \
  -w /src "$RUFF_IMAGE" \
  check --output-format sarif . 2>/dev/null \
  > tests/tools/fixtures/flask-app.ruff.sarif.json
```

### Semgrep (semgrep/semgrep@sha256:f435f06d...)

```bash
SEMGREP_IMAGE="semgrep/semgrep@sha256:f435f06d2332f24d76a93791c8c5bd8c5bef7b426061eb04ff452a9d41e1b596"

docker run --rm --network none \
  -v "$PWD/tests/tools/fixtures/python-lint-sample:/src:ro" \
  -v "$PWD/tests/tools/fixtures/semgrep-rules:/rules:ro" \
  "$SEMGREP_IMAGE" \
  semgrep --config /rules/python-checks.yaml --metrics off --sarif /src \
  2>/dev/null \
  > tests/tools/fixtures/python-lint-sample.semgrep.sarif.json
```
