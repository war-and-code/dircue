# Repository measurements

Added in 1.4.0; structural summaries expanded in 1.5.0.

Assessment combines language statistics with factual repository measurements. It works on an extracted directory or a selected Git tree, without Syft, package-manager execution, network access, or a structural worker. It does not decide whether a repository is large, whether it is a monorepo, or which tools a caller should run.

```sh
dircue analyze assessment --source directory --json /content
dircue analyze all --assessment --source directory --json /content
dircue analyze all --assessment --source git --rev HEAD --json /checkout
dircue capabilities --schema assessment
```

Both analysis commands return the versioned aggregate profile. In 1.5.0, the `assessment` component is version `1.1.0` and uses profile schema `1.10.0`. Current readers also accept the earlier assessment version `1.0.0` in profile `1.9.0`; strict older readers may reject the new fields. Assessment requests metadata discovery, passive project declarations, supported lockfile observations, and manifest/deployment entry points. Languages keep their existing inclusion rules. File and byte measurements describe a wider population than language statistics.

`--source directory` reads current files and supports directories with no Git metadata. Automatic source selection at a Git repository root normally reads committed `HEAD`; it does not include dirty or untracked changes. Directory inspection is not an atomic snapshot. Read failures fail by default; `--on-error continue` records qualified evidence.

## Populations and definitions

The `definitions` array describes how measurements are formed. Each metric records a `count`, its `scope`, `completeness`, and any qualifying `reasons`. These are observations for caller-defined analysis, not organizational policy.

File totals count selected regular files. Bytes are logical content sizes, not allocated disk blocks, Git history size, archive-expanded bytes, or process memory.

The `inventory` field also counts `vendored_files` and `vendored_bytes`: files that Linguist's vendor path rules match (for example paths under `node_modules/`, `vendor/`, `dist/`, `test/fixtures/`, `testdata/`, or `.github/`) or that a `linguist-vendored` attribute marks. Without an attribute override, `.venv/` matches no Linguist vendor rule and stays in the unvendored file and byte totals.

Manifest counts are filename candidates grouped by normalized filename pattern and ecosystem, with a `kind` field identifying each group's role: `manifest`, `workspace`, `solution`, `configuration`, or `toolchain`. Variable project names become patterns such as `*.csproj`; concrete paths remain bounded evidence. They do not establish that a manifest parses or describes a functioning project. Installed npm contents are outside the project-manifest population. Lockfiles and checksum files (`package-lock.json`, `go.sum`, `Cargo.lock`, `yarn.lock`, and similar) record resolutions rather than declarations; they appear in `filename_candidates` under the `lockfile` kind, not in `manifest_candidates`.

`unparsed_manifest_candidates` counts selected manifests that the declaration parser did not read in full and interpret, because they were unreadable, invalid, unsupported, or over a declaration input limit. A manifest merged into another record (such as a `requirements.txt` beside a `pyproject.toml`), one read and found to hold no declarations, or one parsed and then dropped at the declaration report size limit is not counted as unparsed; the last makes project metrics a lower bound instead.

Parsed package and project records and their distinct directories are separate measurements. The project count excludes virtual workspace roots (`go.work`, a Cargo manifest with only `[workspace]`, a uv workspace without `[project]`), which still contribute workspace membership. It also excludes configuration, solution, and annotation records such as `config/application.rb`, Cargo manifests with neither a package nor a workspace table, and `pyproject.toml` files that hold only tool settings: neither a `project` nor a `build-system` table, and no Python lockfile beside them. An unrecognized declaration record kind makes project metrics a lower bound with reason `unclassified_declaration_kind`. Several manifests can describe one project and several projects can share a directory, so manifest counts are not project counts.

