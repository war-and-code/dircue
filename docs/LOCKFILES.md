# Lockfile observations

Lockfile analysis is an optional, bounded view of selected npm and NuGet files. It reports which project and lockfile were associated, then runs a small named check. A `match` is only a match for that check; it does not mean an install or restore would reproduce the same graph.

```sh
dircue analyze lockfiles --json /path/to/checkout
dircue analyze all --lockfiles --json /path/to/checkout
dircue analyze lockfiles --source directory --json /path/to/working-tree
dircue capabilities --schema lockfiles
```

`analyze lockfiles` and `analyze all --lockfiles` emit a versioned profile with profile `schema_version` `1.8.0`; `--assessment` emits assessment profile `schema_version` `1.10.0`. The lockfile module reports `provider_version` `1.1.0`; saved `1.0.0` lockfile reports remain readable. `capabilities --schema lockfiles` exports the standalone component schema. Check the module `status`, `coverage`, each NuGet context's `nuget_evidence`, `association_state`, and named check `status` separately. Module status describes observation coverage, not dependency health.

## npm

Dircue reads root `package.json` declarations and the root package entry (`packages[""]`) in npm lockfile format versions 2 and 3. It compares dependency names and the exact declaration text in `dependencies`, `devDependencies`, `optionalDependencies`, and `peerDependencies`. A text difference is reported as `different`; dircue does not interpret semver ranges, resolve aliases or Git references, or compare the installed or transitive graph. A comparison is `indeterminate` for aliases, Git references or other declarations outside the bounded matcher.

At a package root, dircue applies npm 11's static selection rule: `npm-shrinkwrap.json` takes precedence over `package-lock.json`. A boundary identifies this rule when shrinkwrap is selected. This does not establish the file a particular npm invocation would use: [npm 12 no longer reads shrinkwrap files](https://docs.npmjs.com/cli/v12/configuring-npm/package-lock-json/).

Other package managers are recognized but not analyzed. An explicit non-npm `packageManager` in a project's `package.json` makes it `unsupported` (`npm-alternative-package-manager`), even beside an npm lockfile. Without an npm lockfile of its own, a project is also `unsupported` when its directory holds a Yarn, pnpm, or Bun lockfile (`yarn.lock`, `pnpm-lock.yaml`, `bun.lock`, `bun.lockb`; reason `npm-alternative-lockfile-format`) or a `pnpm-workspace.yaml` or `rush.json` (`npm-alternative-package-manager`). Such files above the project, or a non-npm `packageManager` in the nearest `package.json` above it that declares one (Corepack's rule), leave a project that declares dependencies `indeterminate` (`npm-ancestor-alternative-package-manager`); a project that declares none stays `not_applicable`.

Without `--npm-workspace-locks`, an ancestor npm lockfile leaves ownership `indeterminate` (`workspace-lockfile-owner-unresolved`). With `--npm-workspace-locks`, dircue applies npm's workspace-root selection, verified against npm 11.12.1 and 11.19.0: the nearest ancestor whose `workspaces` list includes the project owns its lockfile; ancestors that do not list the project are skipped. A member is compared against its own `packages["<member path>"]` entry in that lock, not the root package entry. npm selects the workspace root with `npm prefix`; package-lock and shrinkwrap files inside a listed member do not own that workspace, even when the root has no lockfile. At the workspace root, `npm-shrinkwrap.json` takes precedence over `package-lock.json`. An ancestor lock that no listing root owns adds the boundary `npm-ancestor-lock-not-shared`, and the project's state follows the rules for a project without a lockfile of its own. The lockfile module's `semantics` array includes `npm-workspace-member-lock-descriptors-v1` when `--npm-workspace-locks` applies. Assessment always applies this flag. A listed path without a `package.json` is skipped, as npm skips it. Nested workspace roots, incomplete or unparsed ancestors, unresolved member references, and duplicate workspace names remain `indeterminate`. In this mode a Yarn, pnpm, or Bun lockfile above a project matters only through workspace ownership: a member of a workspace whose root holds one is `unsupported`, and an unlisted project is not its owner. A `pnpm-workspace.yaml`, `rush.json`, or `packageManager` above a project still leaves it `indeterminate`.

