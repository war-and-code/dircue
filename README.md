# `dircue`

Profile source code repos and other directories of computer content.

Dircue identifies languages, maps declared projects and their relationships, and describes the contents of unfamiliar directories. Its Go binary works with committed Git trees or ordinary files, without running their build scripts.

Version 0.3.0 adds project mapping and declared build requirements, content composition, and optional structural analysis across 20 languages. Language statistics and [scc](https://github.com/boyter/scc) line counts remain available through the existing commands. Deeper parsing uses a separate native worker built on [big-code-analysis](https://github.com/dekobon/big-code-analysis) and [Tree-sitter](https://tree-sitter.github.io/tree-sitter/).

```sh
dircue analyze all --json /path/to/checkout
dircue analyze projects --json /path/to/checkout
dircue analyze all --projects --metrics --json /path/to/checkout
dircue --breakdown --json /path/to/checkout
```

The repository is currently private, and PyPI publication is deferred. Authenticated repository users can download the [0.3.0 release archives and wheels](https://github.com/war-and-code/dircue/releases/tag/v0.3.0). A [distribution guide](docs/DISTRIBUTION.md) covers GitHub Releases, PyPI, and offline installation.

## Quick start

Build with **Go 1.26.6 or newer**, since that includes security fixes required by the filesystem boundary. Language profiling, project mapping, and scc metrics need only the binary for their OS and architecture. Structural analysis additionally needs its matching native worker.

```sh
CGO_ENABLED=0 go build -trimpath -o bin/dircue .
./bin/dircue --breakdown --json /path/to/checkout
```

On Unix, install the binary into a directory on `PATH`, then call it directly:

```sh
mkdir -p "$HOME/.local/bin"
install -m 755 bin/dircue "$HOME/.local/bin/dircue"
export PATH="$HOME/.local/bin:$PATH"
dircue --breakdown --json /path/to/checkout
```

`go run .` is useful during development, but execution beyond that should use the built binary.

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
| Additional profiling | Language metadata | Language metadata | Project declarations and references, content composition, framework/CI observations, optional scc metrics and structural analysis |

The recorded 0.1 release candidate had 5.38–14.66× faster median execution than Linguist on 11 pinned public projects, with matching language totals and file breakdowns. That Linux arm64 Docker run also recorded higher peak memory on several large projects.

[Verification section](#verification) here links the measurements, separate Enry comparison, and their limitations.

## Content selection

- At a Git repository root, analyze the committed `HEAD` tree. Dirty, untracked, and ignored working files do not affect the results. Symlinks and submodules are excluded.
- For a plain directory, analyze its filesystem contents. No Git executable, history, or metadata is required.
- Use `--source directory` to inspect current filesystem contents, including when Git metadata exists.
- Use `--source git` to require a Git repository and committed tree.

An initialized repository without commits falls back to directory analysis in auto mode. A requested subdirectory beneath a repository is scanned as that filesystem subtree. These cases extend Ruby Linguist, which requires a usable repository at the supplied directory path.

```sh
dircue /checkout --json
dircue --rev HEAD~1 --breakdown /checkout
dircue --source directory --json /exported-source
dircue --json /checkout/main.go
```

Single-file mode follows Linguist's separate inspection layout, including language, MIME, lines, nonblank lines, generated/vendor flags, and large-file status. As in Linguist, `--rev` applies to directory statistics; single-file inspection uses `HEAD` when available. Explicit symlink targets are rejected as a documented safety difference.

## CLI compatibility

Flags can appear before or after the path. With no path, the current directory is used. Use `--` to disambiguate a path named `analyze` or beginning with `-`.

| Flag | Behavior |
| --- | --- |
| `-j`, `--json` | Emit JSON. |
| `-b`, `--breakdown` | Include file paths in language results. |
| `-s`, `--strategies` | Show each file's detection strategy in text output. |
| `-r`, `--rev REV` | Select a Git revision for directory statistics; default `HEAD`. |
| `-t`, `--tree-size N` | Return empty statistics with a warning when the tree reaches this entry count; default 100,000. |
| `--source auto\|git\|directory` | Select the content source; default `auto`. |
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

`analyze structure` sends selected source in 20 supported languages to an explicitly selected worker. Each file is parsed once; the same Tree-sitter tree supplies syntax observations and BCA metrics. Java and C# also receive custom declaration counts; other languages expose syntax-node and recovery counts alongside BCA metrics. `--files` includes per-file metrics and parser provenance. Syntax recovery produces partial results, and unsupported inputs have omission reasons. There is no compiler type checking or cross-file call graph. See the [capability matrix](docs/CAPABILITIES.md) for language and ecosystem coverage, and the [structural analysis guide](docs/STRUCTURE.md) for grammar limitations, offline worker packaging, and resource bounds.

For staged workflows, start with `analyze all --projects --source directory --json`, then choose additional work from the evidence and coverage. The [staged-analysis guide](docs/STAGED_ANALYSIS.md) includes an executable report consumer and explains why empty language totals or XML-heavy content alone are insufficient reasons to skip follow-ups.

Plain `analyze all` retains its existing behavior. Add `--projects`, `--metrics`, or `--structure` for the modules you need. Structural analysis requires `--structural-worker`; it never downloads a parser during a scan.

The [roadmap](docs/ROADMAP.md) tracks proposed discovery, entry-point and relationship mapping, reusable analysis context, and explainable complexity hotspots. These are future extensions of the general-purpose profiler; the [capability matrix](docs/CAPABILITIES.md) describes what is available today.

The development branch after 0.3.0 adds these explicit options:

- [`analyze discovery`](docs/DISCOVERY.md) inventories regular-file metadata without reading source payloads. It includes filename hints for manifests and packaged artifacts, including paths excluded from language statistics.
- [`analyze graph`](docs/GRAPH.md) derives .NET project-reference components, cycles, and degrees from static declarations, keeping conditional and unresolved edges separate.
- [`analyze packages --syft-report FILE`](docs/PACKAGE_EVIDENCE.md) imports an existing Syft JSON report. Coordinate mapping and source binding are explicit; it does not execute Syft.
- [`analyze structure --functions`](docs/FUNCTIONS.md) retains bounded function-space metrics with source spans, source hashes, and coverage. It reuses the worker's existing parse and requires a matching development worker.

These can be combined with `analyze all --discovery --graph --syft-report FILE`. They are not part of the downloadable 0.3.0 binaries. Existing invocations retain their output contracts.

| Requested output | Schema version |
| --- | --- |
| Existing aggregate report without optional modules | `1.0.0` |
| Metrics, without projects or structure | `1.1.0` |
| Projects or structure, with optional metrics | `1.2.0` |
| Discovery, graph, imported package evidence, or function metrics (development branch) | `1.3.0` |

Legacy language JSON is unchanged. Check each requested module's status and omissions before treating its results as complete. A partial report may still have exit status 0; worker failures and deadlines return an error.

Every finding includes its detector, project root, and relative evidence paths. Results are deterministic across worker counts, and empty collections are arrays rather than `null`. Generated manifests and selected CI configuration files can reach hooks without contributing to language totals. Detector hooks exclude vendored dependency trees unless attributes override that exclusion.

Built-in detectors recognize common Go, npm-compatible, Python, Cargo, Maven, Gradle, Bundler, Composer, and NuGet manifests. Selected frameworks are identified from dependencies declared in `package.json`, `composer.json`, and direct Python requirement lines. Project-root and CI/container findings help choose tools and working directories. The optional project mapper adds declared reference edges and target-presence checks. It does not evaluate effective build membership, restore dependencies, or resolve arbitrary build code.

Compiled detectors implement [profile.Detector](pkg/profile/types.go) and are supplied through `scanner.Options.Detectors`. Hooks receive a bounded, immutable file view, the detected language, and whether it contributes to statistics. Hooks must be concurrency-safe and must not execute repository code. A detector error produces a warning; valid accompanying findings are retained.

```text
Committed Git tree or directory
              |
     bounded file-worker pool
              |
   attributes + Enry classification
              |
              +-- language totals and detector findings
              +-- optional scc counts
              +-- optional project declarations and composition
              +-- optional native worker: one tree, two consumers
              |
   deterministic text or JSON report
```

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

Read failures, invalid arguments, and resource-policy violations fail with a nonzero exit status. Warnings are emitted to stderr and included in full JSON reports. Use JSON for pipeline ingestion: legacy text output preserves untrusted filenames verbatim, including unusual characters. Check warnings before deciding whether a profile is sufficient for subsequent analysis.

Structural analysis executes only the worker path explicitly supplied by the user. It runs one worker at a time, with an 8 MiB maximum source input and a per-file deadline. The worker is separate from the portable Go binary. For shared runners, the [resource-budget guide](docs/RESOURCE_BUDGETS.md) describes external container limits and measured behavior under CPU and memory constraints.

Bounded content buffers do not impose a hard total-memory limit. Git delta reconstruction, metadata, findings, and optional file lists consume additional memory. Embedding callers can cancel through context; the CLI handles interrupt and termination signals. Use container CPU, memory, and wall-clock limits where needed.

## Docker and release artifacts

```sh
docker build --build-arg VERSION=0.3.0 -t dircue:0.3.0 .
docker run --rm --network none \
  -v /path/to/checkout:/repo:ro \
  dircue:0.3.0 --breakdown --json /repo
```

The runtime image contains the binary and license notices, and runs as an unprivileged user. Mounted source must be readable by that user; an explicit `--user` can match your pipeline's source permissions.

From a clean committed checkout, choose fresh output directories to prepare Linux/macOS/Windows archives, wheels, checksums, and build provenance locally:

```sh
python3 scripts/release.py --version 0.3.0 --output dist/release-0.3.0
python3 scripts/wheels.py --release-dir dist/release-0.3.0 --output dist/wheels-0.3.0
```

These commands do not publish anything. Wheels package the same Go binaries as the archives and need Python 3.10+ for their launcher. The Docker image and wheels do not include the structural worker; prepare that add-on separately using the [worker packaging instructions](docs/STRUCTURE.md#building-the-add-on).

## GitHub Releases and PyPI

The primary distribution channels will be GitHub Release binaries and PyPI wheels for `uvx dircue@0.3.0` or `uv tool install 'dircue==0.3.0'`. These public commands will become available after publication. Local wheel preparation, offline use, platform requirements, and release steps are described in the [distribution guide](docs/DISTRIBUTION.md).

uv can also install a compatible wheel from a local file or a GitHub Release URL. We may not immediately publish to PyPI.

## Troubleshooting

| Symptom | Check or fix |
| --- | --- |
| `dircue: command not found` | Add the installation directory to `PATH`, or call `./bin/dircue`. |
| Recent edits are missing from the report | Repository roots use committed `HEAD`. Use `--source directory` to inspect working files. |
| A large repository returns empty language statistics | Check stderr for the tree-size warning and set `--tree-size` above the entry count. |
| Docker cannot read mounted source | Check file permissions and use `--user` to select a suitable UID/GID. |
| uv cannot find dircue on PyPI | Public packages are not yet available. Use a local compatible wheel as described in the [distribution guide](docs/DISTRIBUTION.md). |

## Verification

Keep development PRs in draft for lightweight CI. Marking a PR ready runs the full platform and conformance suites; later commits on a ready PR rerun them. See the [CI guide](docs/CI.md) for local checks, runner selection, and release preparation.

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

Java and C#/.NET receive explicit coverage through Spring Framework, Roslyn, ASP.NET Core, and focused Maven/Gradle/MSBuild fixtures. The [scale suite](tests/stress/README.md) adds a fully written 2 GiB Talend-shaped checkout, a 1.1 GiB XML log, and 2,048 interconnected `.csproj` files. It exercises both loose and packed Git objects, bounded delta history, Git-free views, generated-code overrides, and tree-size boundaries. Talend and XML-log fixtures are synthetic; they are not verified Talend exports or MOVEit samples. The 0.3 project mapper now reports declared project-reference edges. Directory attribution and target-presence checks remain distinct from evaluated build membership or dependency resolution.

XML is normally excluded as a data language. The [attribute examples](#attributes-and-boundaries) show how to include XML or generated Java when that suits your pipeline.

These checks establish behavior for the recorded inputs. Re-run the comparison when changing the classifier, language data, source selection, or Git dependencies.

## FAQ

**Can I replace Linguist in an existing job?** Yes. Use the supported flags and path with `dircue`. Check the [documented differences](tests/conformance/DISCREPANCIES.md), especially if your job uses a Linguist version other than 9.7.0.

**Do I need Go, Ruby, or Git installed?** The core executable needs none of them. Building it needs Go; a wheel's launcher needs Python. Optional structural analysis needs a prebuilt native worker. Building that worker also needs Rust and its native build tools.

**Can it profile an extracted archive?** Yes. Point it at the extracted directory; Git metadata is optional.

**Will it pick up upstream detection improvements?** The maintained fork has a reproducible [update procedure](third_party/README.md). Updates are pinned and compared against Linguist before adoption; scans do not download rules.

## Contributions

GitHub Issues are welcome. The project does not currently accept outside pull requests or larger contributions. Please submit bug reports and proposals through [Issues](https://github.com/war-and-code/dircue/issues).

For a classification mismatch, include the dircue version, command, expected result, and a small reproducible example you can share.

## License

[MIT](LICENSE). The maintained Enry fork retains Apache-2.0 licensing and Linguist's MIT data notices. The scc library uses the MIT license. [Third-party notices](THIRD_PARTY_NOTICES.md) cover the Go executable and embedded MIME database. The optional structural worker includes BCA under MPL-2.0 and Tree-sitter and grammar dependencies under their respective licenses; its separate archive includes dependency sources, licenses, and provenance. See [worker redistribution](docs/STRUCTURE.md#dependencies-and-redistribution).