`projects_by_role` and `project_roots_by_role` partition those counts by the path roles `dircue map` uses ([role values](MAP.md#the-role-property)): `vendored`, `fixture`, `example`, `test`, `docs`, `tooling`, and `primary`. These are conventional filename and directory hints; `primary` is the fallback when no auxiliary role matches. A root takes the highest-priority role among its projects, in that order.

Workspace membership and local dependency declarations are separate relationships. A directory hierarchy or a matching package name alone does not establish either relationship. Conditions and unresolved targets remain qualified evidence. Reference counts include confined declared targets; they are not a count of proven connections between functioning projects. Cargo default-member selection remains in declarations and does not add workspace memberships.

Lockfile association counts describe supported static ownership rules. They do not establish dependency resolution, a fresh lockfile, a complete SBOM, or a successful locked restore.

A `complete` count covers the supported population named in its scope. It is not a claim to support every ecosystem or evaluate every build expression. A `lower_bound` count contains the observations retained under its disclosed omissions; the true value is at least the count but possibly higher. An `upper_bound` count may overestimate: the true value is at most the count but possibly lower. An `observed_only` count is exact for the observed data but may change in either direction when observations are incomplete. Do not turn a lower bound into a denominator for a supposedly exact percentage.

## Structural summaries

`structure.populations` partitions five populations by ecosystem and path role: `filename_candidates`, `parsed_projects`, `distinct_roots`, `workspace_groups`, and `solution_groups`. Each row has its own metric and reasons. Candidate counts come from selected filenames before parsing or evidence trimming. Invalid manifests remain candidates; they cannot become parsed projects. Several projects may share one root. A physical directory containing manifests for several ecosystems appears in each ecosystem's root population, so those root counts cannot be added to obtain a distinct-directory total.

`workspace_groups` and `solution_groups` retain explicit declarations, including supported empty workspace groups. A group identifies its manifest, member count, bounded member paths, and unresolved members with their state and resolution. The full group totals and omitted-example counts remain separate from retained lists. Directory nesting establishes neither group membership nor dependency.

The group observers cover supported Maven modules, literal Gradle settings, npm and uv workspaces, Cargo workspaces, Go workspaces, and .NET solutions. Project identities also cover the other manifest families supported by the declaration adapters, including Dart, PHP, Ruby, Swift, and Elixir. Detail varies by ecosystem: recognizing a project does not establish full build-system support. Unsupported workspace declarations keep the affected structural scope partial rather than making unrelated ecosystems uncertain.

`structure.dependencies` separates definite local edges from `qualified_references`, grouped by ecosystem, declaration kind, state, and resolution. A definite edge requires retained, parsed endpoints and an unconditional supported local reference. A Maven sibling match additionally requires case-exact literal coordinates, matching known versions, and both projects in an explicitly declared, unconditional reactor group. A Maven parent reference is a definite edge only when the source POM's declared `maven-parent` groupId:artifactId (case-sensitive) matches the target POM's declared coordinates and, when both sides declare a literal version that is not a range or floating marker, the versions also agree. A mismatched GA produces a `parent_coordinates_mismatch` qualified reference; a mismatched version produces `parent_version_mismatch`; an unresolvable declared coordinate produces `parent_coordinates_unresolved`. This view does not infer a target version inherited from a parent; that match stays qualified. Coordinate-only matches, ambiguous coordinates, conditions, missing targets, and self-references also stay qualified. The legacy local-reference totals still count declarations; they are not interchangeable with definite-edge totals.

A relationship is conditional only when the declaration says so: an MSBuild condition, a Maven profile, Gradle script evaluation, a Cargo `target=` selector or `optional = true`, or a Python environment marker or optional extra. A dependency section is a scope, not a condition, so npm `devDependencies`, Cargo dev and build dependencies, Dart `dev_dependencies`, Python dependency groups, and Go `replace` labels still form definite edges.

A .NET `<Reference Include="Name"/>` whose simple assembly name equals the assembly name of one parsed project (its literal unconditional `AssemblyName`, or else its project file name, compared case-insensitively) is a qualified `assembly-reference` with resolution `assembly_name_match`, or `ambiguous_assembly_name_match` when several projects share the name. Custom build logic may resolve such a reference to that project's output, but the build can equally resolve a package, a framework assembly, or a binary with the same name, so it never becomes a definite edge. Its presence adds `assembly_name_references_to_local_projects` to `nuget` dependency coverage.

`connected_groups` counts weakly connected components in the observed definite-edge graph, including isolated parsed projects. Direction is ignored when forming a component but retained on each edge. This count is exact for that observed graph. Missing edges can only merge components, so when dependency coverage is incomplete the count is an `upper_bound` or `observed_only`, never `lower_bound`. Consult the separate `dependency_connectivity` coverage before using it. `connected_groups_with_qualified` counts the same components after joining definite edges with every qualified local reference whose target is one retained project: conditional relationships, Maven coordinate matches without a declared reactor, and assembly-name matches. It can only be smaller than or equal to `connected_groups`. When the vertices are complete and the only missing dependency evidence is those qualified references, the true number of groups lies between the two counts. Neither count is a verdict: connected groups do not establish independent applications, services, builds, or deployability, and a single group does not establish a monorepo or its absence.

`structure.coverage` separates `workspace_membership`, `solution_membership`, `project_dependencies`, `dependency_connectivity`, and `entry_points` by ecosystem. Ecosystem names in coverage rows use the same vocabulary as populations: `python` for uv projects (not `python-uv`), `nuget` for C# and F# projects, and `dotnet` for .NET solution groups. Declaration diagnostics qualify the relationship scope they could affect, through an explicit table of every diagnostic code the parsers emit; diagnostics about build targets, executables, scripts, engines, or names inferred from directories qualify neither scope. File traversal omissions separately qualify inventory populations. A complete inventory can therefore coexist with partial structural evidence. For example, `coordinate_match_without_declared_reactor`, `self_reference_qualified`, and `shared_msbuild_project_references_not_applied` identify distinct limits on dependency evidence. Gradle's unresolved build-evaluation requirements qualify its membership and dependency scopes with `build_declarations_require_evaluation`; its known project counts remain exact. A group is identified by its aggregator project, as for every ecosystem: the root `package.json`, the aggregator `pom.xml`, or the root `build.gradle` whose `settings.gradle` declares the members. A solution or workspace group whose members include files that exist on disk but are not a recognized project type (for example `.dcproj` Docker Compose files in a Visual Studio solution) shows those members as unresolved with resolution `present_qualified` and sets membership coverage partial with reason `member_project_type_not_retained`.

`structure.entry_points` lists supported manifest interfaces and deployment declarations, with a project association when a static rule supports one. Each row names its evidence path, kind, ecosystem, role, basis, and state, and a qualified or unassociated row also names a reason. A declared script name is retained without its shell command.

`entry_point_count` counts distinct declarations, so one Dockerfile associated with two projects counts once. `entry_point_association_count` counts rows that name a project, including qualified associations; an unassociated declaration contributes no project association. Different association kinds, such as `builds` and `runs`, remain separate rows even for the same declaration and project; this is not a distinct-project count. `entry_point_row_count` counts all rows, including unassociated declarations. These totals are formed before display trimming. `omitted_entry_points` counts rows excluded from the bounded sample, and the retained rows plus omitted rows equal `entry_point_row_count`.

A row's state is `declared` for an interface in the project's own manifest, `associated` for a deployment declaration matched to exactly one project by complete evidence, `qualified` when the match is indirect, conditional, or names several projects, and `unassociated` when no project could be matched. The basis says how an association was made: `declared_manifest` for the project's own manifest, `declared_config` when a deployment file names the project's directory as its build context, `directory_co_location` when the only link is that the files share a directory, `image_reference_match` when a container image name links two declarations, and `rule_inferred` for other structural rules, such as a Dockerfile copying a Maven archive. A `directory_co_location` association reflects layout, not a declaration, and never proves that a project builds or runs through that entry point.

Only declarations of something that can be built, run, or deployed count as entry points: Dockerfiles, Skaffold artifacts, Compose services, Kubernetes workloads (Deployment, StatefulSet, DaemonSet, Job, CronJob, Pod, ReplicaSet, ReplicationController), Helm application charts, CloudFormation and SAM functions and container tasks (ECS task definitions, Batch job definitions, App Runner services), Maven web archives, Procfile processes, Aspire services, and Serverless Framework services. Other observed deployment declarations are counted by `provider:kind` in `excluded_non_entry_kinds`, so their absence from the list is visible: Kubernetes Services, Ingresses, and configuration resources, which route to or configure workloads; Helm library charts, which only provide templates; other CloudFormation resources such as buckets and roles; Terraform modules; CI workflows; and Makefile build invocations, which orchestrate builds rather than declare a deployable unit. A Skaffold artifact without a `context` uses Skaffold's default, the directory of `skaffold.yaml`.

The catalog reuses the deployment observers without running the source-code intent detectors, so entry-point coverage stays partial with `source_entry_points_not_inspected`; finding no entry point is not evidence that none exists. `deployment_observations_incomplete` is added when the deployment observers stopped at a limit.

## Lockfile associations

The component exposes overall and per-ecosystem project counts, an eligible population, and covered, missing, not-applicable, unsupported, and unknown outcomes. `eligible` counts npm and NuGet projects whose outcome is `covered`, `missing`, or `unknown`; projects that are `not_applicable` (no direct declarations) or `unsupported` are excluded. It describes the static checker population; it is not a policy requirement.

Each lockfile row, including `lockfiles_overall`, has a `by_role` array partitioning its counts by the same path roles as `projects_by_role`. Each row also has `outcome_reasons`: a list of `{state, reason, count}` triples for every non-covered project, where `reason` is the project's lockfile boundary reason, `no-direct-declarations` for every `not_applicable` project, or one of `no-lockfile-context`, `lockfiles-skipped`, `lockfiles-not-run`, `ecosystem-outside-association-scope`, or `unspecified`.

Assessment 1.1.0 rows add three breakdowns that keep lockfile presence, ownership, and the named check apart. `checks` partitions covered projects by check status (`match`, `different`, `indeterminate`, `not_applicable`), and `indeterminate_reasons` counts each reason once per project, so a project can appear under several reasons. `nuget_presence`, on the `nuget` and `all` rows, partitions NuGet projects by whether a lockfile exists at a path they can use, regardless of who owns it, with `unknown_reasons`. `causes` lists up to 32 `{reason, path, count}` entries, most frequent first: the selected file named as the cause of a missing or uncertain outcome or an indeterminate check, and how many projects it explains. `omitted_causes` counts the rest. A single `Directory.Build.props` that imports an unmodeled SDK, for example, appears once with the count of projects it qualifies.

Assessment always applies `--npm-workspace-locks` association semantics. A covered project has a supported observed association; its named check can still be `different` or `indeterminate`. The existing `lockfiles.contexts` provide per-project paths, checks, and boundaries.

A workspace member is covered by its workspace root's npm lockfile only when that root is the nearest ancestor whose `workspaces` list the member, as npm selects it, and the lockfile has the member's own package entry; the member is compared with that entry. NuGet ownership comes from a static evaluation of each project's MSBuild imports and lock path. Its per-context `nuget_evidence` keeps candidate presence, ownership, and the named check separate and names the selected file behind each uncertain result. Conditional or expression-valued paths, SDKs and imports outside the model, path collisions, and input limits remain qualified. See [lockfile observations](LOCKFILES.md) for the exact subset and reason codes.

Selected `pnpm-workspace.yaml`, `lerna.json`, and `rush.json` files outside `node_modules` are recognized but not parsed, so their presence makes workspace membership and local dependency counts lower bounds (`pnpm_workspace_unparsed`, `lerna_workspace_unparsed`, `rush_workspace_unparsed`).

## Evidence and limits

If the selected tree exceeds `--tree-size` (default 100,000 entries), assessment is skipped: all counts are zero lower bounds with reason `tree_size_limit`. Raise `--tree-size` to measure larger trees.

A declaration omission makes project metrics a lower bound only when the omitted path could be a manifest, is a directory that could not be read (outside `node_modules`), or when omissions cannot be attributed to paths; a symbolic link or Git submodule link does not.

The scanner API can also summarize environment directories instead of traversing them. This qualifies inventory counts and any project, relationship, or association population the skipped subtree could affect. Installed `node_modules` contents remain outside the project-manifest population.

Candidate examples retain at most 256 paths per kind; project-directory examples retain at most 256 paths; workspace and local-reference examples each retain at most 64 entries. Paths longer than 3,072 bytes are excluded from these examples and counted in their omissions. These limits apply to display evidence independently of aggregate counts. Escaped JSON evidence also has a 7 MiB budget; the assessment component has an 8 MiB serialized ceiling. Budget trimming can reduce the retained examples further and increases their omitted-example counts without changing aggregates. An omitted example is not an omitted file. Parser limits, unreadable inputs, or an incomplete selected inventory are different: they qualify the affected project, relationship, or association population.

Structural examples retain at most 256 workspace groups and 256 solution groups, 64 resolved and 64 unresolved members per group, 512 definite edges, 256 connected components with 64 project paths each, 512 qualified-reference categories, and 512 entry-point rows. The same path and JSON-byte budgets can trim these lists further. Counts are formed before display trimming, and omitted counts reconcile each sample with its observed total.

The source traversal defaults to the existing 100,000-entry limit. Declaration parsing retains at most 4,096 documents, with a 1 MiB per-document and 64 MiB total input ceiling. Lockfile inspection retains at most 256 lockfiles and 4,096 contexts, with a 1 MiB per-file and 16 MiB total input ceiling. The companion modules expose their full limits and coverage.

The existing .NET declaration adapter cannot faithfully interpret a selected POSIX filename containing a literal backslash. Assessment retains its exact file and manifest counts, but qualifies the affected project, root, relationship, and association populations with `dotnet_selected_path_identity_ambiguous`; it does not invent a project at a normalized path. This differs from backslashes used as separators inside a build declaration.

Inspect the metric itself before interpreting absence. An empty language array does not mean an empty directory. Zero known project relationships does not prove independent builds when declarations were incomplete. Unsupported ecosystems and unknown outcomes remain visible.

The assessment module reuses the selected source and declaration or lockfile reports. It does not scan a second directory tree. Existing manifest, inventory, content-read, and output bounds still apply.

## Cost

The 1.4.0 assessment used roughly the same work as explicitly requesting discovery, declarations, and lockfiles. Version 1.5.0 also inspects selected deployment files and joins bounded structural evidence. The language-only and default aggregate commands do not opt into this work. Added cost depends on repository contents and the selected limits; warm-cache measurements cannot establish a guarantee for another machine or repository.

## Reading the report

The component is at `.assessment` in the JSON profile; per-project lockfile contexts are at `.lockfiles.contexts`.

Count of primary-role npm projects without a lockfile:

```sh
dircue analyze assessment --source directory --json /repo \
  | jq '.assessment.lockfiles[] | select(.ecosystem=="npm") | .by_role[] | select(.role=="primary") | .missing'
```

Manifest paths of npm projects where no lockfile was found:

```sh
dircue analyze assessment --source directory --json /repo \
  | jq '.lockfiles.contexts[] | select(.ecosystem=="npm" and .association_state=="missing") | .manifest_path'
```

Outcome reasons for npm projects (state, reason code, and count for each non-covered group):

```sh
dircue analyze assessment --source directory --json /repo \
  | jq '.assessment.lockfiles[] | select(.ecosystem=="npm") | .outcome_reasons[]'
```

Files that explain the most uncertain NuGet results:

```sh
dircue analyze assessment --source directory --json /repo \
  | jq '.assessment.lockfiles[] | select(.ecosystem=="nuget") | .causes[]'
```

Primary project populations by ecosystem and structural qualification:

```sh
dircue analyze all --assessment --source directory --json /repo \
  | jq '.assessment.structure | {populations: [.populations[] | select(.role=="primary")], coverage}'
```

Observed connected groups and their bounded project lists:

```sh
dircue analyze assessment --source directory --json /repo \
  | jq '.assessment.structure | {dependencies, coverage: [.coverage[] | select(.scope=="dependency_connectivity")]}'
```

## Optional Syft evidence

An existing Syft report can enrich the same invocation:

```sh
dircue analyze all --assessment --source directory --json \
  --syft-report /reports/syft.json --syft-root / /content
```

Dircue imports the report once through its existing [package-evidence importer](PACKAGE_EVIDENCE.md). It does not invoke Syft or recreate its package catalog. Native measurements remain based on the selected source: an attachment cannot establish a missing manifest, workspace membership, lockfile ownership, or complete native coverage. Package attribution retains its separate coordinate-mapping, source-binding, schema, and coverage checks. Keep supplied reports and saved dircue outputs outside the directory being measured if they should not contribute to its file or byte totals.

A caller can inspect the native report first, apply its own policy, and import a Syft report in a later invocation.