Supported npm lockfile versions and root package fields follow npm's [package-lock format](https://docs.npmjs.com/cli/v10/configuring-npm/package-lock-json/) and [lockfile-version documentation](https://docs.npmjs.com/cli/v10/using-npm/config/). npm documents the [shrinkwrap precedence rule](https://docs.npmjs.com/cli/v11/configuring-npm/npm-shrinkwrap-json/).

## NuGet

Dircue reads `packages.lock.json` format versions 1 and 2 for `.csproj`, `.fsproj`, and `.vbproj` projects. Its named check, `nuget-observed-direct-package-presence`, asks whether each statically observed `PackageReference` ID appears as a `Direct` entry in the selected lockfile's target groups. This is a set-inclusion check: it does not compare requested or resolved versions, select a target framework, evaluate MSBuild, inspect restore output, or establish restore consistency. `PackageVersion` entries from central package management are never promoted to direct `PackageReference` declarations. Multiple target groups and v2 `Project` or `CentralTransitive` records are disclosed separately.

NuGet reports keep three questions separate:

- `nuget_evidence.presence_state` describes regular candidate files in the selected, bounded snapshot. `observed` means at least one such path exists; invalid contents do not erase observed presence, and presence says nothing about ownership or restore use. `candidate_count` counts observed paths, and `candidate_paths` is a sorted sample capped at 16 with any remainder in `omitted_candidate_paths`. `not_observed` means none matched the bounded known paths; it is not a claim that no conceivable custom path exists. `unknown` means the bounded inventory could not settle candidate presence. `presence_reasons` can include `nuget-candidate-not-regular` or `nuget-custom-lock-path-unresolved`.
- `ownership_state` records the project-to-path ownership decision before lockfile bytes are parsed. It can remain `observed` when the selected file is unreadable, invalid JSON, or an unsupported lockfile version; those are content/check outcomes and do not undo the path ownership observation. It is otherwise the same state as `association_state`. Ownership can also be `indeterminate` while candidate presence is `observed`, for example when two projects claim the same path or a custom path cannot be classified. `ownership_reasons` explain path-ownership uncertainty.
- Each check reports only its named syntactic comparison. A check is not run when there is no observed supported lock association. A `match` never establishes version consistency or a successful NuGet restore.

The standard same-directory `packages.lock.json` is associated only when one project can be identified as its owner. A `packages.<project>.lock.json` candidate is associated only when its project name has one unambiguous match. Other retained MSBuild project types, including `.vcxproj` and `.sqlproj`, count as possible owners even though they do not receive NuGet contexts. Custom paths are also checked against other projects' known custom claims, conventional names, and unresolved possible owners, including projects in other directories. A conflicting custom/conventional claim makes ownership indeterminate for both projects.

For ownership only, the bounded static subset recognizes an unconditional literal `NuGetLockFilePath` in the selected project or nearest selected shared MSBuild inputs. It treats a plain relative lock path as project-relative, accepts a leading `$(MSBuildProjectDirectory)` or `$(MSBuildThisFileDirectory)` anchor, and substitutes `$(MSBuildProjectName)` in a path. The path must remain within the selected source root and resolve to a selected regular file to be observed. Custom filenames may use any extension; the scanner indexes bounded path metadata for the selected snapshot and reads candidate content only when needed. It does not open arbitrary host paths. Reserved directory macros in other positions, differently cased or unknown properties, conditions, property functions, dynamic values, absolute paths, and backslash separators are unresolved rather than guessed. A known custom path that is not present can support a `missing` association only when its possible owners are known and unique.

