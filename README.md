# `dircue`

Profile source code repos and other directories of computer content.

`dircue map` describes what an unfamiliar directory contains and how it fits
together. It works deterministically and offline, without running anything in
the directory.

`dircue map` reads a committed Git tree or an ordinary directory and writes one portable document covering:
- **components:** projects across 26 ecosystems;
- **deployables:** containers, Compose, Kubernetes, Helm, Terraform, serverless and CI;
- **interfaces:** binaries, ports, gRPC and OpenAPI;
- **capabilities:** datastores, caches, messaging, auth and cloud SDKs;
- **relationships:** what builds, runs, depends on and contains what.

Every fact carries its evidence (file, line, rule), and every question carries a coverage status. `complete` means exhaustive for its scope; anything heuristic says `partial` and why. The Linguist-compatible language profiler that dircue started as is unchanged and still available.

The [design principles](docs/DESIGN_PRINCIPLES.md) explain the trade-offs behind defaults, user control and evidence honesty, and the [compatibility policy](docs/COMPATIBILITY.md) says what 1.0 freezes. What's new in 1.0.0 is in the [CHANGELOG](CHANGELOG.md).

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
dircue map --json . > map.json                          # the full evidence graph (schema/map.schema.json)
dircue map --attach syft-json=sbom.json --json . > map.json   # join saved Syft, SARIF, Noir or Bifrost reports
dircue map route --json map.json                        # inert follow-up plans for deeper analyzers
dircue map compare --format markdown base.json head.json   # what changed between two maps
dircue map locate map.json results.sarif > located.sarif     # which component owns each SARIF location
dircue map --forest /disk                               # nested repositories, dependency trees and the rest
```

Attached reports contribute facts and run coverage, never findings or verdicts. Results against hand-written labels for seven repositories, which also informed map development, are in [GOLDEN.md](docs/GOLDEN.md); Linguist and scc parity across 38 repositories is in the [atlas](tests/atlas/README.md).

The classic Linguist-compatible output is unchanged:

```sh
$ dircue --json .
{"Go":{"size":77,"percentage":"100.00"}}
```

Success exits `0` and writes JSON to stdout. Handled errors exit `1` with diagnostics on stderr. Check the exit status before consuming stdout.

## Install

Releases provide platform archives and Python wheels as GitHub release assets, with a `SHA256SUMS` manifest. An archive and its matching wheels contain identical Go executable bytes. PyPI publication and container images are not part of the release; the `Dockerfile` builds an image locally. While the repository is private, use authenticated `gh release download` to fetch assets; the [distribution guide](docs/DISTRIBUTION.md#private-or-draft-github-downloads) shows the commands.

The commands below target the prospective `v1.0.0` release. Its remote tag
and release assets are not available yet. The `go install` command has been
validated against a local module proxy; fetching the module from GitHub remains
untested until the tag is pushed and access is available.

```sh
# Go toolchain (1.26.6 or later): installs the module at the release tag
go install github.com/war-and-code/dircue@v1.0.0

# Release archive + checksum verification (Linux amd64 shown; substitute your platform)
curl -fsSL -O https://github.com/war-and-code/dircue/releases/download/v1.0.0/dircue_1.0.0_linux_amd64.tar.gz
curl -fsSL -O https://github.com/war-and-code/dircue/releases/download/v1.0.0/SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing
tar -xzf dircue_1.0.0_linux_amd64.tar.gz
mkdir -p "$HOME/.local/bin"
install -m 755 dircue "$HOME/.local/bin/dircue"

# Python wheel via uv (offline-compatible; the launcher only invokes the bundled Go binary)
uvx --from \
  https://github.com/war-and-code/dircue/releases/download/v1.0.0/dircue-1.0.0-py3-none-manylinux_2_17_x86_64.whl \
  dircue map --summary /path/to/checkout
