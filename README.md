# `dircue`

Profile source code repos and other directories of computer content.

Handed a repository you didn't write, you first need to know what's in it: its languages and projects, what gets built and deployed, what it exposes and depends on, and which deeper tools are worth running where. Answering that usually means reading around by hand, then running several tools that each cover one slice, such as Linguist for languages, scc for line counts and Syft for packages.

`dircue map` answers it in one deterministic, offline pass, without running anything in the directory. It reads a committed Git tree or an ordinary directory and writes one portable document covering:

- **components:** projects from 36 component kinds, reported under 27 `ecosystem` values;
- **deployables:** containers, Compose, Kubernetes, Helm, Terraform, serverless and CI;
- **interfaces:** binaries, ports, gRPC and OpenAPI;
- **capabilities:** datastores, caches, messaging, auth and cloud SDKs;
- **relationships:** what builds, runs, depends on and contains what.

Every fact carries evidence identifying its source file and rule; a source span is included when the analyzer can locate one. Every question carries a coverage status. `complete` means exhaustive for its scope; anything heuristic says `partial` and why.

`dircue` started as "just" a Linguist-compatible language profiler, and that's still a supported use case. `dirq` is a shorter alias for `dircue`.

The [design principles](docs/DESIGN_PRINCIPLES.md) explain the trade-offs behind defaults, user control and evidence honesty. The [compatibility policy](docs/COMPATIBILITY.md) says what stays stable across 1.x, and the [CHANGELOG](CHANGELOG.md) says what changed in each version.

## Quick example

