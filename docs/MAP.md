# Directory maps

`dircue map` produces a portable description of a selected Git tree or ordinary directory. It combines language populations, content roles, declared projects, local project relationships, deployable definitions, interfaces, capabilities, and explicit coverage in one bounded scan. The command does not build the project, restore packages, run inspected code, contact a network service, or invoke another analyzer.

```sh
dircue map --json /path/to/checkout > map.json
dircue map --summary /path/to/checkout
```

JSON is the default when stdout is redirected. A terminal receives the compact summary unless `--json` is present. `--summary` forces the text view. The JSON document is the automation contract; the summary deliberately omits evidence, stable IDs, facts, and most coverage details.

## What the map represents

The top-level `source` identifies the input snapshot. Every observation is a node, and every supported relationship is an edge between stable node IDs. Evidence records the path, basis, source kind, and dircue rule or external provider responsible for an observation.

| Node kind | Examples |
| --- | --- |
| `content` | Language populations and source, test, documentation, generated, configuration, data, binary, archive, certificate, and other content roles. |
| `component` | Supported projects and workspace members inferred from declarations. |
| `deployable` | Container, Compose, Kubernetes, Helm, Terraform, serverless, CI definitions, and Maven WAR/EAR archives recognized by the built-in observer. Maven `<packaging>war</packaging>` or `<packaging>ear</packaging>` in a pom.xml emits a deployable node of kind `archive` with provider `maven` and a `format` property (`war` or `ear`). The artifact name uses `<finalName>` when declared, or `${artifactId}-${version}.war` (Maven default) otherwise; coverage is `qualified` when static property expressions cannot be resolved. A `builds` edge connects the archive node to the co-located Maven component. |
| `interface` | Supported declared or syntax-observed interfaces: protobuf services and their RPC operations; OpenAPI and Swagger documents (`interface_kind: api_document`, named by `info.title`) and their `METHOD /path` operations (`interface_kind: operation`, at most 500 per document, with `operations_omitted` beyond that); AsyncAPI documents and GraphQL schemas (`api_document`); Noir endpoints; Go, Rust, npm and Python entry points; statically declared .NET `OutputType` application projects (`Exe`/`WinExe`, with MSBuild conditions and expressions retained); Spring Boot applications and main classes; Flask application objects (`interface_kind: flask_application`, detected from module-level `variable = Flask(...)` assignments outside test paths); and declared ports from Dockerfile `EXPOSE`, Compose `ports`/`expose` and Kubernetes `containerPort` (`interface_kind: declared_port`). Python `[project.scripts]` and `[project.gui-scripts]` remain distinct (`python-console-script` and `python-gui-script`) and retain their targets. |
| `capability` | Bounded code/configuration observations such as imported services. |
| `package` | Packages imported from an explicitly attached Syft report. |
| `tool_run` | Metadata and snapshot binding for an explicitly attached provider report. |

Relationships include containment and membership, local dependencies, build and run relationships, exposed interfaces, declared capabilities, package containment, and provider analysis. A relationship means only what its type and evidence establish. For example, a declaration can establish a local reference without proving that the project builds.

Dockerfile `ENTRYPOINT` and `CMD` instructions and Kubernetes CronJob schedule, `suspend`, and `timeZone` fields are retained as evidence-backed facts on their deployable nodes. Docker facts identify exec or shell form, the stage, and whether it is the final declared stage. The map does not resolve instruction ordering, overrides, or the effective command. Docker argv is sensitive input: the map emits only a fixed allowlist label for common launch tokens (and a small allowlist of Python `-m` module names), marks arguments withheld, and never copies arbitrary arguments into output. A token label does not verify that the named program is present in an image. A token outside the allowlist remains a declared launch fact with its value withheld. Malformed or empty exec vectors and multiline launch instructions are unresolved, with arguments withheld. Docker continuations are kept within their originating instruction and are not reparsed as opcodes. CronJob schedules are emitted only for bounded five-field cron syntax or known descriptors; this syntax check does not establish controller acceptance or execution. Missing or invalid schedules do not produce a schedule fact, and dynamic schedules remain unresolved without echoing template text. `suspend` is reported only for YAML booleans (dynamic values remain unresolved); timezone presence is reported with its value withheld, or unresolved when templated. These facts describe source declarations; they do not establish successful execution, effective scheduling, or a running service. YAML source spans are omitted for these facets because the YAML reader does not provide structurally bound field coordinates.

A Dockerfile normally links to the component at its directory. A statically recognized `COPY` source can instead attribute its `builds` edge to a Maven WAR/EAR component when the source path identifies the module and the source uniquely names that module's archive. A direct context copy must occur in the final build stage and survive later writes. For `COPY --from`, attribution also requires a surviving copy in the final stage, a local source stage, and a tracked path back to the module's `target/` artifact. Docker build-context `COPY` paths are relative to the build-context root. A nested Dockerfile does not establish where that context begins, so its `COPY` paths do not establish Maven module ownership. Stage paths are relative to that stage's filesystem root. The final stage is the one whose `FROM` appears last in the file. The bounded path trace follows static destinations and supported `WORKDIR` and two-operand `cp` forms. Any write that could replace the traced archive blocks attribution: a later `COPY` or `ADD` into the same path or a parent directory, a `COPY` with an unreadable source or destination, an unresolved path, or a `RUN` command outside a small vetted set. The vetted set is Maven lifecycle phases (any other word, such as the plugin goal `dependency:copy`, blocks; variables are expanded from static `ARG` and `ENV` defaults and inline assignments, and a variable with no static value, such as a build argument supplied at build time, is accepted unread), `mkdir`, `chmod`, `chown`, `rm` and `cp` on unrelated absolute paths, and OS package manager operations (`apt-get`, `apk`, `yum`, `dnf`), which are assumed not to write the build output being traced. A `RUN` command joined by `;`, `|`, `||`, a background `&`, redirection or command substitution is not vetted. A copied source ending in `/`, without an extension, or with a directory-like suffix such as `.d` is treated as a directory that may write anywhere below its destination. When an archive is copied to a destination without a trailing slash, both the file and the directory reading of that destination must survive. Only the Maven component in the POM's directory can own its archive. This attribution remains partial: it does not interpret build arguments, ignore rules, arbitrary shell commands, or Maven output. Ambiguous or unresolved sources do not establish an archive owner; the directory-based link remains when no other source establishes one. The `copy_source` and `copy_source_stage` fact kinds record context and stage copy sources, respectively; the archive relationship uses the reason `dockerfile_copy_source_matches_maven_archive`.

