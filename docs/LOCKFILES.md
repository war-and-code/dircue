# Lockfile observations

Lockfile analysis is an optional, bounded view of selected npm and NuGet files. It reports which project and lockfile were associated, then runs a small named check. A `match` is only a match for that check; it does not mean an install or restore would reproduce the same graph.

```sh
dircue analyze lockfiles --json /path/to/checkout
dircue analyze all --lockfiles --json /path/to/checkout
dircue analyze lockfiles --source directory --json /path/to/working-tree
dircue capabilities --schema lockfiles
```

`analyze all` leaves lockfile analysis off unless `--lockfiles`, `--npm-workspace-locks`, or `--assessment` is supplied. The first two commands emit the versioned profile with a `lockfiles` module and profile `schema_version` `1.8.0`; assessment uses profile `1.9.0`. `capabilities --schema lockfiles` exports the standalone component schema. Check the report's module `status`, `coverage`, `association_state`, and each check's `status` before interpreting a result. Module status describes observation coverage, not dependency health.

## npm

Dircue reads root `package.json` declarations and the root package entry (`packages[""]`) in npm lockfile format versions 2 and 3. It compares dependency names and the exact declaration text in `dependencies`, `devDependencies`, `optionalDependencies`, and `peerDependencies`. A text difference is reported as `different`; dircue does not interpret semver ranges, resolve aliases or Git references, or compare the installed or transitive graph. A comparison is `indeterminate` for aliases, Git references or other declarations outside the bounded matcher.

At a package root, dircue applies npm 11's static selection rule: `npm-shrinkwrap.json` takes precedence over `package-lock.json`. A boundary identifies this rule when shrinkwrap is selected. This does not establish the file a particular npm invocation would use: [npm 12 no longer reads shrinkwrap files](https://docs.npmjs.com/cli/v12/configuring-npm/package-lock-json/).

Other package managers are recognized but not analyzed. An explicit non-npm `packageManager` in a project's `package.json` makes it `unsupported` (`npm-alternative-package-manager`), even beside an npm lockfile. Without an npm lockfile of its own, a project is also `unsupported` when its directory holds a Yarn, pnpm, or Bun lockfile (`yarn.lock`, `pnpm-lock.yaml`, `bun.lock`, `bun.lockb`; reason `npm-alternative-lockfile-format`) or a `pnpm-workspace.yaml` or `rush.json` (`npm-alternative-package-manager`). Such files above the project, or a non-npm `packageManager` in the nearest `package.json` above it that declares one (Corepack's rule), leave a project that declares dependencies `indeterminate` (`npm-ancestor-alternative-package-manager`); a project that declares none stays `not_applicable`.

Without `--npm-workspace-locks`, an ancestor npm lockfile leaves ownership `indeterminate` (`workspace-lockfile-owner-unresolved`). With `--npm-workspace-locks`, dircue applies npm's workspace-root selection, verified against npm 11.12.1 and 11.19.0: the nearest ancestor whose `workspaces` list includes the project owns its lockfile; ancestors that do not list the project are skipped. A member is compared against its own `packages["<member path>"]` entry in that lock, not the root package entry. npm selects the workspace root with `npm prefix`; package-lock and shrinkwrap files inside a listed member do not own that workspace, even when the root has no lockfile. At the workspace root, `npm-shrinkwrap.json` takes precedence over `package-lock.json`. An ancestor lock that no listing root owns adds the boundary `npm-ancestor-lock-not-shared`, and the project's state follows the rules for a project without a lockfile of its own. The lockfile module's `semantics` array includes `npm-workspace-member-lock-descriptors-v1` when `--npm-workspace-locks` applies. Assessment always applies this flag. A listed path without a `package.json` is skipped, as npm skips it. Nested workspace roots, incomplete or unparsed ancestors, unresolved member references, and duplicate workspace names remain `indeterminate`. In this mode a Yarn, pnpm, or Bun lockfile above a project matters only through workspace ownership: a member of a workspace whose root holds one is `unsupported`, and an unlisted project is not its owner. A `pnpm-workspace.yaml`, `rush.json`, or `packageManager` above a project still leaves it `indeterminate`.

Supported npm lockfile versions and root package fields follow npm's [package-lock format](https://docs.npmjs.com/cli/v10/configuring-npm/package-lock-json/) and [lockfile-version documentation](https://docs.npmjs.com/cli/v10/using-npm/config/). npm documents the [shrinkwrap precedence rule](https://docs.npmjs.com/cli/v11/configuring-npm/npm-shrinkwrap-json/).

## NuGet

Dircue reads `packages.lock.json` format versions 1 and 2 for `.csproj`, `.fsproj`, and `.vbproj` projects. Its named check asks whether each statically observed, unconditional `PackageReference` ID appears as a `Direct` entry in a single lockfile target group. It does not compare requested or resolved versions, evaluate MSBuild, inspect project restore output, or establish restore consistency. Multiple target groups, conditional references, and v2 `Project` or `CentralTransitive` records are reported as boundaries or indeterminate evidence.

