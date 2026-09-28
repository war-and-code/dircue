# Contributing to dircue

Dircue is developed in the open. Bug reports and design discussion through [GitHub Issues](https://github.com/war-and-code/dircue/issues) are the main way to contribute today.

## Before you file

- Read the [design principles](docs/DESIGN_PRINCIPLES.md). They govern what dircue promises and what it explicitly does not, and most contested questions resolve back to them.
- Check the [capability matrix](docs/CAPABILITIES.md) and the [compatibility promise](docs/COMPATIBILITY.md) for what is currently supported and what is intentionally out of scope.
- Search existing issues so we can consolidate related reports.

## Reporting a bug

Use the [bug report template](.github/ISSUE_TEMPLATE/bug_report.yml). Include:

- Output of `dircue --version`.
- The exact command line and its arguments.
- Operating system and architecture.
- Which content source was selected (`--source auto`, `git`, or `directory`).
- A minimal repository or archive that reproduces the issue. When a real repository is too large, a synthetic tree that reproduces the observation is fine; describe how you built it.

Classification mismatches against Linguist should include the file, the detected language, and the Linguist version and result you expected.

## Proposing a change

Use the [feature request template](.github/ISSUE_TEMPLATE/feature_request.yml). Explain the concrete problem, what dircue would need to observe or report, and how it fits (or does not fit) the design principles. Larger proposals benefit from a linked example: a small saved report or a sketch of the JSON your consumer needs.

Security-relevant reports have their own private path; see the [security policy](SECURITY.md). Please do not open a public issue for vulnerabilities.

## Pull requests

Outside pull requests and larger contributions are not accepted at present. Please use GitHub Issues for bug reports, questions, and feature suggestions. Maintainers implement and review changes within the project.

## Maintainer checks

Changes should preserve documented CLI and report contracts, include focused regression tests where behavior changes, and support performance claims with reproducible measurements. Run `go test -race ./...` and `go vet ./...`. For release tooling, also run:

```sh
python3 -m unittest discover -s tests/release -p 'test_*.py'
```

Third-party code and data must retain their licenses and attribution; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