A local `build_context` is attributed to the component whose root is that directory. A `code_uri` or workflow `working_directory` may name a file or subdirectory (e.g. `target/app.jar`), so it is attributed to the nearest enclosing component root. The nearest root is authoritative: when it holds more than one component, no edge is emitted and the `deployable_reference` fact is `partial` with reason `ambiguous_component_root`, rather than falling back to a broader ancestor. A reference that resolves outside the repository is never attributed; its fact is `partial` with reason `path_outside_repository`. Workflow working directories resolve from the workspace. Below an `actions/checkout` `path:` made earlier in the same job, the directory names the repository path inside that checkout; when that checkout names a `repository:` it is not attributed (`named_repository_checkout`), because the map cannot tell offline whether that repository is this one. Any other directory the committed tree lacks is created at run time and is not attributed (`path_not_in_repository`); absence is only asserted when the content inventory saw every file.

Additional build-context links use literal Compose `build.context`/`dockerfile` pairs, explicit supported GitHub Actions `docker/build-push-action` contexts, and bounded root-Makefile `docker build -f FILE CONTEXT` declarations. These links remain partial. Actions contexts follow the earlier checkout path in the same job; named external checkouts and dynamic paths remain unattributed. The observer does not pair action inputs across steps or evaluate Make/shell expressions. A Makefile's `-f` path resolves from the repository root; Compose's `dockerfile` path resolves from its declared context. Nested Makefiles are omitted because their invocation directory is unknown. Literal local Terraform module sources can connect recognized Terraform module deployables; remote, duplicate, interpolated or ambiguous sources do not establish that relationship. A co-located Dockerfile can narrow to a unique Cargo component when copied crate paths and a literal Cargo build command corroborate it; this remains a static clue, not a verified build.

Maven system-scope `systemPath` and .NET `Reference`/`HintPath` declarations can record local JAR/DLL references. A selected regular file is linked by `references_artifact`, with an `artifact_reference` fact on its component. Its content type is a partial filename hint; dircue does not unpack the file, identify a package/version, or verify a managed assembly. Unresolved, missing or inventory-limited targets keep their status without copying the raw target into map output. Root-escaping and dynamic references remain unattributed; supported same-POM properties can resolve a Maven path. Project/declaration reports also retain these references in their existing `references` arrays. Maven XML readers accept declared ISO-8859-1 and US-ASCII as well as UTF-8 and BOM-marked UTF-16; unsupported or contradictory encodings remain diagnostics.

Cargo attribution from copied crate paths is limited to a root Dockerfile with
a known context. The copy and literal build must be in the same stage, with the
copy before the build when source lines are known. A conflicting manifest or
package selector prevents this narrowing. Surviving co-location edges retain
their IDs when their supporting evidence is strengthened.

A Python project with literal maturin `bindings = "bin"` has a `binary` interface with condition `maturin bindings=bin` pointing to its declared local Cargo manifest. This is packaging intent, not proof that the crate contains a runnable binary.

Paths are clean, root-relative paths. Node and edge IDs are deterministic hash-based identities derived from their kind, paths, and discriminator. They can be compared across maps of the same logical content, but an ID is not a content checksum.

Capability import evidence is a bounded source syntax signal. The scanner recognizes Go and Python imports, Java/Kotlin `import` lines, C# `using` and VB `Imports` directives, and TypeScript/JavaScript ES imports and conservative CommonJS `require` calls. The bounded lexical scanners mask comments and treat strings as opaque, accepting string tokens only in import position; they recognize only catalogued package namespaces; unsupported or malformed syntax is omitted. CommonJS evidence requires a top-level, unshadowed `require` call. The lexical observers retain at most 16,384 tokens per file; reaching that limit adds `import_token_limit_reached` to coverage. Truncated JavaScript keeps recognized ES-import evidence but omits CommonJS inference because a later shadowing declaration may be outside the inspected prefix. Import evidence does not prove that the imported code executes. Maven dependencies explicitly carrying `<scope>test</scope>` are excluded from declared capability evidence in the map; `provided` remains eligible because the runtime may supply it. This Maven scope handling is private to the map and does not alter project or declaration reports. Imports under conventional test paths are retained and marked with capability properties `test_path_evidence=true` and, when all contributing import paths appear test-only, `test_only_evidence=true`. Those values reflect path naming conventions, not verified test execution. `test_only_evidence` is emitted only when the intent observer reports no omissions and every retained capability observation is a test-path import; it is omitted when a manifest, config, mixed, or incomplete-scan observation also supports the capability. TypeScript `import type` evidence is marked `type_only_import_evidence`; it does not assert a runtime dependency. Mixed test and non-test source imports retain both `test_path_evidence` and `non_test_path_evidence`.