The static precedence subset is limited to facts verified with controlled .NET SDK fixtures. It applies implicit `Directory.Build.props` and `Directory.Build.targets` only for the exact root SDK identities `Microsoft.NET.Sdk`, `Microsoft.NET.Sdk.Web`, `Microsoft.NET.Sdk.Razor`, and `Microsoft.NET.Sdk.Worker`. A bare `<Project>` does not imply those imports; unknown or custom SDK forms and child-form SDK declarations remain unresolved. Unconditional literals are applied in order, with `Directory.Build.props` before the project body and `Directory.Build.targets` after it; later assignments win. Selected, unconditional literal imports in shared props/targets are traversed in XML order, so later imported assignments override earlier ones. Imports are confined to selected snapshot files and bounded by the selected-input byte/file limits and an eight-level recursion limit. Cycles, conditions, wildcards, dynamic or out-of-root paths, missing or case-aliased targets, and unsupported XML remain unresolved. Settings that control shared-file import enablement or paths, including `ImportDirectoryBuildProps`, `ImportDirectoryBuildTargets`, `DirectoryBuildPropsPath`, `DirectoryBuildTargetsPath`, and `DirectoryPackagesPropsPath`, keep shared-file applicability or the affected path uncertain; Dircue does not assume default imports when selected XML overrides them. The analyzer does not implement general MSBuild property evaluation or observe command-line/global properties. A `NuGetLockFilePath` assignment in `Directory.Packages.props` remains unresolved because its precedence was not established in this subset.

The nearest selected `Directory.Build.props`, `Directory.Build.targets`, and `Directory.Packages.props` are inspected as shared inputs. Empty `Directory.Build` files and inputs restricted to central `PackageVersion` declarations plus central-version settings are safe for this direct-ID set-inclusion check: they can affect versions but do not add direct `PackageReference` IDs. Shared `PackageReference` additions, updates/removals, unclassified items or properties, and other effects keep the check indeterminate (`nuget-shared-input-effect-unresolved`). Explicit project imports remain unresolved because their effects on declarations are not generally evaluated. A bounded shared import may be used to resolve literal lock-path precedence, but that does not turn its arbitrary package-item effects into a supported direct declaration table.

Omitted project inventory blocks NuGet ownership when an omitted path or tree could hide another MSBuild project, or omissions cannot be attributed to paths. For custom paths, any unclassified possible MSBuild owner can point at the selected path and therefore keeps ownership unresolved. An unreadable directory blocks only the decisions its contents could change; files that are not manifests, such as symbolic links in `node_modules/.bin`, block none.

