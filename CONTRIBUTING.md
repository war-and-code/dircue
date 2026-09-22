# Contributing to dircue

Dircue is developed in the open. Bug reports and design discussion
through [GitHub Issues](https://github.com/war-and-code/dircue/issues)
are the main way to contribute today.

## Before you file

- Read the [design principles](docs/DESIGN_PRINCIPLES.md). They
  govern what dircue promises and what it explicitly does not, and
  most contested questions resolve back to them.
- Check the [capability matrix](docs/CAPABILITIES.md) and the
  [compatibility promise](docs/COMPATIBILITY.md) for what is currently
  supported and what is intentionally out of scope.
- Search existing issues so we can consolidate related reports.

## Reporting a bug

Use the [bug report template](.github/ISSUE_TEMPLATE/bug_report.yml).
Include:

- Output of `dircue --version`.
- The exact command line and its arguments.
- Operating system and architecture.
- Which content source was selected (`--source auto`, `git`, or
  `directory`).
- A minimal repository or archive that reproduces the issue. When a
  real repository is too large, a synthetic tree that reproduces the
  observation is fine; describe how you built it.

Classification mismatches against Linguist should include the file, the
detected language, and the Linguist version and result you expected.

## Proposing a change

Use the [feature request template](.github/ISSUE_TEMPLATE/feature_request.yml).
Explain the concrete problem, what dircue would need to observe or
report, and how it fits (or does not fit) the design principles. Larger
proposals benefit from a linked example: a small saved report or a
sketch of the JSON your consumer needs.

Security-relevant reports have their own private path; see the
[security policy](SECURITY.md). Please do not open a public issue for
vulnerabilities.

## Pull requests

Pull requests are welcome after prior discussion in an issue. Opening
a matching issue first lets us confirm scope and design before you
invest time; unsolicited large changes may be closed with a request to
discuss first.

When you do open a pull request, follow the
[pull request template](.github/PULL_REQUEST_TEMPLATE.md). Concretely,
the project's review practice looks for:

- **Tests.** Every behavior change gets a Go or Python test that would
  have failed before the change.
- **Contract discipline.** Successful CLI outputs and exit statuses
  for existing invocations must not change; the
  [1.0 compatibility promise](docs/COMPATIBILITY.md) spells out what
  else is frozen. Adjustments to those contracts belong in a
  discussion first.
- **Evidence receipts.** Performance or coverage claims need
  reproducible measurements: what was compared, how, how many
  samples, and what environment. `tests/performance/` and
  `tests/compatibility_v100/` show the format we use.
- **Design-principle alignment.** Incomplete knowledge must remain
  visible. Silent fallbacks, unlabelled partial results, or executing
  inspected content are not acceptable trade-offs.
- **Minimal, reviewable diffs.** Match surrounding code style; keep
  drive-by refactors out of unrelated changes.
- **Contained scope.** Do not add analysis that requires the network,
  a build step, or executing project code.

Run `go test -race ./...` and `go vet ./...` locally before requesting
review. When your change touches release tooling or documentation,
also run `python3 -m pytest tests/release -q`.

## Legal

By contributing you agree that your contribution may be released under
the project's [MIT license](LICENSE). Third-party code and data must
retain their existing licenses and attribution; the
[third-party notices](THIRD_PARTY_NOTICES.md) list the current set.