JavaScript and TypeScript import matching uses case-sensitive, canonical npm
package roots. Import evidence retains that root in `import`, with
`import_representation=package_root`; the source span identifies the declaration.
Subpaths do not change the package match. A source specifier such as `PG` or
`redis@version` is not treated as the manifest dependency `pg` or `redis`.
Manifest dependency matching and source import matching have different syntax.

The following `properties` keys carry stable meanings. A missing key means the observation did not establish that value; consumers should preserve unknown keys for future producers.

| Kind | Common properties |
| --- | --- |
| Content population | `role`, `scope=inventory_population`, `files`, `bytes`; language populations also have `language` and `percentage`. |
| Individual content | `role`, `format`, `bytes`; a filename hint alone has partial coverage. |
| Component | `root`, `ecosystem`, `project_kind`; `language` is present only when attributed, with `language_basis`. Auxiliary paths may carry `role` and `role_basis=path_name`. Toolchain declarations that are not interface endpoints — npm `engines`, Python `requires-python`, Rust toolchain, build backends, and build scripts — are stored as component properties: `runtime_requirements` (semicolon-separated list), `build_backend`, and `build_script`. |
| Deployable | `kind`, `provider`, `source_sha256`; auxiliary path roles use the same `role` keys. Archive deployables (Maven WAR/EAR) also carry `format` (`war` or `ear`). |
| Interface or capability | `observation_kind`, `state`, `basis`; detector-specific structural keys identify the declaration without storing configuration values. `owning_component` holds the ID of the declaring component node when determinable. |
| Capability | In addition to the interface/capability keys: `evidence_path_count` is the total number of source paths that contributed observations, capped display at 20 in `evidence`; `declared_port` is set to the port string for `interface_kind: declared_port` nodes. |
| Main-class interface | Maven and Gradle main-class declarations use `interface_kind: maven_main_class` and `gradle_main_class`; their node names are the statically declared class names. Each .NET `Exe`/`WinExe` OutputType declaration produces a separate `dotnet-application` interface; MSBuild conditions remain attached, and expressions remain unresolved. |
| Package | `package_type` from the attached provider report. |

### Edge semantics

Each edge type has a distinct meaning. They are not interchangeable and do not imply each other.

| Edge type | What it means | What it does not mean |
| --- | --- | --- |
| `contains` | The source node is the declared or structural parent of the target (a deployable contains a component it references by path; a component contains content files by directory containment). | The parent manages, builds, or depends on the target. |
| `member_of` | The source component is a named member of the target workspace or multi-module root as declared in the workspace manifest. Membership is a declared relationship in a workspace configuration file. | The member is built by the root, or has any dependency on it. A workspace member and a `depends_on_local` reference are independent claims. |
| `depends_on_local` | The source component declares a local path, module-replace, or Maven reactor sibling reference to the target component. The reference is what the manifest states; it does not mean a successful build, that the version constraint is satisfied, or that the target is reachable at runtime. A Maven reactor sibling is identified by groupId:artifactId match across co-located POM files; the edge `declaration_kind` is `maven-sibling-dependency`. | The target is a transitive dependency, or that the reference resolves without a package manager. |
| `depends_on` | The source deployable declares a startup or readiness dependency on the target deployable (e.g. a Compose `depends_on` entry). The dependency is what the Compose file states; it does not mean a health check passes or that the services communicate. | The target is reachable at runtime, or that startup ordering is enforced by the runtime. |
| `builds` | A deployable definition references or contains an image, build context, or artifact whose path or name matches the target component. Derived by path or image-name matching; it is `partial` unless a direct manifest link is present. | The build succeeds, or the resulting artifact is the definitive version of the component. |
| `runs` | A deployable definition names an image that matches the target component's declared image identity. Derived by image-name matching. | The component is currently running, or that the image is built from that component's source. |
| `exposes` | The source node (component or deployable) is the declared or inferred owner of the target interface. | The interface is reachable at runtime. |
| `declares` | The source component declares the target interface in its manifest or source. | The interface is implemented, reachable, or exposed outside the component. |
| `uses_capability` | The source component has evidence of using the target capability (declared dependency, imported symbol, or config key). When `attributed_by_directory_containment` is set, the owning component was inferred by directory containment. | The capability is exercised at runtime, or is present in every code path. |
| `references_artifact` | A component declares a local JAR/DLL path and the selected inventory contains that file. Coverage remains partial. | The file is a verified package, a managed assembly, or a successfully resolved runtime dependency. |
| `packaged_in` | The source package (from an attached provider report) is packaged in or provided by the target component or deployable. | The component depends on or imports the package. |
| `analyzed_by` | The source node was analyzed by the target tool run from an attached provider report. | The analysis is complete or the findings are exhaustive. |

An edge's `declaration_kind` and `state` describe a local declaration when present. A `builds` or `runs` edge derived from a matching path or image is partial and cites both declarations when needed; it does not prove that a build or deployment succeeded.

Export the bundled Draft 2020-12 schema without network access:

```sh
dircue capabilities --schema map --json > map.schema.json
```

## Source selection and binding

Source selection follows the rest of dircue:

- At a usable Git repository root, `--source auto` maps the committed `HEAD` tree. Dirty and untracked files are excluded.
- `--source directory` maps current filesystem content and does not require `.git`.
- `--source git` requires committed Git content.
- `--rev REV` chooses a Git revision. `--tree ID` chooses an exact tree object.

A Git map records the selected tree object and, when a commit was selected, its resolved commit ID. `revision` retains the caller's expression, such as `HEAD`; `commit` is the resolved ID used when a provider supplies a comparable revision. An explicit tree selection has no commit ID. A provider binding is verified only when comparable snapshot identities match.