```

While the repository is private, `go install` also needs `GOPRIVATE=github.com/war-and-code` and Git credentials for GitHub.

The [distribution guide](docs/DISTRIBUTION.md) covers the full archive/wheel matrix, offline installation, and how to build a local archive from source without publishing. The optional structural worker is packaged separately; see the [worker guide](docs/STRUCTURE.md#building-the-add-on).

Building from source needs **Go 1.26.6** (the version pinned in `go.mod`; release archives are produced with exactly this toolchain):

```sh
CGO_ENABLED=0 go build -trimpath -o bin/dircue .
./bin/dircue --breakdown --json /path/to/checkout
```

## Compatibility direction

Version 0.9 keeps the documented legacy Linguist CLI and JSON as a
compatibility target. The [`dircue map`](docs/MAP.md) document is the primary
1.x contract, and the [compatibility policy](docs/COMPATIBILITY.md) lists
every surface 1.x keeps stable.

Against the published 0.9.0 executable, 227 of 278 compatibility cases produce
identical stdout and stderr; 51 have output changes, with no exit-status
changes. Strict raw Linguist output matches for committed trees, ordinary
directories and unborn repositories. See the [comparison receipt](tests/compatibility_v100/results/v090-compatibility.json).

## License

[MIT](LICENSE). Third-party components retain their own licenses; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## What dircue observes

Language detection uses a maintained Enry fork with compatibility tests against [GitHub Linguist](https://github.com/github-linguist/linguist). Dircue also identifies supported ecosystems, frameworks, and layouts. Optional modules add [metadata discovery](docs/DISCOVERY.md), [project declarations and interfaces](docs/DECLARATIONS.md), .NET project graphs, [environment requirements](docs/ENVIRONMENTS.md), [source-availability evidence](docs/AVAILABILITY.md), [focused project profiling](docs/FOCUS.md), [format evidence](docs/FORMATS.md), [package-source declarations](docs/REGISTRIES.md), caller-supplied rules, Syft package-evidence import, and [scc](https://github.com/boyter/scc) line counts. Optional structural analysis covers 20 languages through a separate native worker built on [big-code-analysis](https://github.com/dekobon/big-code-analysis) and [Tree-sitter](https://tree-sitter.github.io/tree-sitter/).

```sh
dircue analyze discovery --json /path/to/checkout
dircue analyze all --json /path/to/checkout
dircue analyze projects --json /path/to/checkout
dircue analyze declarations --json /path/to/checkout
dircue analyze environments --json /path/to/checkout
dircue analyze formats --json /path/to/content
dircue analyze focus --project services/app/app.csproj --metrics --json /path/to/checkout
dircue analyze availability --json /path/to/checkout
dircue analyze explain --file src/main.go --json /path/to/checkout
dircue analyze structure --hotspots --structural-worker ./dircue-structural-worker --json /path/to/checkout
dircue analyze all --projects --metrics --json /path/to/checkout
dircue map --json /path/to/checkout
dircue map compare --json before-map.json after-map.json
dircue map route --json map.json
dircue map locate map.json results.sarif > located.sarif
dircue --breakdown --json /path/to/checkout
dircue capabilities --json
dircue plan saved-profile.json --module declarations --json
```

## Help without leaving the CLI

The installed binary includes command help, a workflow guide, a machine-readable
CLI catalog, and JSON schemas. These commands work offline and do not inspect a
source directory or execute their examples:

```sh
dircue --help
dircue analyze --help
dircue analyze metrics --help
dircue map --help
dircue map settings --help
dircue map locate --help
dircue capabilities --guide
dircue capabilities --guide --json
dircue capabilities --cli --json
dircue capabilities --schema profile > profile.schema.json
```

The catalog describes commands, applicable flags, source selection, output
contracts, and exit codes. `capabilities --json` keeps its original, narrower
purpose: describing modules supported by saved-report planning. Schema export
provides a standalone Draft 2020-12 document for an external validator; it does
not validate an input report. The catalog distinguishes whole-output schemas
from profile components and identifies the legacy single-file output, which
does not yet have a bundled schema.

Misspelled options can produce a suggested correction on stderr. Suggestions
never execute automatically; the original invocation still fails.

## Replace `github-linguist --json`

Install dircue on `PATH` and replace `github-linguist --json` with `dircue --json`. Keep the same working directory. No path or subcommand is required; at a Git repository root, both commands analyze committed `HEAD`.

Dircue emits the same language-keyed JSON structure, with integer byte sizes and string percentages. For example:

```json
{"C#":{"size":43,"percentage":"67.19"},"Java":{"size":21,"percentage":"32.81"}}
```

The process contract is:

- Successful analysis exits `0` and writes JSON to stdout.
  - No detected languages produces `{}` and still exits `0`.
- Handled CLI errors, such as an invalid flag, missing path, or missing revision, exit `1` and write diagnostics to stderr.
  - Check the exit status before consuming stdout.
- Warnings go to stderr and do not themselves change a successful exit status.
  - As in Linguist, reaching the default 100,000-entry tree limit returns `{}` with a warning and exit `0`; it is not a partial language report.

This replacement uses the legacy language JSON contract. `dircue analyze all --json` emits a different, extended schema and requires a different consumer. JSON object-key order is not part of the compatibility contract.

One deliberate difference affects exit handling: dircue can successfully profile a directory without a committed Git repository. If your job *must* fail when Git content is unavailable, use `dircue --source git --json`.

Compatibility is tested against Linguist 9.7.0; other versions may use different language data. See the [documented differences](tests/conformance/DISCREPANCIES.md).

Existing supported flags and explicit paths can be retained.

If the executable name is fixed in your job, install the binary as `github-linguist`.

## Comparison

Language profiling targets **GitHub Linguist 9.7.0** compatibility through a maintained Enry fork. The comparison harness measures upstream Enry v2.9.6 separately. The [upstream update process](third_party/README.md) records source versions, patches, generated data, and licenses. Git object reads use a maintained go-git v5.19.2 fork with fixes for streaming delta reconstruction and file-handle cleanup; scc v4.1.0 is used without source changes.

| | GitHub Linguist 9.7.0 | Upstream Enry | Dircue |
| --- | --- | --- | --- |
| Implementation | Ruby with native dependencies | Go library and CLI | Go CLI with a maintained Enry fork; optional native parser worker |
| Directory statistics | Requires a usable Git repository | Supports ordinary directories | Committed Git trees or ordinary directories |
| CLI output | Reference contract | Its own defaults and output | Targets Linguist's supported flags and output |
| Additional profiling | Language metadata | Language metadata | Portable directory maps; metadata and format evidence; project declarations and graphs; package/configuration observations; caller rules; optional scc and structural metrics/hotspots |

The recorded 0.1 release candidate had 5.38–14.66× faster median execution than Linguist on 11 pinned public projects, with matching language totals and file breakdowns. That Linux arm64 Docker run also recorded higher peak memory on several large projects.

The [verification section](#verification) links these measurements, the separate Enry comparison, and their limitations.

## Content selection

- At a Git repository root, analyze the committed `HEAD` tree. Dirty, untracked, and ignored working files do not affect the results. Symlinks and submodules are excluded.
- For a plain directory, analyze its filesystem contents. No Git executable, history, or metadata is required.
- Use `--source directory` to inspect current filesystem contents, including when Git metadata exists.
- Use `--source git` to require a Git repository and committed tree.

An initialized repository without commits falls back to directory analysis in auto mode. A requested subdirectory beneath a repository is scanned as that filesystem subtree. These cases extend Ruby Linguist, which requires a usable repository at the supplied directory path.

In auto mode, a **bare** repository, an **unborn** repository (no branch refs yet), or a corrupted-enough-to-be-unusable Git directory also falls back to directory analysis. The legacy language JSON does not carry a `source` field, so this fallback is silent on the Linguist-compatible surface today (see [known boundaries](docs/COMPATIBILITY.md#known-boundaries-at-10)); the extended `analyze all` report's `discovery.source` field records the effective mode. Use `--source git` to require a Git repository — the run fails explicitly if it cannot select a committed tree.

```sh
dircue /checkout --json
dircue --rev HEAD~1 --breakdown /checkout
dircue --source directory --json /exported-source
dircue --json /checkout/main.go
```

Single-file mode follows Linguist's separate inspection layout, including language, MIME, lines, nonblank lines, generated/vendor flags, and large-file status. As in Linguist, `--rev` applies to directory statistics; single-file inspection uses `HEAD` when available. Explicit symlink targets are rejected as a documented safety difference.

## CLI compatibility

Flags can appear before or after the path. With no path, the current directory is used. Use `--` to disambiguate a path named `analyze` or `compare`, or beginning with `-`. For a directory named `compare`, `dircue ./compare --json` also retains language analysis; bare `dircue compare` selects the new comparison command.

| Flag | Behavior |
| --- | --- |
| `-j`, `--json` | Emit JSON. |
| `-b`, `--breakdown` | Include file paths in language results. |
| `-s`, `--strategies` | Show each file's detection strategy in text output. |
| `--tree ID` | Select an exact 40-hex Git tree object; mutually exclusive with `--rev`. |
| `--on-error fail\|continue` | Fail on per-file read errors by default; opt into explicit partial results for recoverable reads. |
| `-r`, `--rev REV` | Select a Git revision for directory statistics; default `HEAD`. |
| `-t`, `--tree-size N` | Return empty statistics with a warning when the tree reaches this entry count; default 100,000. |
| `--source auto\|git\|directory` | Select the content source; default `auto`. `auto` uses committed Git content when a usable repository is present and falls back to directory content otherwise, including for bare and unborn repositories (see [Content selection](#content-selection)). Use `git` to require a Git tree. |
| `--workers N` | Use 1–1024 file workers. Zero selects the smaller of GOMAXPROCS and 16. |
| `--max-file-bytes N` | Skip files larger than N bytes, with a warning. Zero disables this optional limit. |
| `-v`, `--version` | Print the version. |
| `-h`, `--help` | Print command help. |

Legacy directory JSON maps language names to integer `size` and a two-decimal string `percentage`; `--breakdown` adds `files`. Plain output retains Linguist's percentage/byte/language rows and breakdown ordering. JSON consumers should compare object fields rather than object-key order.

An attribute-forced language on an empty file can produce Linguist's legacy string percentage `"NaN"`. The extended report uses numeric zero for that case.

Directory language statistics count full file sizes. Classification examines at most the first **128 KiB**, matching Linguist's lazy repository blobs. Large source files are included by default; `--max-file-bytes N` optionally skips larger files with a warning.

Standalone single-file inspection outside Git reads the complete file through 1 MiB. For larger files it inspects a 128 KiB prefix and reports zero lines/SLOC with `large: true`. Ruby inspects the entire standalone file for language, binary type, and generated status, so those fields can differ beyond this explicit resource boundary ([DISC-007](tests/conformance/DISCREPANCIES.md#disc-007-bounded-inspection-of-large-git-free-single-files)). Git single-file inspection and directory statistics use the 128 KiB repository semantics in both tools.

For trees with 100,000 or more entries, raise `--tree-size` **above** the entry count to obtain complete statistics. Reaching the limit produces empty statistics and a warning.

## Profiling beyond languages

The 1.0 entry point is `dircue map`. It performs one bounded scan and
produces a portable graph of observed content, languages, components,
deployables, interfaces, capabilities, and supported relationships. Coverage
is part of the document, so an unknown or bounded question does not look like a
proven absence.

```sh
dircue map --json /checkout > map.json
dircue map --summary /checkout
dircue map --json --attach syft-json=syft.json /checkout > enriched-map.json
```

The narrower `analyze` reports remain available alongside it. The [map guide](docs/MAP.md) documents its node and edge model,
limits, source binding, attachments, comparison, routing, and SARIF annotation.

```sh
dircue analyze languages /checkout --json
dircue analyze ecosystems /checkout --json
dircue analyze frameworks /checkout --json
dircue analyze all /checkout --json --breakdown
dircue analyze metrics /checkout --json --files
dircue analyze all /checkout --json --metrics
dircue analyze projects /checkout --json
dircue analyze all /checkout --json --projects --metrics
dircue analyze structure /checkout --json --files --structural-worker /opt/dircue/dircue-structural-worker
```

Language commands use the legacy output contract. Ecosystem and framework commands emit finding arrays. `analyze all --json` emits the versioned [profile schema](schema/profile.schema.json): languages, ecosystem/framework/layout findings, summary counts, and warnings.

`analyze metrics` counts code, comment, and blank lines and estimates lexical complexity with scc. It also reports totals by language and immediate parent directory; `--files` adds per-file counts and skip reasons. `analyze all --metrics` includes counting alongside the other profilers. Counting is opt-in, and plain language commands keep their existing behavior.

Metrics default to files included in language statistics, so XML logs are excluded unless attributes override their selection. `--metrics-scope text` includes detected textual languages beyond that source selection. Files larger than 16 MiB are skipped for counting by default; `--metrics-max-file-bytes` can raise that bound to 256 MiB. Counts always cover complete files. Check `metrics.status` and skip reasons before treating totals as complete. See the [metrics guide](docs/METRICS.md) for scope, limits, output fields, and which scc capabilities are integrated.

`analyze projects` reports .NET and Maven declarations, conservative Gradle observations, and filename-based project discovery for other ecosystems. It records references, configuration candidates, and file/byte composition. Dynamic build expressions remain conditional or unresolved; a present reference target does not establish a working build. Directory-based file attribution reports ambiguous and unassigned files. See the [project guide](docs/PROJECTS.md).

`analyze declarations` reads supported manifests for project identities, workspace relationships, requirements and named interfaces without running repository code. Standalone use avoids reading unrelated file contents; `analyze all --declarations` adds the same module to a broader profile. Script bodies are withheld. See the [declaration guide](docs/DECLARATIONS.md) for supported syntax and limits.

`analyze structure` sends selected source in 20 supported languages to an explicitly selected worker. Each file is parsed once; the same Tree-sitter tree supplies syntax observations and BCA metrics. Java and C# also receive custom declaration counts; other languages expose syntax-node and recovery counts alongside BCA metrics. `--files` includes per-file metrics and parser provenance. Syntax recovery produces partial results, and unsupported inputs have omission reasons. There is no compiler type checking or cross-file call graph. See the [capability matrix](docs/CAPABILITIES.md) for language and ecosystem coverage, and the [structural analysis guide](docs/STRUCTURE.md) for grammar limitations, offline worker packaging, and resource bounds.

For a lightweight first pass, use [`analyze discovery --json`](docs/DISCOVERY.md), adding `--source directory` when you want current files rather than the committed Git tree. It inventories file metadata and candidate manifests without reading source payloads. Then choose project, line, or structural analysis from that evidence. The [staged-analysis guide](docs/STAGED_ANALYSIS.md) includes a consumer for the earlier projects-report schema and explains why empty language totals or XML-heavy content alone are insufficient reasons to skip follow-ups.

Plain `analyze all` retains its existing behavior. Add `--declarations`, `--environments`, `--projects`, `--metrics`, or `--structure` for the modules you need. Environment analysis automatically includes the declaration evidence it reuses. Structural analysis requires `--structural-worker`; it never downloads a parser during a scan.

The directory map combines relationship and entry-point observations in one
document. The [capability matrix](docs/CAPABILITIES.md)
describes the supported inputs and limits of the existing analysis modules.

Available since 0.4.0:

- [`analyze discovery`](docs/DISCOVERY.md) inventories regular-file metadata without reading source payloads. It includes filename hints for manifests and packaged artifacts, including paths excluded from language statistics.
- [`analyze graph`](docs/GRAPH.md) derives .NET project-reference components, cycles, and degrees from static declarations, keeping conditional and unresolved edges separate.
- [`analyze packages --syft-report FILE`](docs/PACKAGE_EVIDENCE.md) imports an existing Syft JSON report. Coordinate mapping and source binding are explicit; it does not execute Syft.
- [`analyze rules --rules-file FILE`](docs/RULES.md) applies explicit, bounded filename/path/content rules and reports matches, provenance, and omissions.
- [`analyze registries`](docs/REGISTRIES.md) reads selected NuGet and npm package-source declarations, retaining qualified names and sanitized URL origins without evaluating an effective feed set.
- [`analyze structure --functions`](docs/FUNCTIONS.md) retains bounded function-space metrics with source spans, source hashes, and coverage. It reuses the worker's existing parse and requires a separately packaged worker with function-metric support, introduced in 0.4.0.

Select combinations explicitly, such as `analyze all --discovery --graph` or `analyze all --structure --functions`. Add `--registries` to inspect package-source declarations, or `--rules-file FILE` and `--syft-report FILE` when supplying those inputs. These modules are opt-in. Existing profiling invocations retain their output contracts.

| Requested output | Schema version |
| --- | --- |
| Existing aggregate report without optional modules | `1.0.0` |
| Metrics, without projects or structure | `1.1.0` |
| Projects or structure, with optional metrics | `1.2.0` |
| Discovery, graph, imported package evidence, rules, registries, or function metrics | `1.3.0` |
| Project declarations, alone or with other modules | `1.4.0` |
| File-format evidence | `1.5.0` |
| Focus, availability, explanations, or focused metrics | `1.6.0` |
| Declared environments, with reused declarations | `1.7.0` |

Legacy language JSON is unchanged. Check each requested module's status and omissions before treating its results as complete. A partial report may still have exit status 0; worker failures and deadlines return an error.

Every finding includes its detector, project root, and relative evidence paths. Results are deterministic across worker counts, and empty collections are arrays rather than `null`. Generated manifests and selected CI configuration files can reach hooks without contributing to language totals. Detector hooks exclude vendored dependency trees unless attributes override that exclusion.

Built-in detectors recognize common Go, npm-compatible, Python, Cargo, Maven, Gradle, Bundler, Composer, and NuGet manifests. Selected frameworks are identified from dependencies declared in `package.json`, `composer.json`, and direct Python requirement lines. Project-root and CI/container findings help choose tools and working directories. The optional project mapper adds declared reference edges and target-presence checks. It does not evaluate effective build membership, restore dependencies, or resolve arbitrary build code.

Compiled detectors implement [profile.Detector](pkg/profile/types.go) and are supplied through `scanner.Options.Detectors`. Hooks receive a bounded, immutable file view, the detected language, and whether it contributes to statistics. Hooks must be concurrency-safe and must not execute repository code. A detector error produces a warning; valid accompanying findings are retained.

```text
Committed Git tree or directory
              |
     bounded file-worker pool
              |
   metadata inventory + attributes
              |
              +-- optional discovery, rules, and registry declarations
              +-- Enry language totals and detector findings
              +-- optional scc counts
              +-- optional project declarations, composition, and graphs
              +-- optional native worker: one parse, syntax and metrics
              |
   deterministic text or JSON report