The standard same-directory `packages.lock.json` is associated only when one project can be identified as its owner. A `packages.<project>.lock.json` candidate is associated only when its project name has one unambiguous match. Arbitrary `NuGetLockFilePath` settings are not evaluated. Dircue does not evaluate imported MSBuild files. Explicit project imports, or selected shared inputs such as `Directory.Build.props`, `Directory.Build.targets` and `Directory.Packages.props`, keep the comparison indeterminate because they may affect package declarations or lockfile ownership. Other MSBuild project types, such as `.vcxproj` and `.sqlproj`, still count when deciding whether a shared lockfile has one owner but do not receive their own NuGet contexts.

Omitted project inventory blocks a NuGet association only when an omitted path in the project's directory could be an MSBuild project file, or omissions cannot be attributed to paths. An unreadable directory blocks only the decisions its contents could change; files that are not manifests, such as symbolic links in `node_modules/.bin`, block none.

Lockfile format versions and restore behavior are described in Microsoft's [PackageReference documentation](https://learn.microsoft.com/en-us/nuget/consume-packages/package-references-in-project-files); the custom path property is documented in [MSBuild NuGet targets](https://learn.microsoft.com/en-us/nuget/reference/msbuild-targets). NuGet's v2 `Project` and `CentralTransitive` entry types are illustrated in [NuGet/Home issue 14102](https://github.com/NuGet/Home/issues/14102).

## Coverage and source selection

`association_state` distinguishes `observed`, `missing`, `unsupported`, `indeterminate`, and `not_applicable`. A project without a lockfile is `missing` when it declares dependencies or workspaces and `not_applicable` when it does not. An npm project whose dependency or workspace fields have diagnostics establishes neither and is `indeterminate` (`npm-manifest-declarations-unresolved`). With incomplete inventory, absence cannot be established and remains `indeterminate`. Unsupported formats and ambiguous ownership also remain distinct from absence.

Duplicate NuGet package IDs within a target group, compared without regard to case, make the check `indeterminate`. Case-variant lockfile names remain indeterminate because a selected source tree does not establish the host filesystem's case behavior. An observed association identifies a candidate under the documented ownership rule; it does not prove which file restore would use, and its check can still be `indeterminate`.

Coverage describes the supported project contexts and named checks, not a parse of every lockfile in the directory. A lower-priority `package-lock.json` is not read when shrinkwrap takes precedence. Orphan lockfiles without a supported project context do not establish a comparison population.

Git scans read the selected commit or tree, not dirty working-tree changes. Directory scans read the current selected directory, including untracked files. In either mode, lockfile contents come from that same selected source. The analyzer rejects conflicting declaration-source metadata, and saved aggregate reports must bind lockfile observations to the declaration source they reused. Symlink and other non-regular candidates are not followed as lockfile content.

The module never runs npm, NuGet, MSBuild, install scripts, or restore; it does not contact registries or use ambient package-manager configuration. Defaults are disclosed in the report's `limits` object:

| Bound | Default |
| --- | ---: |
| Selected relevant inventory paths | 200,000 |
| Lockfile candidates admitted | 256 |
| Bytes in one lockfile or selected project XML | 1 MiB |
| Total lockfile and selected project XML bytes read | 16 MiB |
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
| `nuget-custom-lock-path-unresolved` | indeterminate | The project declares `NuGetLockFilePath`; the custom path is not evaluated |
| `nuget-imported-project-input-unresolved` | indeterminate | The project has MSBuild `Import` elements that may affect its declarations or lockfile |
| `nuget-lockfile-case-unresolved` | indeterminate | The lockfile name differs from the expected name only by case |
| `nuget-lockfile-owner-unresolved` | indeterminate | The NuGet lockfile owner cannot be determined from the available candidates |
| `nuget-package-type-missing` | unsupported | A package entry in the NuGet lockfile has no `type` field |
| `nuget-project-config-unresolved` | indeterminate | The project XML could not be fully inspected within the input byte limit |
| `nuget-project-or-central-transitive-unexamined` | observed | The NuGet lockfile v2 has Project or CentralTransitive entries that are not compared |
| `nuget-shared-inputs-or-custom-lock-path-unresolved` | indeterminate, observed | Shared MSBuild inputs or a custom lock path may affect declarations or ownership |
| `nuget-targets-missing` | unsupported | The NuGet lockfile has no `dependencies` targets map or it is empty |
| `package-name-limit` | observed | The package-name comparison budget was exhausted |
| `project-declarations-incomplete` | indeterminate | The project's declarations were not parsed or are incomplete |
| `project-inventory-incomplete-association` | indeterminate | NuGet project inventory for this directory is incomplete; an observed association cannot be confirmed |
| `unsupported-npm-lockfile-version` | unsupported | The npm lockfile version is not 2 or 3 |
| `unsupported-nuget-lockfile-version` | unsupported | The NuGet lockfile version is not 1 or 2 |
| `unsupported-nuget-package-type` | unsupported | A package entry has a `type` value other than Direct, Transitive, Project, or CentralTransitive |
| `workspace-lockfile-owner-unresolved` | indeterminate | An ancestor npm lockfile is present but `--npm-workspace-locks` was not applied |