A directory map records a content identity in `source.digest`. By default it is the **Git-compatible tree ID**: the tree object ID that `git add -A && git write-tree` would record for the same files in a fresh repository (`algorithm: git-sha1`, `scope: gitignore_filtered`, `normalization: git_normalized`). A clean checkout of a commit therefore has the same digest as that commit's tree, so a directory map can be tied to a Git snapshot, and `map compare` reports a Git map and a directory map of identical content as the same source. Without the checkout's `.git` index, a committed file that matches an ignore rule cannot be told apart from ignored build output, so such a digest is qualified. On a case-insensitive filesystem, a checkout of a repository with case-only name pairs cannot hold both files, and its digest honestly differs from the commit. The digest runs concurrently with the scan when the source is known to be a directory; on the Linux kernel it added about one second on an Apple M1 Max.

The digest applies repository content the way Git does:

- `.gitignore` files, `$GIT_DIR/info/exclude`, and, when the directory is a checkout, its index (tracked files are never ignored, and uninitialized submodules keep their recorded commit);
- `.gitattributes` and `$GIT_DIR/info/attributes` line-ending normalization (`text`, `text=auto`, `eol`, and the legacy `crlf` attribute);
- executable bits, symlinks, and nested repositories, which are recorded as gitlinks at their checked-out commit.

User and system Git configuration is deliberately not consulted. The digest assumes `core.autocrlf=false`, `core.filemode=true`, `core.symlinks=true`, `core.ignorecase=false`, `core.precomposeunicode=false`, and no global excludes or attributes. The CLI never runs Git and never reads outside the selected root.

`source_binding` states how far the digest can be trusted:

| Status | Meaning |
| --- | --- |
| `complete` | The digest is exactly what Git records for this directory under the stated assumptions. |
| `partial` | The digest identifies the content, but Git could record something different: `filter_driver_not_applied`, `ident_not_applied`, `working_tree_encoding_not_applied`, `special_files_excluded`, `nested_repository_unresolved`, `text_auto_index_state_assumed_empty`, `ignored_entries_without_index` (ignore rules excluded entries and no checkout index was available to show which of them are committed), `sparse_checkout_entries_not_present`, or `windows_checkout_semantics` (on Windows the file system cannot record executable bits and Git checks out with `core.filemode=false`, `core.symlinks=false` and case-insensitive names, so a directory cannot be shown to equal a POSIX checkout's tree). |
| `unknown` | No digest was produced: `source_digest_disabled`, `unreadable_entry`, `digest_entry_limit`, `digest_byte_limit`, or `content_changed_during_read`. |

Computing the digest reads every in-scope file once more. Control it with `--set source.digest=git|raw|off` (`raw` hashes every file and symlink byte for byte without ignore rules or normalization: `scope` `all_entries`, `normalization` `raw`), `--set source.digest_format=sha1|sha256`, and `--set source.digest_bytes=N` (default 16 GiB; `0` removes the limit). The digest also honors the inventory entry limit. Absent a digest, attached reports and SARIF locations can still be joined by path, but the output does not claim they came from the same immutable snapshot. Embedding producers can supply their own digest in the same field.

## Coverage and limits

Successful execution means dircue produced a valid map; it does not imply that every question was answered completely. Inspect both top-level `status` and each entry in `coverage`.

| Coverage status | Meaning |
| --- | --- |
| `complete` | The evidence fully supports the named question within its declared scope. |
| `partial` | Recognized evidence is useful, but supported patterns, input limits, unresolved references, or binding prevent an exhaustive answer. |
| `unknown` | The observer did not have evidence sufficient to answer the question. |

The default inventory budget is 100,000 source entries. `--budget-files N` and `--tree-size N` set the same inventory limit and cannot be combined. Crossing the map budget returns a valid partial map and exit status `0`; the content coverage reasons record `tree_size_limit`. `--max-file-bytes N` changes which files can contribute content evidence and therefore changes coverage. It is not a hard process-memory limit.

Warnings go to stderr. JSON remains on stdout. Treat a zero exit status, warning-free stderr, and complete coverage as separate facts.

The component, deployable, interface, and capability catalogs are bounded static recognizers. Their question coverage stays `partial` even after a successful scan, because recognized forms cannot prove that no other project, entry point, or dependency declaration exists. A filename-only content role is also partial until content evidence verifies it. Nodes under common test, fixture, example, and vendor paths remain in JSON with a path-derived `role` hint; the summary and default route planner leave them out of headlines.

### The `role` property

The `role` property on component, interface, deployable, and content nodes classifies where a node sits in the repository tree. Consumers that want only production facts should filter on `role == "primary"` (or the absence of a `role` key, which is treated as primary). `--summary` already applies this filter to its headline list.

| Value | Meaning |
| --- | --- |
| `primary` | Production source and entry points. |
| `test` | Test code and test-only targets (e.g. Cargo `[[test]]` integration targets, `*_test.go`, paths under `test/`, `tests/`, `spec/`). |
| `fixture` | Test-fixture data paths. |
| `example` | Example code (e.g. Cargo `[[example]]` targets, paths under `examples/`). |
| `docs` | Documentation source (paths under `docs/`, `doc/`). |
| `tooling` | Build, benchmark, CI, and dev-tooling targets (e.g. Cargo `[[bench]]` targets, paths under `benchmarks/`, `bench/`, `scripts/`, `hack/`, `.devcontainer`). |
| `vendored` | Vendored third-party code (paths under `vendor/`, `third_party/`). |

The basis for the assignment is recorded in `role_basis`: `path_name` when derived from a path segment, `interface_kind` when derived from the declared target type (e.g. a Cargo bench or test target whose manifest path does not include a disambiguating directory segment).

A `uses_capability` edge carries the reason `attributed_by_directory_containment` and coverage `partial` when the capability observation's project assignment was inferred from directory containment rather than an explicit manifest declaration. This distinguishes heuristic attribution from explicit evidence (e.g. a `go.mod` dependency entry) and lets consumers calibrate confidence. In either case the evidence path is recorded; only the completeness claim differs.

## Control execution and coverage

The default `balanced` preset retains automatic worker selection and the 100,000-entry inventory limit. Other presets make a small set of named choices:

| Preset | Current effect |
| --- | --- |
| `balanced` | Automatic workers and the default inventory limit. |
| `low-memory` | Use two file workers, an 8 MiB retained Git object cache, a 512 KiB content cap per file, and fewer retained intent observations. Content and observation caps may omit evidence; coverage records omissions. This is a relative preference, not a hard RSS ceiling. |
| `thorough` | Raise the inventory limit to 250,000 entries; this can replace a partial answer with a more complete one. |

Inspect effective values, categories, and origins without scanning:

```sh
dircue map settings
dircue map settings --preset low-memory --json
dircue map settings --set workers=8 --set git.object_cache_bytes=128MiB --json
dircue map settings --cpu-limit 2 --memory-limit 512MiB --json
```

`--set` accepts `source.digest`, `source.digest_format`, `source.digest_bytes`, `workers`, `git.object_cache_bytes`, `inventory.files`, `content.file_bytes`, `runtime.cpu`, and `runtime.memory_bytes`. A named flag such as `--workers`, `--budget-files`, `--tree-size`, or `--max-file-bytes` has the highest precedence, as do `--cpu-limit` and `--memory-limit`; `--set` follows, then the preset. Memory values accept bytes or `KiB`, `MiB`, and `GiB` suffixes; nonzero values must be at least 1 MiB. The settings report exposes the effective value, unit, origin, and applicable fixed range. It labels runtime controls and worker concurrency `performance-only`, and the two skip/inventory bounds `coverage-affecting`. The report also lists the classifier's fixed `classification.prefix_bytes` window as `conformance-locked`; it cannot be overridden because changing it would invalidate the current Linguist parity claim. Effective settings are available from `map settings`; they are omitted from the map document so worker-only choices preserve JSON bytes.

The `fast` preset was removed after a three-run scan of the pinned 21-repository corpus on one Apple Silicon host showed no consistent or material aggregate speed improvement: it took 18.64–18.83 seconds versus 18.64–18.98 seconds for `balanced`, while its maximum observed RSS was 638–669 MiB versus 596–608 MiB. The `low-memory` preset took 25.75–26.08 seconds and reduced maximum observed RSS to 274–323 MiB on that corpus. These are host-specific observations, not performance guarantees. Custom worker and cache values remain available through `--set` when you want to test another tradeoff locally.

`--cpu-limit` scopes `GOMAXPROCS` to the map command. `--memory-limit` scopes Go's soft memory limit to the command. A value of zero inherits the process setting. These controls preserve the map answer and are restored before an embedded caller regains control. They are cooperative preferences: neither is a hard CPU quota or RSS ceiling, and they do not constrain child processes, native allocations, memory-mapped files, or operating-system caches. `GOMEMLIMIT` remains an inherited cooperative default when no explicit memory limit is supplied. Use operating-system or container controls when a workload requires enforced CPU, memory, or wall-time bounds.

### Fixed bounds

`map settings --json` also lists read-only entries categorized as
`fixed-safety-bound`. They cover retained Git readers, format input/output and
parser budgets, manifest input sizes, and retained import tokens. The registry also names bounds for
optional registry, environment and structural analysis; listing them does not
enable those modules. Their descriptions identify where each bound applies.

These bounds protect parser work and report size. They are separate from a
memory ceiling: transient Git delta readers, for example, are outside the
retained-reader count. Fixed entries cannot be changed through `--set`.
Cooperative CPU, memory, worker and cache choices preserve semantic answers
with the same analysis budgets. Presets that alter content or inventory limits
may change retained evidence and must disclose their omissions.

## Attach saved provider reports

`--attach KIND=PATH` joins an existing report into the map. The flag is repeatable. Dircue reads the file as bounded JSON data and never runs the provider.

```sh
dircue map --json \
  --attach syft-json=syft.json \
  --attach sarif=analysis.sarif \
  --attach noir-json=noir.json \
  /path/to/checkout > enriched-map.json

# Use only after independently verifying the report's source snapshot.
# This assertion requires a plain directory with complete source digest coverage.
dircue map --json --attach-binding caller-asserted \
  --attach syft-json=syft.json /path/to/plain-directory > asserted-map.json
```

Supported attachment kinds are:

- `syft-json`: package nodes, supported package relationships, reported file locations, provider metadata, and snapshot binding.
- `sarif`: run/tool metadata, reported artifact coverage, invocation state, ownership links, and snapshot binding. Findings, rules, severity, and verdicts are intentionally outside this importer.
- `noir-json`: endpoint interfaces, supported parameters, ownership links, reported files, and snapshot binding.
- `bifrost-code-query-json`: selected neutral CodeQuery structure facts and reported files. Finding, witness, conflict, severity, and source-snippet fields stay with Bifrost.

An attachment without comparable source identity is retained with an `unknown` binding. For a plain directory whose source digest has `complete` coverage, `--attach-binding caller-asserted` records the caller's explicit assertion; it never becomes `verified`. An identity mismatch cannot be overridden by that flag and does not become complete repository evidence. SARIF's standard version-control revision is compared with the map's resolved commit when both are present. Dircue-specific keys in provider metadata are not treated as source identity. Provider coverage replaces the corresponding `packages`, `analyzer_coverage`, and `routing` question entries in the resulting document. Each attached run also receives a typed `coverage_ledger` entry with its tool, report kind, scope, binding, run state, and reported files. This ledger records only what the supplied report disclosed. An empty `covered_files` array means unknown coverage rather than a clean run. Attachments must be regular files. At most 16 attachments are accepted; each is bounded to 32 MiB and 100,000 records by default.

## Analyzer coverage and blind spots

The map's `analyzer_coverage` matrix accounts for every known component, attributable language, and built-in analyzer descriptor. Entries distinguish `not_run`, `unsupported_language`, `unsupported_framework`, `prerequisite_unmet`, `tool_error`, and `unknown`. The compact `analyzer_blind_spots` array counts entries in each state for summary display.

Descriptors are versioned dircue data and include their upstream documentation source. Built-ins cover Syft, OpenTaint, Noir, Bifrost, BCA, and scc. An unrecognized SARIF producer receives the generic SARIF descriptor and remains `unknown`; SARIF syntax does not prove analyzer language support.

This accounting is intentionally conservative. `language_basis` distinguishes an explicit component property, a root language population, and an unattributed language. Component languages that cannot be attributed remain `unknown`; no per-component file denominator is invented. Provider file lists are retained but do not prove exhaustive coverage. No report means `not_run`, while an empty report from a tool that ran remains `unknown`. Repository content cannot override the built-in capability data.

## Capability catalog

The built-in catalog maps package coordinates and import names to capability categories using ecosystem-aware exact matching. Each ecosystem has its own lookup table keyed by canonical package name; entries cover the top ~10 packages per category across npm, PyPI, RubyGems, Maven/Gradle, NuGet, Go, Cargo, Composer, Dart/pub, and Python import names. The catalog is in `pkg/intentmap/catalog.go`.

**Ecosystem matching rules:**

| Ecosystem | Kind value | Name format | Version stripping |
| --- | --- | --- | --- |
| npm | `npm-dependency` | Exact package name | Strip trailing `@version` (`@scope/pkg@ver` → `@scope/pkg`) |
| PyPI | `python-dependency` | PEP 503 canonical (lowercase, `[-_.]` → `-`) | Strip at `[<>=!~(@ ;` |
| RubyGems | `ruby-gem-dependency` | Exact gem name | None (already canonical) |
| Maven | `maven-dependency` | `group:artifact` prefix | Exact `group:artifact` from `group:artifact:version` |
| Gradle | `gradle-dependency` | `group:artifact` prefix | Same as Maven |
| NuGet | `package-reference` | Exact package name (case-insensitive) | Strip at `@` |
| Go modules | `go-require` | Module path prefix | Strip `@version` suffix |
| Go imports | `go-import` | Import path prefix | Exact match |
| Cargo | `cargo-dependency` | Exact crate name | Strip space-separated version |
| Composer | `php-dependency` | `vendor/package` exact | None |
| Dart/pub | `dart-dependency` | Exact package name | None |
| Python imports | `python-import` | Exact import name | None |

Current capability categories:

| Category | Description |
| --- | --- |
| `datastore:postgresql` | PostgreSQL client libraries |
| `datastore:mysql` | MySQL/MariaDB client libraries |
| `datastore:mongodb` | MongoDB client libraries |
| `datastore:relational` | ORM / relational DB frameworks (SQLAlchemy, Hibernate, Doctrine, etc.) |
| `cache:redis` | Redis client libraries |
| `messaging:kafka` | Apache Kafka client libraries |
| `messaging:amqp` | AMQP / RabbitMQ client libraries |
| `messaging:smtp` | SMTP / email-sending libraries |
| `messaging:nats` | NATS messaging client libraries |
| `messaging:event-bus` | Higher-level message-bus frameworks |
| `storage:object` | Object / blob storage (S3, GCS, Azure Blob) |
| `net:http-client` | Outbound HTTP client libraries (Go `net/http`: see [below](#go-nethttp-rule)) |
| `auth:oidc` | OpenID Connect / OAuth2 provider clients |
| `auth:jwt` | JWT signing and verification libraries |
| `auth:oauth2` | OAuth2 server-side authentication frameworks |
| `crypto:library` | General-purpose cryptography libraries |
| `serialization:protobuf` | Protocol Buffers runtime libraries |
| `serialization:yaml` | YAML parsing libraries |
| `exec:process` | APIs that spawn child processes |
| `ai:llm-sdk` | LLM provider SDKs (OpenAI, Anthropic, LangChain, HuggingFace) |
| `cloud:aws` | AWS SDK and service client libraries |
| `cloud:azure` | Azure SDK and service client libraries |
| `cloud:gcp` | Google Cloud / Firebase client libraries |
| `search:elasticsearch` | Elasticsearch / OpenSearch full-text search clients |
| `directory:ldap` | LDAP client libraries and embedded LDAP server libraries (Spring LDAP, UnboundID LDAP SDK, Apache Directory Server, python-ldap, ldap3, ldapjs) |

Evidence levels (stored in the `basis` property of a capability node or `uses_capability` edge):

| Basis | What it means |
| --- | --- |
| `declared_dependency` | The package coordinate appears in a parsed manifest (requirements.txt, go.mod, package.json, etc.). |
| `declared_config` | A recognized configuration key or declaration in a config file (e.g. `DATABASE_URL`, `spring.datasource.*`, or a Prisma `schema.prisma` datasource `provider`) names the capability. Key names and provider strings only; connection strings and runtime values are never stored. |
| `imported` | A top-level import statement in a source file names a package in the catalog. Go and Python retain their bounded import rules; Java/Kotlin, C#/VB and JS/TS use their documented lexical rules above. Imports inside unsupported or conditional source constructs may be omitted. |
| `code_syntax` | A referenced symbol in source code establishes the capability, cited at the first reference. Used where an import alone is ambiguous, such as Go `net/http`. |

A `declared_dependency` with no corroborating import is still valid evidence. An `imported` observation without a matching declaration means the import was found in source but not in a parsed manifest (possible in lock-file-only or vendor setups). The map records both and lets consumers decide how to weight agreement vs. single-path evidence.

### Go `net/http` rule

Go's `net/http` serves both inbound handlers and outbound requests, so its import alone is not evidence of an HTTP client. The `net/http` family is classified per package:

| Import | `net:http-client` when | Basis |
| --- | --- | --- |
| `net/http` | the file references `Client`, `DefaultClient`, `Get`, `Head`, `Post`, `PostForm`, `Transport`, `DefaultTransport` or `RoundTripper` through the unshadowed package name | `code_syntax`, at the first reference |
| `net/http/httputil` | the file references `ReverseProxy`, `NewSingleHostReverseProxy`, `DumpRequestOut`, `ClientConn`, `NewClientConn` or `NewProxyClientConn` | `code_syntax`, at the first reference |
| `net/http/cookiejar`, `net/http/httptrace` | always (client-side packages) | `imported` |
| `net/http/pprof`, `httptest`, `fcgi`, `cgi` and any other sub-package | never | — |

`http.NewRequest` and `http.NewRequestWithContext` do not count: server tests use them to build requests for `handler.ServeHTTP`. Dot and blank imports are never attributed.

Import evidence is read from every source file of a component, including its tests. A test that calls a local test server with `http.Get` still gives the component `net:http-client`; distinguishing test-only capabilities is tracked in [#177](https://github.com/war-and-code/dircue/issues/177).

### Contributing a new catalog entry

The catalog is in `pkg/intentmap/catalog.go`. Entries are per-ecosystem exact maps (`npmExact`, `pypiExact`, `rubyExact`, `cargoExact`, `phpExact`, `nugetExact`, `pythonImportExact`) or prefix entries for Go module paths and Maven/Gradle group coordinates.

To add entries:

1. Pick or propose a category name. Prefer an existing category for the same technology, not a new one per library.
2. Add the package name to the appropriate ecosystem map, keeping entries sorted by category within each map. For Go module paths and Maven groups, add to the `prefixEntries` slice with a `prefixMatchKind`-compatible kind.
3. Add or extend the table test in `pkg/intentmap/catalog_test.go` (`TestCatalogEcosystemExactMatching`) with at least one positive and one negative case for the new entry.
4. Run `go test ./pkg/intentmap/...` and `go test -race ./pkg/intentmap/...`.
5. Run `python3 tests/map_corpus/verify.py --results <dir>` after `python3 tests/map_corpus/run.py` to confirm no fixture regressions.

Catalog entries must be verifiable against public documentation for the package or import name. Do not add entries for internal or private packages.

## Route follow-up tools

Routing reads a saved map and emits deterministic plans without rescanning the source:

```sh
dircue map route --json map.json > routes.json
```

Plans describe applicability, scope, prerequisites, and report kind. A plan has an argv template only where dircue carries a reviewed recipe; its values contain placeholders such as `{scope}` and `{report}`. Other tools return an empty argv plus the unmet `exact_invocation` prerequisite. Templates are data, not approved shell commands. Dircue does not inspect `PATH`, check installed tool versions, download anything, or execute a plan. The caller must choose tools, substitute confined paths, apply resource policy, and validate the final invocation.

Current route descriptors cover Syft, scc, BCA, Noir, OpenTaint, and Bifrost where the map contains applicable evidence. A routing descriptor does not mean that dircue can import every output mode from that tool; attachment support is limited to the formats listed above.

## Forests

`dircue map --forest PATH` scans a directory that may contain multiple Git repositories (a developer laptop, an archive drive, a CI workspace) and produces a single forest document that covers every nested root plus the remaining unrooted content.

```sh
dircue map --forest --json /path/to/drive > forest.json
dircue map --forest --summary /path/to/drive
```

### What a forest document contains

| Field | Description |
| --- | --- |
| `roots` | One entry per discovered Git root with path, kind, HEAD, commit, tree, credential-stripped remotes, and committer time. |
| `environment_trees` | Recognized dependency, build-output, and cache trees that were counted rather than scanned in detail. |
| `residual` | A directory-mode map of all content not covered by a root or an environment tree. |
| `coverage` | Per-question coverage for roots, residual, and environment trees. |

### Root kinds

| Kind | Description |
| --- | --- |
| `git_worktree` | Normal working-tree repository (`.git/` directory or file). |
| `git_bare` | Bare repository (HEAD + objects/ + refs/ without a working tree). |
| `git_submodule` | Working tree whose `.git` is a file pointing to a parent's `.git/modules/`. |

### Environment and build-output trees

The following directories are recognized and summarized rather than scanned entry-by-entry:

| Pattern | Kind | Ecosystem |
| --- | --- | --- |
| `node_modules/` | `dependency_tree` | Node.js |
| `pyvenv.cfg` sibling | `dependency_tree` | Python |
| `__pycache__/` | `cache` | Python |
| `.tox/`, `.nox/` | `cache` | Python |
| `.mypy_cache/`, `.pytest_cache/`, `.ruff_cache/` | `cache` | Python |
| CACHEDIR.TAG with correct signature | `cache` | (ecosystem from parent) |
| `.gradle/` with Gradle sibling | `build_output` | Gradle |
| `build/` with Gradle sibling | `build_output` | Gradle |
| `.terraform/` | `build_output` | Terraform |
| `Pods/` with `Podfile` sibling | `dependency_tree` | CocoaPods |

`vendor/`, `third_party/`, `dist/`, and a plain `build/` without a Gradle sibling are never summarized. Set `--set content.summarize_trees=off` to disable summarization and scan every directory as content.

### Credential stripping

Remote URLs are stripped of credentials before they appear in any output. HTTPS user-info (`user:token@host` → `host`) and SCP-style user prefixes (`git@host:path` → `host:path`) are removed. Query strings and fragments are dropped. A canary embedded in a remote URL will not appear in the forest document.

### Identity resolution

Each root's `identity_status` is either `resolved` (HEAD commit read successfully) or `unknown` (unborn HEAD, missing objects, or unreadable `.git`). Unknowns are listed separately in `--summary` output. A forest with any unknown root has `status: partial`.

### Export the schema

```sh
dircue capabilities --schema forest --json > forest.schema.json
```

## Compare saved maps

```sh
dircue map compare --json before.json after.json > map-change.json
dircue map compare --format markdown before.json after.json >> "$GITHUB_STEP_SUMMARY"
```

Without `--json`, `--format text` (the default) prints a plain summary and `--format markdown` a pull-request-ready one; `--json` writes the full comparison document and takes precedence over `--format`.

Comparison uses stable IDs and opens neither source tree. It separates material source changes, provider observations, evidence changes, and coverage changes. Provider-only nodes and edges do not inflate source material-change counts. If the head map has incomplete or incomparable provider coverage, a missing observation is reported as an indeterminate removal rather than a proven deletion. The caller selects the pair; dircue does not infer repository identity or rename relationships. A valid comparison exits `0` even when changes are present.

`dircue map compare` accepts only map documents (`kind: "map"`). Passing a legacy profile document produces a helpful error pointing to `dircue compare`. The older `dircue compare` command accepts only aggregate `analyze all` profile documents; passing a map document to it produces a helpful error pointing to `dircue map compare`. Their input and output contracts are different. This split preserves the legacy command contract; scripts comparing maps should use `dircue map compare` explicitly.

### Comparison outcome exits

By default a valid comparison exits `0`, including when it finds changes.
For an automation gate, select the new behavior explicitly:

```sh
dircue map compare --exit-code --json before.json after.json
```

The comparison document is unchanged by this option. A complete report is
written before returning `0` for observed unchanged or `1` for observed changed.
Uncertain comparisons return `2`: this includes indeterminate removals,
incomparable results, unbound or partially bound source identity, decreased question coverage and differing observer identities. Equal partial digests do not establish a fully bound source.
Uncertainty takes precedence when a report also contains observed changes.
These outcome exits do not write an error diagnostic to stderr. A usage, input
or output error still exits `1` with a diagnostic; do not mistake it for a
successful changed result.

`--on-uncertain allow`, accepted only with `--exit-code`, explicitly permits the
observed changed/unchanged exit instead. It never removes caveats or changes the
JSON status. An observed unchanged result is limited to the report's evidence
and coverage; it is not proof that an incompletely inspected tree has no changes.

## Locate SARIF results in the map

`map locate` adds a `dircue.map` property to physical SARIF result locations:

```sh
dircue map locate map.json results.sarif > located.sarif
dircue map locate --summary map.json results.sarif > location-summary.json
```

Annotations can name owning components, deployables, interfaces, and a content role. Resolution is one of `resolved`, `ambiguous`, `not_in_snapshot`, `outside_root`, or `unresolvable_uri`. Existing SARIF fields and location properties are preserved.

Use `--source-uri /selected/root` only to relativize absolute SARIF artifact URIs. The value is not copied into output. Windows source paths are matched case-insensitively; other paths are case-sensitive by default. Directory maps with a producer-supplied digest can pass the corresponding SARIF snapshot identity as `--source-digest ALGORITHM:SCOPE:VALUE`. This flag cannot make an ordinary undigested directory map immutable after the fact.

A `uriBaseId` is resolved through the report's `originalUriBaseIds`. Some tools use a base such as `%SRCROOT%` without declaring it; define it with `--uri-base SRCROOT=.` (the map root) or another URI. Declared bases always take precedence. An absolute URI does not depend on its base (SARIF 2.1.0 section 3.4.4), so a base attached to an absolute URI is ignored and `--source-uri` applies instead. Nothing is guessed: an undefined base on a relative URI stays `unresolvable_uri`.

Inputs are limited to regular files. The saved map limit is 64 MiB. SARIF is limited to 64 MiB, 64 runs, 1,000,000 results, and 4,000,000 locations. The command supports SARIF 2.1.0 physical locations; logical-only locations and unsupported or escaping URIs remain unresolved.

## Current boundaries

- The map is a static evidence graph, not a build graph produced by evaluating arbitrary build logic.
- Dynamic dependency resolution, compiler type information, call graphs, and runtime behavior are outside the one-shot map.
- Project and deployable recognition is limited to documented built-in adapters. Unknown syntax remains unknown rather than guessed.
- Repository prose cannot establish components, interfaces, deployables, or capabilities. Documentation remains a distinct content observation.
- Attached provider reports are trusted as caller-supplied data, with source binding and coverage disclosed. Dircue does not attest that a provider ran correctly or completely.
- The map command uses bounded native observers. Optional Tree-sitter/BCA structural analysis remains a separate explicit workflow.

For exact command and flag metadata from an installed binary, run:

```sh
dircue map --help
dircue map settings --help
dircue map compare --help
dircue map route --help
dircue map locate --help
dircue capabilities --cli --json
```