```

Portable maps and aggregate profiles have separate saved-report comparison
commands:

```sh
dircue map compare before-map.json after-map.json --json
dircue compare before.json after.json --json
```

`map compare` uses stable map node and edge IDs and keeps indeterminate removals
separate when head coverage is incomplete. The older top-level `compare`
command uses aggregate reports such as those from `analyze all --json`.
Comparison reports distinguish observation changes from changes in provider or
selection policy; incomplete coverage limits what absence can establish. The
caller chooses the pair, and dircue does not verify repository identity. Valid
comparisons exit zero even when observations differ. See the [map
guide](docs/MAP.md#compare-saved-maps) and [aggregate saved-report
comparison](docs/COMPARISON.md).

## Attributes and boundaries

Dircue is configured through CLI flags and `.gitattributes`; there is no dircue YAML configuration file. For example, these opt-in overrides include XML and generated Java in language statistics:

```gitattributes
*.xml linguist-detectable=true
generated/**/*.java linguist-generated=false
```

Root and nested `.gitattributes` support language, vendor, generated, documentation, detectable, and LFS attributes, with scoped precedence, macros, common Git glob syntax, and reset semantics. Git source uses committed attributes and supported local `info/attributes` overrides. The pinned Ruby/Rugged reference's quoted-pattern behavior is reproduced for Git source; flat directories support quoted patterns as an explicit extension.

An analysis accepts at most 10,000 compiled attribute rules across its scopes. Exceeding that budget fails explicitly. This limit is an intentional difference from Linguist.

The [conformance scope](tests/conformance/COVERAGE.md) and [documented differences](tests/conformance/DISCREPANCIES.md) identify the Git configuration and revision behavior covered by tests, along with known exceptions.

Dircue reads source and Git objects without invoking project hooks, package managers, Git executables, or build scripts. Directory reads use `os.Root`; normal traversal excludes symlinks and special files. Unix reads additionally reject final-component symlinks and use nonblocking opens to prevent FIFO substitutions from hanging workers. Use a stable checkout: neither filesystem mode nor local Git metadata is an atomic snapshot of an actively modified directory.

Read failures and invalid arguments fail with a nonzero exit status by default. `--on-error continue` permits partial results for recoverable per-file reads; cancellation, invalid worker responses, and source-level failures remain fatal. Attribute files beyond the rule budget are omitted with warnings. Some bounds instead produce skipped or partial reports with exit status zero; check the requested module’s status and coverage as well as the process result. Warnings are emitted to stderr and included in full JSON reports. Use JSON for pipeline ingestion: text output escapes control characters in filenames; JSON retains their exact values. Check warnings before deciding whether a profile is sufficient for subsequent analysis.

Structural analysis executes only the worker path explicitly supplied by the user. It runs one worker at a time, with an 8 MiB maximum source input and a per-file deadline. The worker is separate from the portable Go binary. For shared runners, the [resource-budget guide](docs/RESOURCE_BUDGETS.md) describes external container limits and measured behavior under CPU and memory constraints.

Bounded content buffers do not impose a hard total-memory limit. Git delta reconstruction, metadata, findings, and optional file lists consume additional memory. Embedding callers can cancel through context; the CLI handles interrupt and termination signals. Use container CPU, memory, and wall-clock limits where needed.

## Docker and release artifacts

Build a local image from the tagged source and run it with the network denied and the source mounted read-only:

```sh
docker build --build-arg VERSION=1.0.0 -t dircue:1.0.0 .
docker run --rm --network none \
  -v /path/to/checkout:/repo:ro \
  dircue:1.0.0 map --json /repo