The summary for [GoogleCloudPlatform/microservices-demo](https://github.com/GoogleCloudPlatform/microservices-demo):

```text
$ dircue map --summary .
Directory map: partial (git source)
Languages: Go 28.9%, Python 27.9%, HTML 10.0%, C# 8.1%, Shell 6.6%, Dockerfile 4.6% (+4 more)
Content populations: 5   Relationships: 187   Packages: 0
Relationship declarations: 12 runs, 13 builds
Components: 13 (dotnet 2, go 4, gradle 1, npm 2, python 4) (+1 test)
  cartservice [dotnet]
  checkoutservice [go]
  emailservice [python]
  frontend [go]
  (+8 more components)
Deployables: 99 (+9 CI workflows, +49 cluster resources)
  adservice [workload] → runs hipstershop
  cartservice [workload] → runs cartservice
  checkoutservice [workload] → runs checkoutservice
  currencyservice [workload] → runs grpc-currency-service
  (+37 more runnable deployables)
Interfaces: checkoutservice, frontend, grpc.health.v1.Health, grpc.health.v1.Health/Check (+33 more)
Capabilities: ai:llm-sdk, auth:oauth2, cache:redis, cloud:gcp (+7 more)
Possible next analyzers: bca, bifrost, scc, syft
Attached provider runs: 0
Still uncertain:
  Analyzer coverage: no analyzer report attached
  Capabilities: some service dependencies may be unrecognized
  Components: some project declarations or references may be unresolved
  Deployables: 1 Helm chart not rendered (templates require evaluation for complete coverage)
  Interfaces: some entry points or contracts may be unrecognized
  Packages: no complete package inventory is established
  (+1 more questions)
Use --json for evidence and full coverage details.
```

Common next steps:

```sh
dircue map --json . > map.json                               # the full evidence graph (schema/map.schema.json)
dircue map --attach syft-json=sbom.json --json . > map.json  # join saved Syft, SARIF, Noir or Bifrost reports
dircue map route --json map.json                             # inert follow-up plans for deeper analyzers
dircue map compare --format markdown base.json head.json     # what changed between two maps
dircue map locate map.json results.sarif > located.sarif     # which component owns each SARIF location
dircue map --forest /disk                                    # nested repositories, dependency trees and the rest
```

Attached reports add facts and run coverage to the map. The [map guide](docs/MAP.md) documents the node and edge model, limits, source binding, attachments, comparison, routing and SARIF annotation.

Results against hand-written labels for seven repositories, which also informed map development, are in [GOLDEN.md](docs/GOLDEN.md); Linguist and scc parity across 38 repositories is in the [atlas](tests/atlas/README.md).

The classic Linguist-compatible output is unchanged. In a directory holding one small Go file:

```sh
$ printf 'package main\n\nfunc main() {}\n' > main.go
$ dircue --json .
{"Go":{"size":29,"percentage":"100.00"}}
```

Check the exit status before consuming stdout:

- Success exits `0` and writes JSON to stdout.
- Handled errors exit `1` with diagnostics on stderr.

## Install

Each release has platform archives and Python wheels, which contain the same Go executable, plus a signed `SHA256SUMS` manifest. There is no PyPI package or published container image. While the repository is private, fetch assets with authenticated `gh release download` (see the [distribution guide](docs/DISTRIBUTION.md#private-or-draft-github-downloads)), and give `go install` `GOPRIVATE=github.com/war-and-code` plus Git credentials for GitHub.

The commands below target `v1.0.0`, which isn't tagged yet as of this writing.

```sh
# Go toolchain (1.26.6 or later): installs the module at the release tag
go install github.com/war-and-code/dircue@v1.0.0           # dircue
go install github.com/war-and-code/dircue/cmd/dirq@v1.0.0  # dirq, the same program

# Release archive + checksum verification (Linux amd64 shown; substitute your platform)
curl -fsSL -O https://github.com/war-and-code/dircue/releases/download/v1.0.0/dircue_1.0.0_linux_amd64.tar.gz
curl -fsSL -O https://github.com/war-and-code/dircue/releases/download/v1.0.0/SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing
tar -xzf dircue_1.0.0_linux_amd64.tar.gz
mkdir -p "$HOME/.local/bin"
install -m 755 dircue "$HOME/.local/bin/dircue"
ln -sf dircue "$HOME/.local/bin/dirq"    # the archive's dirq is this same link

# Python wheel via uv (offline-compatible; the launcher only invokes the bundled Go binary)
uvx --from \
  https://github.com/war-and-code/dircue/releases/download/v1.0.0/dircue-1.0.0-py3-none-manylinux_2_17_x86_64.whl \
  dircue map --summary /path/to/checkout    # the wheel also installs dirq
```

The [distribution guide](docs/DISTRIBUTION.md) covers the full platform matrix, offline installation and building release archives locally. The optional structural worker is packaged separately; see the [worker guide](docs/STRUCTURE.md#building-the-add-on).

### Verifying release integrity

Every release asset has a GitHub build attestation (SLSA provenance), and `SHA256SUMS` is signed with keyless Sigstore via cosign:

```sh
# Install the GitHub CLI (https://cli.github.com) and cosign (https://docs.sigstore.dev/cosign/system_config/installation)

# 1. Verify the SLSA build provenance attestation for any asset (example: the Linux amd64 archive)
gh attestation verify dircue_1.0.0_linux_amd64.tar.gz \
  --repo war-and-code/dircue \
  --signer-workflow war-and-code/dircue/.github/workflows/release-candidate.yml \
  --source-ref refs/heads/main

# 2. Download the Sigstore bundle alongside SHA256SUMS
curl -fsSL -O https://github.com/war-and-code/dircue/releases/download/v1.0.0/SHA256SUMS
curl -fsSL -O https://github.com/war-and-code/dircue/releases/download/v1.0.0/SHA256SUMS.sigstore.json

# 3. Verify the cosign signature on SHA256SUMS
cosign verify-blob \
  --bundle SHA256SUMS.sigstore.json \
  --certificate-identity 'https://github.com/war-and-code/dircue/.github/workflows/release-candidate.yml@refs/heads/main' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  SHA256SUMS

# 4. Verify the checksum of your downloaded asset
sha256sum -c SHA256SUMS --ignore-missing
```

Both checks confirm that the release workflow produced the files while running on `main`, without long-lived keys. The attestation records the source commit (`--format json` shows it); the cosign signature alone doesn't pin a run or commit. The workflow itself only builds from the current `main` head, and only when an annotated version tag points at it.

### Building from source

Building needs **Go 1.26.6**, the version pinned in `go.mod` and used for release archives:

```sh
CGO_ENABLED=0 go build -trimpath -o bin/dircue .
./bin/dircue --breakdown --json /path/to/checkout
```

### Docker

Build a local image from the tagged source and run it with the network denied and the source mounted read-only:

```sh
docker build --build-arg VERSION=1.0.0 -t dircue:1.0.0 .
docker run --rm --network none \
  -v /path/to/checkout:/repo:ro \
  dircue:1.0.0 map --json /repo
```

The image runs as an unprivileged user and includes `dirq` but not the structural worker. Mounted source must be readable by that user; `--user` can match your pipeline's permissions.

## Compatibility direction

Version 0.9 kept the documented legacy Linguist CLI and JSON as a compatibility target.

The [`dircue map`](docs/MAP.md) document is the primary 1.x contract, and the [compatibility policy](docs/COMPATIBILITY.md) lists every surface 1.x keeps stable.

Against the published 0.9.0 executable, 227 of 278 compatibility cases produce identical stdout and stderr; 51 have output changes, with no exit-status changes. Strict raw Linguist output matches for committed trees, ordinary directories and unborn repositories. See the [comparison receipt](tests/compatibility_v100/results/v090-compatibility.json).

## Replace `github-linguist --json`

Put dircue on `PATH` and replace `github-linguist --json` with `dircue --json` in the same working directory. At a Git repository root, both analyze committed `HEAD`. If the executable name is fixed in your job, install the binary as `github-linguist`.

The output has the same language-keyed structure, with integer byte sizes and string percentages:

```json
{"C#":{"size":43,"percentage":"67.19"},"Java":{"size":21,"percentage":"32.81"}}
```

- No detected languages produces `{}` and exits `0`.
- Warnings go to stderr and don't change a successful exit status. As in Linguist, reaching the default 100,000-entry tree limit returns `{}` with a warning and exit `0`; raise `--tree-size` above the entry count for complete statistics.
- JSON object-key order isn't part of the contract.
- dircue can profile a directory without a committed Git repository, where Linguist fails. If your job must fail when Git content is unavailable, use `dircue --source git --json`.

Compatibility is tested against Linguist 9.7.0; other versions may use different language data. See the [documented differences](tests/conformance/DISCREPANCIES.md).

| | GitHub Linguist 9.7.0 | Upstream Enry | dircue |
| --- | --- | --- | --- |
| Implementation | Ruby with native dependencies | Go library and CLI | Go CLI with a maintained Enry fork; optional native parser worker |
| Directory statistics | Requires a usable Git repository | Supports ordinary directories | Committed Git trees or ordinary directories |
| CLI output | Reference contract | Its own defaults and output | Targets Linguist's supported flags and output |
| Additional profiling | Language metadata | Language metadata | Portable directory maps; metadata and format evidence; project declarations and graphs; package/configuration observations; caller rules; optional scc and structural metrics/hotspots |

Language detection uses a maintained Enry fork; Git object reads use a maintained go-git v5.19.2 fork with fixes for streaming delta reconstruction and file-handle cleanup; scc v4.1.0 is used unmodified. The [upstream update process](third_party/README.md) records source versions, patches, generated data and licenses.

In the recorded 0.1 release candidate run (Linux arm64, Docker, 11 pinned public projects), median execution was 5.38–14.66× faster than Linguist with matching language totals and file breakdowns; peak memory was higher on several large projects.

## Content selection

- At a Git repository root, dircue analyzes the committed `HEAD` tree. Dirty, untracked and ignored files don't affect the results. Symlinks and submodules are excluded.
- For a plain directory, it analyzes the filesystem contents. No Git executable, history or metadata is needed.
- `--source directory` inspects current files even when Git metadata exists.
- `--source git` requires a Git repository and committed tree, and fails otherwise.

In the default `auto` mode, a repository without commits, a bare repository, a subdirectory beneath a repository, or an unusable Git directory is analyzed as a plain directory. The legacy language JSON has no `source` field, so this fallback is silent there (see [known boundaries](docs/COMPATIBILITY.md#known-boundaries-at-10)); the `analyze all` report records it in `discovery.source`.

```sh
dircue /checkout --json
dircue --rev HEAD~1 --breakdown /checkout
dircue --source directory --json /exported-source
dircue --json /checkout/main.go
```

A single file gets Linguist's separate inspection layout: language, MIME type, lines, nonblank lines, generated/vendor flags and large-file status. `--rev` applies only to directory statistics. Explicit symlink targets are rejected.

## CLI flags

Flags can appear before or after the path. With no path, the current directory is used. Use `--` or `./name` for a path named like a subcommand or beginning with `-`.

| Flag | Behavior |
| --- | --- |
| `-j`, `--json` | Emit JSON. |
| `-b`, `--breakdown` | Include file paths in language results. |
| `-s`, `--strategies` | Show each file's detection strategy in text output. |
| `-r`, `--rev REV` | Select a Git revision for directory statistics; default `HEAD`. |
| `--tree ID` | Select an exact 40-hex Git tree object; mutually exclusive with `--rev`. |
| `-t`, `--tree-size N` | Return empty statistics with a warning when the tree reaches this entry count; default 100,000. |
| `--source auto\|git\|directory` | Select the content source; default `auto` (see [Content selection](#content-selection)). |
| `--on-error fail\|continue` | Fail on per-file read errors (default), or continue with explicit omissions. |
| `--workers N` | Use 1–1024 file workers. Zero selects the smaller of GOMAXPROCS and 16. |
| `--max-file-bytes N` | Skip files larger than N bytes, with a warning. Zero disables this limit. |
| `-v`, `--version` | Print the version. |
| `-h`, `--help` | Print command help. |

Directory statistics count full file sizes, and classification reads at most the first 128 KiB of each file, as Linguist does for repository blobs. Outside Git, single-file inspection reads the whole file up to 1 MiB and a 128 KiB prefix beyond that ([DISC-007](tests/conformance/DISCREPANCIES.md#disc-007-bounded-inspection-of-large-git-free-single-files)).

## Other profilers

`dircue map` is the main entry point. The narrower `analyze` reports it grew from are still available, each opt-in and documented in its own guide:

| Command | Reports | Guide |
| --- | --- | --- |
| `analyze discovery` | file metadata and candidate manifests, without reading source payloads | [DISCOVERY.md](docs/DISCOVERY.md) |
| `analyze metrics` | code, comment and blank lines and complexity estimates, via scc | [METRICS.md](docs/METRICS.md) |
| `analyze projects` | .NET, Maven and Gradle declarations, references and file composition | [PROJECTS.md](docs/PROJECTS.md) |
| `analyze declarations` | project identities, workspaces, requirements and named interfaces from manifests | [DECLARATIONS.md](docs/DECLARATIONS.md) |
| `analyze environments` | environment requirements declared in manifests | [ENVIRONMENTS.md](docs/ENVIRONMENTS.md) |
| `analyze formats` | file-format evidence | [FORMATS.md](docs/FORMATS.md) |
| `analyze availability` | Git LFS pointers, gitlinks, submodules and sparse checkouts that can make source look absent | [AVAILABILITY.md](docs/AVAILABILITY.md) |
| `analyze focus` | one project, with the original root's inventory as context | [FOCUS.md](docs/FOCUS.md) |
| `analyze graph` | .NET project-reference graphs | [GRAPH.md](docs/GRAPH.md) |
| `analyze packages --syft-report FILE` | package evidence from a saved Syft report | [PACKAGE_EVIDENCE.md](docs/PACKAGE_EVIDENCE.md) |
| `analyze rules --rules-file FILE` | matches for caller-supplied filename, path and content rules | [RULES.md](docs/RULES.md) |
| `analyze registries` | NuGet and npm package-source declarations | [REGISTRIES.md](docs/REGISTRIES.md) |
| `analyze structure` | syntax and metrics for 20 languages from a separate native worker | [STRUCTURE.md](docs/STRUCTURE.md) |
| `analyze explain --file PATH` | why one file or project (`--project`) was classified and selected as it was | [EXPLANATIONS.md](docs/EXPLANATIONS.md) |

`analyze all --json` combines languages, ecosystem and framework findings, and any modules you add with flags such as `--declarations`, `--metrics` or `--structure`. It emits the versioned [profile schema](schema/profile.schema.json). `compare` diffs two saved profiles, and `plan` suggests follow-up runs from one; see [COMPARISON.md](docs/COMPARISON.md) and [PLANNING.md](docs/PLANNING.md).

```sh
dircue analyze discovery --json /checkout
dircue analyze all --declarations --metrics --json /checkout
dircue analyze structure --hotspots --structural-worker ./dircue-structural-worker --json /checkout
```

A report can exit `0` and still be partial. Check each module's status and omissions before treating its results as complete. The [capability matrix](docs/CAPABILITIES.md) lists supported languages, ecosystems and limits, and the [staged-analysis guide](docs/STAGED_ANALYSIS.md) shows how to go from a cheap first pass to deeper modules.

## Built-in help

The binary carries command help, a workflow guide, a machine-readable CLI catalog and JSON schemas. None of these read a source directory:

```sh
dircue --help
dircue map --help
dircue capabilities --guide
dircue capabilities --cli --json
dircue capabilities --schema profile > profile.schema.json
```

`capabilities --cli --json` describes every command, flag, output contract and exit code. Misspelled options get a suggested correction on stderr; the command still fails.

## Attributes and boundaries

dircue is configured through CLI flags and `.gitattributes`; there's no configuration file. For example, these overrides include XML and generated Java in language statistics:

```gitattributes
*.xml linguist-detectable=true
generated/**/*.java linguist-generated=false
```

Root and nested `.gitattributes` support Linguist's language, vendor, generated, documentation, detectable and LFS attributes, with Git's precedence, macros and glob syntax. An analysis accepts at most 10,000 compiled attribute rules, a limit Linguist doesn't have. The [conformance scope](tests/conformance/COVERAGE.md) lists what's tested.

dircue reads files and Git objects without invoking hooks, package managers, Git or build scripts. Directory reads use `os.Root`, skip symlinks and special files, and on Unix can't be hung by a file swapped for a FIFO. Neither filesystem nor Git mode is an atomic snapshot of a directory that's changing, so use a stable checkout.

Read failures fail the run by default; `--on-error continue` turns recoverable per-file read errors into reported omissions. Some limits produce skipped or partial results with exit status `0`, so check coverage as well as the exit status. Use JSON for pipelines: text output escapes control characters in filenames.

Structural analysis runs only the worker executable you pass, one at a time, with an 8 MiB input limit and a per-file deadline. Memory isn't hard-capped; use container CPU, memory and time limits where that matters. The [resource-budget guide](docs/RESOURCE_BUDGETS.md) has measurements, and [SECURITY.md](SECURITY.md) describes the analysis boundaries.

## Troubleshooting

| Symptom | Check or fix |
| --- | --- |
| `dircue: command not found` | Add the installation directory to `PATH`, or call `./bin/dircue`. |
| Recent edits are missing from the report | Repository roots use committed `HEAD`. Use `--source directory` to inspect working files. |
| A large repository returns empty language statistics | Check stderr for the tree-size warning and set `--tree-size` above the entry count. |
| A map exits `0` but says `partial` | Exit status confirms that a valid map was produced. Inspect each `coverage` entry and its reasons before relying on absence. |
| An attached report has `binding: unknown` | The report and selected source lack comparable snapshot identity. See [source selection and binding](docs/MAP.md#source-selection-and-binding); don't treat path association as proof of the same snapshot. |
| `map locate` reports `unresolvable_uri` | Supply `--source-uri` when SARIF uses absolute artifact URIs, and confirm that the URI is inside that root. |
| Docker cannot read mounted source | Check file permissions and use `--user` to select a suitable UID/GID. |
| uv cannot find dircue on PyPI | Releases aren't published to PyPI. Install a wheel from the GitHub Release URL or a local file; see the [distribution guide](docs/DISTRIBUTION.md). |

## Verification

[Conformance](tests/conformance/README.md) compares output with the pinned Ruby Linguist CLI, including failures and intentional extensions. [Upstream sample results](tests/conformance/results/samples.md) compare language classifiers, and the [performance harness](tests/performance/README.md) times identical public checkouts only after requiring identical language output.

The maintained classifier's `GetLanguage` was 1.64× faster on full sample contents and 1.71× faster on 128 KiB prefixes than upstream Enry v2.9.6 in a controlled library comparison. Against the published Enry CLI, three comparisons were inconclusive, and many scenarios differ under Enry's defaults.

Raw measurements, source identities and validation records are archived in the [evidence-archive-1 release](https://github.com/war-and-code/dircue/releases/tag/evidence-archive-1); restore them locally with `make fetch-receipts`. These results hold for the recorded inputs. Records from before the project was renamed from **auragaze** keep the old name; see [the rename notes](docs/RENAME.md).

```sh
go test -race ./...
go vet ./...
```

The [CI guide](docs/CI.md) covers local checks and release preparation.

## FAQ

**Do I need Go, Ruby, or Git installed?** The core executable needs none of them. Building it needs Go; a wheel's launcher needs Python 3.10+. Optional structural analysis needs a prebuilt native worker, and building that worker needs Rust.

**Can it profile an extracted archive?** Yes. Point it at the extracted directory; Git metadata is optional.

**Will it pick up upstream detection improvements?** The maintained fork has a reproducible [update procedure](third_party/README.md). Updates are pinned and compared against Linguist before adoption; scans never download rules.

**Is there a Go library API?** Not a supported one in 1.x; `pkg/` packages may change in any release. See the [compatibility policy](docs/COMPATIBILITY.md).

## Contributions

Outside pull requests are not accepted at present.

Bug reports and design proposals go through [GitHub Issues](https://github.com/war-and-code/dircue/issues) with the templates the repository ships.

See [CONTRIBUTING.md](CONTRIBUTING.md) for reporting guidance and [SECURITY.md](SECURITY.md) for private vulnerability reporting.

## License

[MIT](LICENSE). The maintained Enry fork keeps Apache-2.0 licensing and Linguist's MIT data notices, and scc is MIT. [Third-party notices](THIRD_PARTY_NOTICES.md) cover the Go executable and embedded MIME database. The optional structural worker includes BCA under MPL-2.0 and Tree-sitter grammars under their own licenses; its archive carries their sources, licenses and provenance (see [worker redistribution](docs/STRUCTURE.md#dependencies-and-redistribution)).

The optional Bend research models include [modified Apache-2.0 proof examples](research/bend-aggregation/topk/THIRD_PARTY_NOTICES.md) and are kept separate from the released executables.
