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

Dircue reads `packages.lock.json` format versions 1 and 2 for `.csproj`, `.fsproj`, and `.vbproj` projects. Its named check, `nuget-observed-direct-package-presence`, asks whether each `PackageReference` ID kept by the static evaluation below appears as a `Direct` entry in the selected lockfile's target groups. This is a set-inclusion check: it does not compare requested or resolved versions, select a target framework, run MSBuild, inspect restore output, or establish restore consistency. `PackageVersion` entries from central package management are never promoted to direct `PackageReference` declarations. Multiple target groups and v2 `Project` or `CentralTransitive` records are disclosed separately.

NuGet reports keep three questions separate:

- `nuget_evidence.presence_state` describes regular candidate files in the selected, bounded snapshot: the conventional names in the project directory plus every path the project's possible `NuGetLockFilePath` values can name. `observed` means at least one such path exists; invalid contents do not erase observed presence, and presence says nothing about ownership or restore use. `candidate_count` counts observed paths exactly, and `candidate_paths` is a sorted sample capped at 16 with any remainder in `omitted_candidate_paths`. `not_observed` means no possible path exists in the snapshot. `unknown` means the inventory is incomplete or a possible value can name any path; `presence_reasons` say which.
- `ownership_state` records the project-to-path ownership decision before lockfile bytes are parsed. It can remain `observed` when the selected file is unreadable, invalid JSON, or an unsupported lockfile version; those are content outcomes and do not undo the ownership observation. It is otherwise the same state as `association_state`, and `ownership_reasons` explain it. `lock_path_basis` says how the path was decided: `conventional`, `custom_literal`, `conditional`, `pattern`, `outside_snapshot`, `open_evidence` (an expression or task can name any path), `open_unmodeled` (an SDK or import outside the model can set it), or `unresolved` (the project file could not be evaluated).
- `check_state` is the named check's status, or `not_compared` when no check ran. `check_reasons` uses the codes below plus `no-direct-declarations` when the evaluation kept no package references and `lockfile-not-associated` when no lockfile was associated. A `match` never establishes version consistency or a successful NuGet restore.

`causes` names up to eight selected files behind these qualifications, each with a reason code from the table below and a short detail such as the SDK name, the import, or the conditional assignment; `omitted_causes` counts the rest. A cause is always a path in the snapshot, so a reader can fix or inspect the file that made a result uncertain.

### Static MSBuild evaluation

Dircue evaluates each MSBuild project file statically, in the order MSBuild imports files for the modeled SDKs: `Microsoft.NET.Sdk`, `Microsoft.NET.Sdk.Web`, `Microsoft.NET.Sdk.Razor`, `Microsoft.NET.Sdk.Worker`, `Microsoft.NET.Sdk.BlazorWebAssembly`, and `Microsoft.NET.Sdk.WindowsDesktop`, in the `Sdk` attribute or an `<Sdk>` element. A project without an SDK gets the same phases only where it imports `Microsoft.Common.props` and the C#, Visual Basic, or common targets through the standard toolset paths. The props phase imports `CustomBeforeDirectoryBuildProps`, the nearest `Directory.Build.props`, `CustomAfterDirectoryBuildProps`, the `Microsoft.Common.props` hooks, and the nearest `Directory.Packages.props`. The project body follows. The targets phase imports the language and common target hooks, the project's `.user` file, `CustomBeforeDirectoryBuildTargets`, the nearest `Directory.Build.targets`, and `CustomAfterDirectoryBuildTargets`. Explicit `Import` elements are followed in XML order wherever they appear, with MSBuild's rule that a file already imported into the evaluation is skipped. MSBuild finds Directory files by exact name, so where only a differently cased file exists, it is imported on case-insensitive file systems and skipped on others; both results stay possible.

The evaluation tracks `NuGetLockFilePath`, the `Import*`, `*Path`, and `Custom*` properties that change these imports, and `PackageReference` and `GlobalPackageReference` `Include`, `Remove`, and `Exclude`. Property names are compared case-insensitively. An unconditional evaluation-time assignment replaces earlier evaluation-time values; a conditional assignment or an assignment inside `Choose` keeps every value it could produce possible. Assignments inside a `Target` run after evaluation, so their possible values remain conditional even when a later XML element assigns an evaluation-time literal. The idiom `Condition="'$(NuGetLockFilePath)' == ''"` assigns only while the path is unset, so it is applied exactly. Values may use `$(MSBuildThisFileDirectory)`, `$(MSBuildProjectDirectory)`, `$(MSBuildProjectName)`, `$(MSBuildProjectFile)`, `$([MSBuild]::GetPathOfFileAbove(...))`, and `$([MSBuild]::GetDirectoryNameOfFileAbove(...))`. `$(MSBuildProjectDirectory)` has no trailing separator, so `$(MSBuildProjectDirectory)lock.json` names a sibling file. Other expressions remain unresolved. Dircue may use a pattern such as `locks/$(TargetFramework).json` to find possible files, but a property can contain separators or `..`, so that pattern cannot prove absence or exclusive ownership. An unresolved final path retains an open claim over selected paths. A backslash in an import is a directory separator; in `NuGetLockFilePath` it is one only on Windows, so both spellings stay possible. A relative lock path is joined to the project directory, and spaces in the conventional project-specific name become underscores, as NuGet does.