```

The runtime image contains the binary and license notices, and runs as an unprivileged user. Mounted source must be readable by that user; an explicit `--user` can match your pipeline's source permissions.

From a clean committed checkout, choose fresh output directories to prepare Linux/macOS/Windows archives, wheels, checksums, and build provenance locally:

```sh
python3 scripts/release.py --version 1.0.0 --output dist/release-1.0.0
python3 scripts/wheels.py --release-dir dist/release-1.0.0 --output dist/wheels-1.0.0
```

These commands do not publish anything. Wheels package the same Go binaries as the archives and need Python 3.10+ for their launcher. The Docker image and wheels do not include the structural worker; prepare that add-on separately using the [worker packaging instructions](docs/STRUCTURE.md#building-the-add-on).

## GitHub Releases

GitHub Releases provide the standalone archives, wheels, checksums, and build provenance for each tagged version. `uv` can install a compatible wheel from a local file or a GitHub Release URL; the wheel's launcher only invokes the bundled Go binary and does not download anything at runtime. See the [distribution guide](docs/DISTRIBUTION.md) for the platform matrix, offline use, and authentication for private or draft assets.

The 1.0.0 release does not include a PyPI publication step. If a future release adds one, its CHANGELOG entry will announce it and the distribution guide will document the `uvx dircue@<version>` and `uv tool install 'dircue==<version>'` commands.

## Troubleshooting

| Symptom | Check or fix |
| --- | --- |
| `dircue: command not found` | Add the installation directory to `PATH`, or call `./bin/dircue`. |
| Recent edits are missing from the report | Repository roots use committed `HEAD`. Use `--source directory` to inspect working files. |
| A large repository returns empty language statistics | Check stderr for the tree-size warning and set `--tree-size` above the entry count. |
| A map exits `0` but says `partial` | Exit status confirms that a valid map was produced. Inspect each `coverage` entry and its reasons before relying on absence. |
| An attached report has `binding: unknown` | The report and selected source lack comparable snapshot identity. See [source selection and binding](docs/MAP.md#source-selection-and-binding); do not treat path association as immutable-snapshot proof. |
| `map locate` reports `unresolvable_uri` | Supply `--source-uri` when SARIF uses absolute artifact URIs, and confirm that the URI is inside that root. |
| Docker cannot read mounted source | Check file permissions and use `--user` to select a suitable UID/GID. |
| uv cannot find dircue on PyPI | Releases are not published to PyPI. Install a compatible wheel directly from the GitHub Release URL, or point `uv` at a local wheel; see the [distribution guide](docs/DISTRIBUTION.md). |

## Verification

Keep development PRs in draft for lightweight CI. Marking a PR ready runs the full platform and conformance suites; later commits on a ready PR rerun them. See the [CI guide](docs/CI.md) for local checks, runner selection, and release preparation.

The [0.6.0 candidate report](docs/releases/0.6.0-validation.md) records 278 compatibility cases, format and hotspot evidence, and measured default and opt-in costs against 0.5.0.

The [0.5.0 candidate report](docs/releases/0.5.0-validation.md) records declaration and comparison coverage, compatibility checks, and measured default and opt-in costs against 0.4.0. Its source-bound evidence distinguishes local validation from the separate release packaging gates.

The [0.4.0 candidate report](docs/releases/0.4.0-validation.md) separates final-candidate checks from earlier experiments and outstanding release gates. It links the 209-case comparison with 0.3.0, optional-module validation, and measured costs. Results below retain the versions and inputs they originally tested.

The 0.3 candidate adds [project-map validation](tests/projects/README.md) on Roslyn, ASP.NET Core, Spring Framework, and Apache Maven, plus schema checks covering combined reports. The [production structural check](tests/structure/README.md) compares 150 Java/C# files with the pinned worker and preserves known grammar limitations. The [structural breadth harness](tests/structural_breadth/README.md) adds small fixtures for every enabled parser; these do not provide equivalent real-project coverage for every language. [CLI compatibility](tests/compatibility_v030/README.md) compares existing invocations with the shipped 0.2.0 binary; [performance and scale checks](tests/performance_v030/README.md) record the cost of project mapping. The [structural prototype](prototypes/structural/README.md) records parse reuse and offline execution. These checks have a different scope from the historical language and scc benchmarks below; they do not establish that structural parsing has the same cost as language classification.

The 0.2 metrics validation compared 910 committed files from Spring Framework, Roslyn, and ASP.NET Core against native Git bytes and standalone scc. Fixtures cover Java, C#, scope overrides, and a 1,100 MiB XML file. All five [candidate CI jobs](https://github.com/war-and-code/dircue/actions/runs/35128787043) passed, including Linux, macOS, Windows, and both conformance suites. See the [metrics validation](tests/metrics/results/README.md) for counters, performance measurements, source identities, and limitations.

Note that this project was renamed from **auragaze** to **dircue** before public release. Archived benchmark and validation records retain the former name, original source revisions, and executable hashes. They describe the pre-rename candidate; the rename validation is recorded separately in [the rename notes](docs/RENAME.md).

The 0.1 release candidate matched Linguist 9.7.0 language totals and file breakdowns on all 11 pinned public projects. In the recorded Linux arm64 Docker run, median execution was **5.38–14.66× faster**: **9.99×** for Spring Framework, **7.05×** for Roslyn, and **8.21×** for ASP.NET Core. These are measurements of those checkouts and commands, not a guarantee for every repository or host. Peak memory was higher on several large projects; Roslyn used about 345 MiB versus Linguist's 223 MiB.

See the [final public-project evidence](tests/performance/results/final/README.md), [final scale evidence](tests/stress/results/final/README.md), and [release validation](tests/release/results/final/README.md) for raw measurements, source identities, checks and limitations. Historical RC1 results remain available separately.

The maintained classifier's `GetLanguage` calls were **1.64× faster** on full sample contents and **1.71× faster** on 128 KiB prefixes than upstream Enry v2.9.6 in the controlled library comparison. The [Enry comparison](tests/enry-performance/results/final/README.md) also records CLI timings, differing language policies, and memory costs. Three comparisons against the published Enry CLI were inconclusive, and many scenarios produce different outputs under Enry's defaults.

```sh
go test -race ./...
go vet ./...
```

[Conformance](tests/conformance/README.md) compares the actual pinned Ruby CLI, including failures and intentional extensions. [Upstream sample results](tests/conformance/results/samples.md) compare language classifiers. The [performance harness](tests/performance/README.md) compares identical committed public checkouts, requires exact language output before timing, and records raw measurements and environment details.

Java and C#/.NET receive explicit coverage through Spring Framework, Roslyn, ASP.NET Core, and focused Maven/Gradle/MSBuild fixtures. The [scale suite](tests/stress/README.md) adds a fully written 2 GiB ETL-pipeline-shaped checkout, a 1.1 GiB XML log, and 2,048 interconnected `.csproj` files. It exercises both loose and packed Git objects, bounded delta history, Git-free views, generated-code overrides, and tree-size boundaries. ETL-pipeline and XML-log fixtures are synthetic and not verified exports from any vendor. The 0.3 project mapper now reports declared project-reference edges. Directory attribution and target-presence checks remain distinct from evaluated build membership or dependency resolution.

XML is normally excluded as a data language. The [attribute examples](#attributes-and-boundaries) show how to include XML or generated Java when that suits your pipeline.

These checks establish behavior for the recorded inputs. Re-run the comparison when changing the classifier, language data, source selection, or Git dependencies.

## FAQ

**Can I replace Linguist in an existing job?** Yes. Use the supported flags and path with `dircue`. Check the [documented differences](tests/conformance/DISCREPANCIES.md), especially if your job uses a Linguist version other than 9.7.0.

**Do I need Go, Ruby, or Git installed?** The core executable needs none of them. Building it needs Go; a wheel's launcher needs Python. Optional structural analysis needs a prebuilt native worker. Building that worker also needs Rust and its native build tools.

**Can it profile an extracted archive?** Yes. Point it at the extracted directory; Git metadata is optional.

**Will it pick up upstream detection improvements?** The maintained fork has a reproducible [update procedure](third_party/README.md). Updates are pinned and compared against Linguist before adoption; scans do not download rules.

## Contributions

Bug reports and design proposals go through [GitHub Issues](https://github.com/war-and-code/dircue/issues) with the templates the repository ships. Outside pull requests are not accepted at present. See [CONTRIBUTING.md](CONTRIBUTING.md) for reporting guidance and [SECURITY.md](SECURITY.md) for private vulnerability reporting.

For a classification mismatch, include the dircue version, command, expected result, and a small reproducible example you can share.

## Full license notice

[MIT](LICENSE). The maintained Enry fork retains Apache-2.0 licensing and Linguist's MIT data notices. The scc library uses the MIT license. [Third-party notices](THIRD_PARTY_NOTICES.md) cover the Go executable and embedded MIME database. The optional structural worker includes BCA under MPL-2.0 and Tree-sitter and grammar dependencies under their respective licenses; its separate archive includes dependency sources, licenses, and provenance. See [worker redistribution](docs/STRUCTURE.md#dependencies-and-redistribution).

The optional Bend research models include [modified Apache-2.0 proof examples](research/bend-aggregation/topk/THIRD_PARTY_NOTICES.md). They are kept separately from the released executables.