Lockfile format versions and restore behavior are described in Microsoft's [PackageReference documentation](https://learn.microsoft.com/en-us/nuget/consume-packages/package-references-in-project-files); the custom path property is documented in [MSBuild NuGet targets](https://learn.microsoft.com/en-us/nuget/reference/msbuild-targets). NuGet's v2 `Project` and `CentralTransitive` entry types are illustrated in [NuGet/Home issue 14102](https://github.com/NuGet/Home/issues/14102).

## Coverage and source selection

`association_state` distinguishes `observed`, `missing`, `unsupported`, `indeterminate`, and `not_applicable`. A project without a lockfile is `missing` when it declares dependencies or workspaces and `not_applicable` when it does not. An npm project whose dependency or workspace fields have diagnostics establishes neither and is `indeterminate` (`npm-manifest-declarations-unresolved`). With incomplete inventory, absence cannot be established and remains `indeterminate`. Unsupported formats and ambiguous ownership also remain distinct from absence. NuGet `presence_state` is not an alias for any association state: a regular file can be observed even when its owner is unknown, and bounded non-observation is not universal absence.

Duplicate NuGet package IDs within a target group, compared without regard to case, make the check `indeterminate`. Case-variant lockfile names remain indeterminate because a selected source tree does not establish the host filesystem's case behavior. An observed association identifies a candidate under the documented ownership rule; it does not prove which file restore would use, and its check can still be `indeterminate`.

Coverage describes the supported project contexts and named checks, not a parse of every lockfile in the directory. A lower-priority `package-lock.json` is not read when shrinkwrap takes precedence. Orphan lockfiles without a supported project context do not establish a comparison population.

Git scans read the selected commit or tree, not dirty working-tree changes. Directory scans read the current selected directory, including untracked files. In either mode, lockfile contents come from that same selected source. The analyzer rejects conflicting declaration-source metadata, and saved aggregate reports must bind lockfile observations to the declaration source they reused. Symlink and other non-regular candidates are not followed as lockfile content.

The module never runs npm, NuGet, MSBuild, install scripts, or restore; it does not contact registries or use ambient package-manager configuration. Defaults are disclosed in the report's `limits` object:

| Bound | Default |
| --- | ---: |
| Selected relevant inventory paths | 200,000 |
| Lockfile candidates admitted | 256 |
| Bytes in one lockfile or selected MSBuild XML input | 1 MiB |
| Total lockfile and selected MSBuild XML input bytes read | 16 MiB |
| Package names in comparisons | 65,536 |
| Project contexts | 4,096 |
| Serialized module report | 16 MiB |

`--max-file-bytes` can lower the file-read ceiling for a scan. Input and observation bounds, or lost selected-file coverage, are disclosed through partial or skipped status, coverage counts, boundaries, and diagnostics; they are not treated as successful comparisons. If the serialized report itself exceeds its output ceiling, the command returns an error rather than a truncated success-shaped report.

The Go analyzer rejects negative omission counts, conflicting source identities, and inputs or requested limits above its fixed maxima. Contradictory inventory entries and omitted declaration diagnostics qualify the resulting evidence instead of permitting a certain comparison. Successful reports pass the native report validator before being returned.

## Reason codes

Every boundary in `lockfiles.contexts[].boundaries[].reason` and every non-covered project's reason in `assessment.lockfiles[].outcome_reasons[].reason` is one of the codes below. Assessment adds a few outcome reasons of its own: `ecosystem-outside-association-scope` for projects outside npm and NuGet, `no-direct-declarations` for every `not_applicable` project (its boundaries remain in its context), `no-lockfile-context`, `lockfiles-skipped`, or `lockfiles-not-run` when no lockfile context exists, and `unspecified` otherwise.

| Code | State(s) | Meaning |
| --- | --- | --- |
| `ambiguous-nuget-lockfile-owner` | indeterminate | Multiple candidate lockfiles or NuGet projects in the directory |
| `duplicate-nuget-package-id` | unsupported | A target group has duplicate package IDs (case-insensitive) |
| `file-read-error` | indeterminate | The lockfile could not be read |
| `incomplete-lockfile-read` | indeterminate | The lockfile changed, was incomplete, or exceeded the read limit |
| `invalid-lockfile-json` | unsupported | The lockfile is not valid JSON or has duplicate keys |
| `invalid-npm-direct-entry` | unsupported | An entry in the root entry's direct table is not a string or has an unsupported name |
| `invalid-npm-direct-table` | unsupported | A dependency field in the root package entry is not an object |
| `invalid-npm-package-descriptor` | unsupported | The workspace member's package descriptor in the lockfile is not an object |
| `invalid-nuget-package-entry` | unsupported | A package entry in a NuGet lockfile target group is not an object |
| `invalid-nuget-target` | unsupported | A target group value in a NuGet lockfile is not an object |
| `inventory-incomplete` | indeterminate | Full inventory not established; a missing outcome cannot be confirmed |
| `inventory-incomplete-association` | indeterminate | Full inventory not established; an observed association cannot be confirmed |
| `lockfile-limit` | indeterminate | The lockfile count limit was reached before this lockfile was read |
| `lockfile-not-present` | missing | No lockfile in the project's directory |
| `lockfile-unreadable` | indeterminate | The lockfile is non-regular, has no recorded size, or exceeds the file or input byte limit |
| `multiple-target-frameworks` | observed | The NuGet lockfile has multiple target groups; the check compares without target selection |
| `nested-npm-workspace-owner-unresolved` | indeterminate | The npm member declares its own workspaces; the actual lock owner is unclear |
| `npm-alternative-lockfile-format` | unsupported | A Yarn, pnpm, or Bun lockfile is present at the project's or workspace owner's root |
| `npm-alternative-package-manager` | unsupported | A `pnpm-workspace.yaml`, `rush.json`, or non-npm `packageManager` governs the project's directory or its npm workspace |
| `npm-ancestor-alternative-package-manager` | indeterminate | Another manager's lockfile, workspace file, or `packageManager` above the project leaves its owner unknown |
| `npm-ancestor-lock-not-shared` | missing, not_applicable, indeterminate | An ancestor npm lockfile exists but no listing workspace root owns this project |
| `npm-manifest-declarations-unresolved` | observed, indeterminate | The `package.json` has dependency-field diagnostics, so its full direct table, or whether it declares anything, is unknown |
| `npm-packages-table-missing` | unsupported | The npm lockfile has no `packages` object |
| `npm-root-package-entry-missing` | unsupported | The npm lockfile has no root entry (`packages[""]`) |
| `npm-v11-shrinkwrap-selection` | observed | `npm-shrinkwrap.json` was selected over `package-lock.json` under npm v11 static rules |
| `npm-workspace-lock-member-entry-missing` | indeterminate | The workspace lockfile has no `packages` entry for this member's path |
| `npm-workspace-lock-membership-unresolved` | indeterminate | An ancestor's workspace list has an unresolved member, or lists the project more than once |
| `npm-workspace-lock-owner-incomplete` | indeterminate | A manifest above the project or beneath its workspace owner may have been omitted, or an ancestor is unparsed, incomplete, or has workspace diagnostics, so the owner is unknown |
| `npm-workspace-lock-ownership-observed` | observed | The workspace owner was identified; the member is compared against its own lockfile entry |
| `npm-workspace-root-lockfile-not-present` | missing, not_applicable | The identified workspace owner has no lockfile |
| `nuget-custom-lock-path-unresolved` | indeterminate | A custom path is conditional, dynamic, unsupported, unselected, case-aliased, out of root, or has an unresolved possible owner |
| `nuget-imported-project-input-unresolved` | indeterminate | A project import may affect declarations or lockfile selection and is outside the supported static subset |
| `nuget-lockfile-case-unresolved` | indeterminate | The lockfile name differs from the expected name only by case |
| `nuget-lockfile-owner-unresolved` | indeterminate | The NuGet lockfile owner cannot be determined from the available candidates |
| `nuget-package-type-missing` | unsupported | A package entry in the NuGet lockfile has no `type` field |
| `nuget-project-config-unresolved` | indeterminate | The selected project XML was unavailable, malformed, used an unsupported namespace, or exceeded an input bound |
| `nuget-project-or-central-transitive-unexamined` | observed | The NuGet lockfile v2 has Project or CentralTransitive entries that are not compared |
| `nuget-shared-input-effect-unresolved` | indeterminate, observed | Shared MSBuild input may change the direct PackageReference ID set or a missing/not-applicable ownership result |
| `nuget-targets-missing` | unsupported | The NuGet lockfile has no `dependencies` targets map or it is empty |
| `package-name-limit` | observed | The package-name comparison budget was exhausted |
| `project-declarations-incomplete` | indeterminate | The project's declarations were not parsed or are incomplete |
| `project-inventory-incomplete-association` | indeterminate | NuGet project inventory for this directory is incomplete; an observed association cannot be confirmed |
| `unsupported-npm-lockfile-version` | unsupported | The npm lockfile version is not 2 or 3 |
| `unsupported-nuget-lockfile-version` | unsupported | The NuGet lockfile version is not 1 or 2 |
| `unsupported-nuget-package-type` | unsupported | A package entry has a `type` value other than Direct, Transitive, Project, or CentralTransitive |
| `workspace-lockfile-owner-unresolved` | indeterminate | An ancestor npm lockfile is present but `--npm-workspace-locks` was not applied |