Version-qualified project SDK references can select different props and targets through [MSBuild SDK resolution](https://learn.microsoft.com/en-us/visualstudio/msbuild/how-to-use-project-sdk#how-project-sdks-are-resolved), so their identity is outside the bundled-SDK model even when the name matches a supported family. Selected ancestor `global.json` files are checked for matching `msbuild-sdks` overrides; their ordinary `sdk.version` field selects the .NET SDK and is not a project-SDK override. Ancestor configurations are considered conservatively because MSBuild can start its search at a solution directory or project directory; a nearer ordinary configuration does not prove that a parent override is irrelevant. Dircue does not select an invocation or solution-specific SDK-resolution context.

Some inputs are outside the model and qualify only the project that uses them, with `nuget-msbuild-input-unmodeled`: another SDK, an `Import` with an `Sdk` attribute, a property that redirects SDK imports such as `NuGetTargets` or `MSBuildExtensionsPath`, an unexpanded import path, and imports deeper than 32 levels. The model does not inspect generated package build assets. It cannot establish which package targets a later build will import. MSBuild's upward search for Directory files stops at the snapshot root here; scanning a subdirectory cannot see a parent repository's files. Command-line and environment properties are not observed.

A conventional `packages.<project>.lock.json` is preferred over `packages.lock.json`, as NuGet does. A path is owned by a project only when no other evaluated MSBuild project can name it. Other retained MSBuild project types, including `.vcxproj` and `.sqlproj`, are evaluated as possible owners. A project whose lock path is open, or whose file could not be read or parsed, can name any path and qualifies every owner, and the `ambiguous-nuget-lockfile-owner` boundary names that project. A project with an unmodeled SDK does not qualify other projects: the SDK can change only its own path. A project that MSBuild would reject, such as one with a missing unconditional import, writes no lockfile and claims nothing.

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
| `ambiguous-nuget-lockfile-owner` | indeterminate | Another evaluated MSBuild project can also name this lockfile; the boundary path is that project |
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
| `lockfile-not-present` | missing | No lockfile exists at any path the project can use |
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
| `nuget-custom-lock-path-unresolved` | indeterminate | A `NuGetLockFilePath` value is outside the snapshot or uses an expression that can name any path |
| `nuget-lockfile-case-unresolved` | indeterminate | The lockfile name differs from the expected name only by case |
| `nuget-lockfile-owner-unresolved` | indeterminate | The NuGet lockfile owner cannot be determined from the available candidates |
| `nuget-lock-path-conditional` | indeterminate | Conditional, `Choose`, `Target`, or platform-dependent assignments leave several possible lockfiles, and at least one exists |
| `nuget-msbuild-input-unmodeled` | indeterminate | The project uses an SDK, import, or property outside the static model, which can set its lock path |
| `nuget-package-reference-conditional` | observed | A `PackageReference` is conditional, so the compared ID set may differ by configuration |
| `nuget-package-reference-dynamic` | observed | A `PackageReference` or `Remove` uses an unexpanded expression, so the ID set is not known |
| `nuget-package-type-missing` | unsupported | A package entry in the NuGet lockfile has no `type` field |
| `nuget-project-config-unresolved` | indeterminate | The project XML or an import it needs was unavailable, malformed, used an unsupported namespace, exceeded an input bound, or names a missing unconditional import |
| `nuget-project-or-central-transitive-unexamined` | observed | The NuGet lockfile v2 has Project or CentralTransitive entries that are not compared |
| `nuget-targets-missing` | unsupported | The NuGet lockfile has no `dependencies` targets map or it is empty |
| `package-name-limit` | observed | The package-name comparison budget was exhausted |
| `project-declarations-incomplete` | indeterminate | The project's declarations were not parsed or are incomplete |
| `project-inventory-incomplete-association` | indeterminate | NuGet project inventory for this directory is incomplete; an observed association cannot be confirmed |
| `unsupported-npm-lockfile-version` | unsupported | The npm lockfile version is not 2 or 3 |
| `unsupported-nuget-lockfile-version` | unsupported | The NuGet lockfile version is not 1 or 2 |
| `unsupported-nuget-package-type` | unsupported | A package entry has a `type` value other than Direct, Transitive, Project, or CentralTransitive |
| `workspace-lockfile-owner-unresolved` | indeterminate | An ancestor npm lockfile is present but `--npm-workspace-locks` was not applied |

1.5.0 retires two 1.4.0 NuGet codes. `nuget-imported-project-input-unresolved` and `nuget-shared-inputs-or-custom-lock-path-unresolved` covered every import and shared input as one unknown. Imports and shared inputs are now evaluated, so the same projects report a definite result or the specific code above: `nuget-lock-path-conditional`, `nuget-msbuild-input-unmodeled`, `nuget-custom-lock-path-unresolved`, `nuget-package-reference-conditional`, or `nuget-package-reference-dynamic`, with the responsible file in `nuget_evidence.causes`.
