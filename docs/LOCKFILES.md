# Lockfile observations

Lockfile analysis is an optional, bounded view of selected npm and NuGet files. It reports which project and lockfile were associated, then runs a small named check. A `match` is only a match for that check; it does not mean an install or restore would reproduce the same graph.

```sh
dircue analyze lockfiles --json /path/to/checkout
dircue analyze all --lockfiles --json /path/to/checkout
dircue analyze lockfiles --source directory --json /path/to/working-tree
dircue capabilities --schema lockfiles
```

`analyze all` leaves lockfile analysis off unless `--lockfiles` is supplied. The first two commands emit the versioned profile with a `lockfiles` module and profile `schema_version` `1.8.0`; `capabilities --schema lockfiles` exports the standalone component schema. Check the report's module `status`, `coverage`, `association_state`, and each check's `status` before interpreting a result. Module status describes observation coverage, not dependency health.

## npm

Dircue reads root `package.json` declarations and the root package entry (`packages[""]`) in npm lockfile format versions 2 and 3. It compares dependency names and the exact declaration text in `dependencies`, `devDependencies`, `optionalDependencies`, and `peerDependencies`. A text difference is reported as `different`; dircue does not interpret semver ranges, resolve aliases or Git references, or compare the installed or transitive graph. A comparison is `indeterminate` for aliases, Git references or other declarations outside the bounded matcher.

At a package root, dircue applies npm 11's static selection rule: `npm-shrinkwrap.json` takes precedence over `package-lock.json`. A boundary identifies this rule when shrinkwrap is selected. This does not establish the file a particular npm invocation would use: [npm 12 no longer reads shrinkwrap files](https://docs.npmjs.com/cli/v12/configuring-npm/package-lock-json/). Workspace membership is not inferred from nearby manifests: when a member has only an ancestor lockfile, ownership remains indeterminate. Other managers' lockfiles, including pnpm and Yarn, are not analyzed.

Supported npm lockfile versions and root package fields follow npm's [package-lock format](https://docs.npmjs.com/cli/v10/configuring-npm/package-lock-json/) and [lockfile-version documentation](https://docs.npmjs.com/cli/v10/using-npm/config/). npm documents the [shrinkwrap precedence rule](https://docs.npmjs.com/cli/v11/configuring-npm/npm-shrinkwrap-json/).

## NuGet

Dircue reads `packages.lock.json` format versions 1 and 2 for statically observed `.csproj` projects. Its named check asks whether each statically observed, unconditional `PackageReference` ID appears as a `Direct` entry in a single lockfile target group. It does not compare requested or resolved versions, evaluate MSBuild, inspect project restore output, or establish restore consistency. Multiple target groups, conditional references, and v2 `Project` or `CentralTransitive` records are reported as boundaries or indeterminate evidence.

The standard same-directory `packages.lock.json` is associated only when one project can be identified as its owner. A `packages.<project>.lock.json` candidate is associated only when its project name has one unambiguous match. Arbitrary `NuGetLockFilePath`/`-LockFilePath` settings are not evaluated. Dircue does not evaluate imported MSBuild files. Explicit project imports, or selected shared inputs such as `Directory.Build.props`, `Directory.Build.targets` and `Directory.Packages.props`, keep the comparison indeterminate because they may affect package declarations or lockfile ownership. Lockfile format versions and restore behavior are described in Microsoft's [PackageReference documentation](https://learn.microsoft.com/en-us/nuget/consume-packages/package-references-in-project-files); the custom path property is documented in [MSBuild NuGet targets](https://learn.microsoft.com/en-us/nuget/reference/msbuild-targets). NuGet's v2 `Project` and `CentralTransitive` entry types are illustrated in [NuGet/Home issue 14102](https://github.com/NuGet/Home/issues/14102).

## Coverage and source selection

Omitted project inventory prevents a certain NuGet association. Case-variant lockfile names remain indeterminate because a selected source tree does not establish the host filesystem's case behavior. An observed association identifies a candidate under the documented ownership rule; it does not prove which file restore would use, and its check can still be indeterminate.

`association_state` distinguishes `observed`, `missing`, `unsupported`, `indeterminate`, and `not_applicable`. A missing lockfile is reported as `missing` only when the selected inventory is complete and a relevant declaration exists. With incomplete inventory, absence cannot be established and remains indeterminate. Unsupported formats and ambiguous ownership also remain distinct from absence.

Coverage describes the supported project contexts and named checks, not a parse of every lockfile in the directory. A lower-priority `package-lock.json` is not read when shrinkwrap takes precedence. Orphan lockfiles without a supported project context do not establish a comparison population. Only `.csproj` projects receive NuGet checks; other MSBuild project types are counted when determining whether a shared lockfile has an unambiguous owner.

Git scans read the selected commit or tree, not dirty working-tree changes. Directory scans read the current selected directory, including untracked files. In either mode, lockfile contents come from that same selected source. Symlink and other non-regular candidates are not followed as lockfile content.

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

`--max-file-bytes` can lower the file-read ceiling for a scan. Reaching a bound or losing selected-file coverage is disclosed through partial or skipped status, coverage counts, boundaries, and diagnostics; it is not treated as a successful comparison.
