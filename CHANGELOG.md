# Changelog

## 0.9.0 (unreleased)

- Match pinned Linguist 9.7.0 candidate narrowing for filename and extension
  strategies, defer its 23 generic extensions to later strategies, and limit
  content heuristics to the first 50 KiB. These corrections intentionally change
  affected language labels and totals, including Perl scripts and headers in
  the Linux kernel. Generate generic-extension rules from upstream data rather
  than a separate maintained list. Verify the classifier's existing 50 KiB
  window with a regression test. Metadata-only discovery retains its filename
  hints; those hints do not claim content-confirmed language detection.
- Treat pattern-only `.gitattributes` lines as silent no-ops, accept Cargo's
  edition-aware inherited `default-features` forms with precise diagnostics,
  and report added or removed languages when a warning-free comparison input
  proves the language population ran. Present compound comparison IDs as
  readable quoted components in text while retaining their JSON identities.
- Enforce event-only GitHub Actions triggers and full commit-hash pins for
  external Actions with a lightweight CI contract test.
- Publish a [proposed 1.0 compatibility policy](docs/COMPATIBILITY.md) for
  discussion. The legacy Linguist CLI remains a compatibility target; whether
  existing `analyze` reports become a frozen legacy profile in 1.x remains an
  owner decision under [#81](https://github.com/war-and-code/dircue/issues/81).
  The proposal covers JSON schema families and their `schema_version` semantics,
  exit statuses, and boundary guarantees. Publish a [security policy](SECURITY.md), a
  [contribution guide](CONTRIBUTING.md), issue templates, and a pull request
  template. Rewrite the README front matter to lead with a quick example
  and a real install story (release archives with checksums, clone-and-build,
  Python wheel from a GitHub Release URL; `go install` is unsupported because
  go.mod carries replace directives for the maintained forks). Document the current boundaries
  (`--source auto` fallback disclosure, absent-focus-query, registry adapter
  coverage, environment adapter breadth, structural worker isolation) in
  [docs/CAPABILITIES.md](docs/CAPABILITIES.md#known-boundaries-at-10) and
  cross-link from the relevant module docs.
- Move the CLI ergonomics evidence tree from `agent_ergonomics_audit/` (a
  repository-root directory) to `tests/ergonomics_v100/`. Redact captured
  absolute home paths in the audit transcripts. No shipped module reads from
  the audit tree; the regression harness continues to work against a
  caller-supplied dircue binary.
- Add an offline CLI guide, a command and option catalog, and standalone JSON
  Schema export through explicit `capabilities` views. Keep the original planner
  descriptor and existing analysis output contracts.
- Improve command help, bounded spelling hints, saved-report diagnostics, and
  plain-text plans. Suggested corrections never execute automatically; plans
  remain inert argument templates requiring caller revalidation.
- Escape terminal controls in final CLI errors while preserving underlying
  error causes and ordinary diagnostic text. Retain causes behind privacy-safe
  saved-report errors, and publish finite option choices in the CLI catalog.
  Explain which inherited flags framework help ignores. Reject invalid
  explanation error policies and explicitly empty tree selectors before scanning.
- Correct the directory-language schema to accept the existing `"NaN"` string
  percentage for attributed zero-byte files, without changing language output.
  Reject undefined percentages for nonempty entries and numeric values outside
  the emitted 0–100 range.
- Accept the existing `focus_scope_partial` metrics marker when validating saved
  profiles, so partial focused reports can be read back for explanation.
- Reuse at most eight packfile descriptors per Git snapshot and close them on
  success, failure and cancellation. [Paired measurements](tests/performance/v100_preparation/OPTIMIZATION.md)
  show 20–43% lower elapsed time on five selected Git workloads; the XML directory
  control is unchanged. Preserve classification, output and source-selection
  behavior. Fix a maintained go-git descriptor leak on cache insertion failure.
- Preserve the directory tree-size preflight and per-file regular-file check.
  Add regressions for unreadable attributes, detector calls on oversized trees,
  and symlinks to regular files; reject proposed optimizations that weakened
  these behaviors.
- Honor `--on-error continue` uniformly. Previously a single unreadable manifest
  aborted the whole `analyze all` run even under `continue`, because formats,
  declarations, registries and the environments `global.json` selection
  returned plain read errors. Under `continue` the unreadable file is now a
  per-module omission (`file_read_error`) plus the same-shaped top-level
  warning, the module reports `partial`, and the remaining modules keep their
  evidence. Under `continue` only, a per-file structural worker timeout or
  non-zero exit becomes a `structural_timeout` or `structural_worker_failure`
  omission instead of ending the scan. Cancellation, worker startup failures,
  and protocol violations remain fatal. Recoverable worker warnings omit raw
  worker stderr. Cache failed global.json selections once per analysis and
  deduplicate file-read warnings without quadratic scans.
- Preserve Linguist-compatible `--tree-size` edge behavior: nonpositive values
  behave as 1 for directory scans, while single-file inspection ignores the
  directory-only limit.
- Suggest nearby values for finite option choices (`--source`, `--on-error`,
  `--metrics-scope`, `--module`), two-edit typos of longer command names, and
  an unknown first token that is not a path. Give `analyze explain --report` the
  same unreadable-report hint as `plan` and `compare`. Name the object kind
  when `--tree` resolves to a commit or blob. Advertise the `--guide`, `--cli`
  and `--schema` capability views through explicit help, record signal
  termination (SIGPIPE on a closed stdout) truthfully in
  the CLI catalog exit codes, and add a catalog drift test so a new flag cannot
  ship without its documented semantics.
- Report a `global.json` accepted through the JSONC path (comments, trailing
  commas) with a `global-json-lenient-syntax` diagnostic. Allow complete environment
  coverage only when all upstream gaps are independently resolved strict-JSON
  rejections; retain partial status for omitted or malformed project requirements.
  Consider only `global.json` files that are
  nearest ancestors of a modeled .NET invocation context, so a Jekyll or Hugo
  `_data/global.json` no longer receives a .NET diagnostic.
- Add fuzz targets with seed corpora for the gitattributes parser, the
  `global.json` and `requires-python` readers, saved-report planning input and
  the capability descriptor, asserting bounded output and determinism.

## 0.8.0 (2026-09-21)

- Add opt-in environment requirement observations from supported project declarations, with bounded .NET `global.json` selection for modeled project-directory invocation contexts. Keep SDK selection, target frameworks, advisory toolchains and unresolved declarations distinct; do not probe installed tools or run builds.
- Add offline follow-up planning from a saved profile and explicit caller questions or modules. Bind decisions to report bytes and source evidence, expose a machine-readable capability registry, and emit inert argument templates with prerequisites and coverage qualifications.
- Extend offline comparison to focused project populations, separate line metrics, affected-project observations, and source-availability evidence. Changed scope, policy and incomplete coverage qualify differences; environment and explanation comparison remain explicitly unsupported.
- Use aggregate schema 1.7.0 only when environment evidence is requested. Existing profiling commands retain their schemas and process contracts.

## 0.7.0 (2026-09-21)

- Add focused .NET and Python/uv project profiling with original-root attribute context, qualified ownership boundaries, and explicit shared declarations. Count primary and explicitly selected related projects separately with optional scc metrics.
- Add bounded source-availability observations for Git LFS pointers, Gitlinks, submodule declarations, and supported local sparse-checkout metadata, without fetching or hydrating content.
- Add fresh language decision traces and bounded explanations of retained project, focus, and availability evidence. Saved-report queries never reopen source paths.
- Use aggregate schema 1.6.0 only for the new capabilities. Preserve existing command schemas; reject new targeted reports in comparison until their population semantics are supported.
- Isolate ordinary draft CI concurrency from full validation, preserving inexpensive draft checks.

## 0.6.1 (2026-09-20)

- Reduce allocation in bounded file reads using capped size hints, while preserving read limits and handling files that grow or shrink. [Measurements](tests/performance/bounded_reader/README.md) record workload-specific speed gains and memory tradeoffs.
- Strengthen aggregation tests with independent full-population references and mutation checks. Add optional checked Bend models with explicit proof boundaries; Bend is not a runtime or ordinary CI dependency.
- Document [design principles](docs/DESIGN_PRINCIPLES.md) and the distinction between analysis scope, execution preferences, and externally enforced resource limits.

## 0.6.0 (2026-09-20)

- Add explicit bounded format inspection for mixed directories, with filename hints, signatures, parsed prefixes and complete supported syntax checks kept distinct. Include vendor/data files, selected-source provenance, read accounting and omissions without archive expansion or content execution.
- Add optional function hotspot distributions using the existing BCA parse. Compute exact fixed-bin counts and top-ten evidence before retention, separated by language/grammar and clean/recovered syntax. Preserve nested-space overlap, bounded names/paths and missing populations.
- Extend offline comparison to format observations and qualified hotspot distributions. Ranking changes do not establish function additions or removals.
- Fix saved-report import for per-file structural provider metrics. Existing structural reports can now be loaded without rejecting their embedded JSON objects.
- Use aggregate schema 1.5.0 only for the new modules; retain existing output contracts. Probe hotspot worker capabilities explicitly, including on empty scans.
- Extend native release and installed-wheel verification with the new capabilities while retaining source and artifact identity checks.

## 0.5.0 (2026-09-20)

- Add opt-in project declaration profiling for npm, Go, Python/uv and Cargo, alongside the existing .NET/Maven/Gradle readers. Report manifest identities, workspace membership, supported local relationships, requirements and named interfaces with their evidence and limitations. Script bodies are withheld; build logic is not executed.
- Add `analyze declarations` for bounded manifest reads without classifying unrelated file contents, and `analyze all --declarations` for combined profiling. Supported manifests have a separate population from language statistics; installed `node_modules` manifests are excluded.
- Add offline `compare` for two saved aggregate reports. Compare supported observations by module, separate provider and policy differences, and qualify incomplete coverage or missing provenance. Source directories and evidence paths are not opened; valid differences retain exit status zero.
- Compare project/workspace declarations, requirements, interfaces, language/content observations, registry and caller-rule observations, imported package evidence, and aggregate/per-file line metrics. Detailed structural/function and graph comparison remain unsupported; renames are not inferred.
- Use aggregate schema `1.4.0` only when declarations are requested, plus separate comparison schema `1.0.0`. Existing profiling commands keep their prior JSON and process contracts.
- Require five additional packaged-core declaration/comparison smoke receipts for release assembly from 0.5.0 onward. Verify executable and fixture identities, expected manifest facts, unchanged defaults, offline comparison, malformed inputs and qualified coverage; retain earlier-version gates unchanged.

## 0.4.0 (2026-09-20)

This release extends the optional profilers while preserving existing command
contracts. The [validation report](docs/releases/0.4.0-validation.md) records candidate
evidence. The [release preparation](https://github.com/war-and-code/dircue/actions/runs/35478628605)
passed all seven jobs and verified all 39 assets before publication.

- Add explicit metadata discovery that includes regular files outside language statistics, with candidate manifest/artifact hints and declared omissions.
- Add static .NET project-graph components, cycles, and degrees while keeping conditional and unresolved references separate.
- Import existing native Syft JSON through `analyze packages --syft-report FILE`, with bounded parsing, credential redaction, explicit coordinate mapping, and caller-provided source binding. Importing does not execute Syft.
- Add caller-supplied observation rules through `--rules-file`, with deterministic metadata/content matching, an exact policy digest, hashes for matched content, selected-source metadata, and coverage limits. Repository files cannot enable rules automatically or disable other modules.
- Add explicit bounded NuGet/npm package-source declarations, with sanitized origins, syntax qualification, deterministic selection, and no package-manager execution.
- Add optional BCA function-space metrics with source spans and hashes. Reuse the native parse, preserve nested-metric semantics, and qualify bounded evidence rather than assigning quality grades.
- Reduce redundant JSON traversal in function-response decoding. [Retained measurements](tests/functions/decode-performance/RESULTS.md) show a 26.3% median decoder-time reduction for the 128-entry fixture; this is not a whole-scan or peak-memory claim.
- Reuse lazy canonical field tables in the optional Syft importer. The [20,000-package fixture](tests/packageevidence/field-table-performance/README.md) allocates 6.41 MiB less per import (1.65%); measured latency remains within the noise threshold. A separate capacity experiment was rejected after increasing memory on malformed inputs.
- Use aggregate schema 1.3.0 only when a new optional module is requested. Existing command output and exit contracts remain regression gates; the integrated development binary matches 209 retained 0.3.0 cases.
- Add a manual workflow for verified draft-release assembly, including packaged native smoke checks and artifact provenance. It does not publish to PyPI or change repository visibility. The five native platform jobs passed in both the release-candidate rehearsal and final release preparation; the published assets matched the verified draft assets.
- Document measured container CPU/memory behavior, conservative evidence interpretation, and the limits of each optional module.

## 0.3.0 (2026-09-17)

- Add opt-in project mapping through `analyze projects` and `analyze all --projects`, including .NET/Maven declarations, conservative Gradle observations, and broader manifest discovery.
- Report declared project and solution relationships, reference target presence, unevaluated conditions, configuration candidates, and ambiguous or unassigned directory attribution.
- Describe selected content by source, test, configuration, generated, vendor, documentation, data, binary, and unknown categories with the basis for each classification.
- Add optional structural analysis across 20 source languages through `analyze structure` and `analyze all --structure`. A separate native worker uses one BCA-owned Tree-sitter parse per file for syntax observations and BCA metrics. Java/C# additionally receive custom declaration counts; unavailable declaration fields remain absent for other languages.
- Report structural omissions, syntax recovery, parser versions, and per-file metrics. Limit worker admission to one process, bound source/output sizes, and enforce a per-file deadline.
- Add native-worker packaging with pinned dependency sources, license notices, checksums, and runtime provenance. The core binary and Python wheels remain usable without this add-on.
- Use aggregate schema 1.2.0 when projects or structure are requested. Existing language JSON, plain `analyze all`, and metrics-only schema 1.1.0 retain their contracts.
- Document staged analysis with a tested report consumer that suggests independent follow-ups and preserves incomplete or unknown evidence for review.

Project mapping reads declarations without running build tools or restoring packages. Structural analysis has documented grammar and per-language observation limits; it does not perform compiler type resolution or construct cross-file call graphs.

## 0.2.0 (2026-09-16)

- Add opt-in scc code metrics through `analyze metrics` and `analyze all --metrics`, with code, comment, blank, and total lines, bytes, file counts, and lexical complexity estimates.
- Report metrics by language and immediate parent directory, with optional per-file rows and explicit coverage and skip reasons.
- Count source files by default, with a broader text scope and bounded full-file reads. XML logs remain excluded from default counting unless attributes include them.
- Extend the aggregate JSON schema to 1.1.0 when metrics are requested. Existing language output and reports without metrics retain their contracts.
- Backport a go-git streaming delta fix: backward copies could reconstruct incorrect bytes from packed Git objects without reporting an error. The fix preserves bounded reads and applies to both language profiling and metrics.
- Close Git object and linked-worktree metadata files on success and error paths, preventing descriptor leaks and Windows cleanup failures.

## 0.1.0 (2026-09-08)

First release as dircue, renamed from auragaze before publication. Language profiling targets GitHub Linguist 9.7.0.

- Portable Go CLI with legacy language output, JSON/breakdown/strategy flags, single-file metadata, and Git revision selection.
- Committed Git-tree analysis by default at repository roots, plus arbitrary directories and explicit working-directory mode.
- Nested Linguist attributes, bounded concurrent classification, generated/vendor/documentation filtering, and language grouping.
- Maintained Enry compatibility fork with pinned Linguist data, centroid classification, pure-Go tokenizer, regeneration provenance, and license notices.
- Ecosystem, framework, and layout detectors with a versioned aggregate JSON schema.
- Differential checks against the actual Ruby CLI and upstream samples, pinned public-repository comparisons, cross-platform builds, and an unprivileged minimal Docker image.
- Java/.NET coverage and synthetic GiB-scale Talend/XML, interconnected project, packed-object, and 100,000-file cases, including regressions for binary-prefix detection and tree-size cutoffs.
- Local release packaging for Linux, macOS, and Windows, plus Python wheels containing the same binaries for installation with uv or pip.

Compatibility results apply to Linguist 9.7.0 and the recorded inputs. See the [conformance results](tests/conformance/results/latest.md), [classifier results](tests/conformance/results/samples.md), [final performance results](tests/performance/results/final/README.md), and [documented differences](tests/conformance/DISCREPANCIES.md). Attribute/resource limits, refusal to inspect symlink files, and arbitrary-directory support are intentional differences. Future Linguist releases require renewed comparison.

The [large-input methodology](tests/stress/README.md) distinguishes synthetic scale evidence from the real Java and .NET projects in the public corpus.
