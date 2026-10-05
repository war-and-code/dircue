# Generated repository assessment checks

The runner exercises a supplied binary against generated, Git-free directories. It does not install packages, run repository code, invoke a resolver, or mutate the supplied binary. Python uses the standard library; schema validation uses the matching checkout's existing Go dependencies with network dependency fetching disabled.

```sh
mkdir -p .cache/assessment140
go build -o .cache/assessment140/candidate .
python3 -m unittest discover -s tests/assessment -p 'test*.py'
python3 tests/assessment/run.py \
  --candidate .cache/assessment140/candidate \
  --output .cache/assessment140/quality-final \
  --seeds 3
```

Event-driven CI runs this suite on Linux, macOS, and Windows and retains its receipts and report outputs as artifacts for seven days.

The native packaging matrix also runs the 1.4 assessment gate through an installed wheel's console launcher on each supported platform. No tag, release, or package publication is created by those smoke tests.

Choose a fresh output directory. Receipts, fixture trees, raw stdout/stderr, input hashes, exported candidate schema, and schema validation logs remain under ignored `.cache/`. Each scan checks fixture hashes before and after execution. `capabilities --schema profile` exports the supplied binary's offline schema; `go test ./tests/assessment -run TestAssessmentGeneratedReports` validates every retained report against that schema and through the matching checkout's native saved-report validator. It rejects unknown fields, missing project metrics, and invalid versions.

Fixture facts are labeled independently: regular-file counts and bytes come from the generated filesystem, manifest counts and bytes from the explicitly generated manifest filenames, and project, workspace, and local-edge counts from generator parameters. The npm forest has a local dependency chain, a distinct shared-lock workspace, and a Python project outside supported native lock association. Separate controls cover NuGet availability states (including an F# project), malformed manifests, unsupported lock versions, unrelated ancestor locks, wrong provider roots, language attributes, path identity, and bounded populations. A 560-file fixture checks grouping of arbitrary .NET project names, case and backslash variants of conventional .NET configuration names, and requirements include names. Generated filename, root, local-edge, and workspace populations exceed 256 while retained evidence stays bounded; a 4,100-manifest fixture exceeds the parser's 4,096-document limit. A semantics fixture checks the Linguist vendored partition, the lockfile filename kind, manifest ecosystem and kind groups, exclusion of virtual workspace roots and tool-settings pyproject files, project roles, and Yarn, pnpm, and `packageManager` evidence. Every report must keep npm and NuGet eligibility equal to covered plus missing plus unknown, and its role and outcome-reason partitions must sum to each lockfile row.

Seven scored relations span additive, multiplicative, permutative, equivalence, inclusive, and invertive categories: irrelevant file addition, forest replication, root renaming, formatting and unrelated metadata, subtree selection, lock removal/restoration, and optional provider independence. Addition followed by renaming supplies a composed relation. Each relation runs over seeded generated populations; seeds vary project/member counts, dependency versions, payload lengths, and replication factors.

Output-corruption probes establish assertion-guard sensitivity to specific planted report defects. Native-validator guards additionally corrupt unsupported ecosystem totals and retained relationship kinds. They are not production-code mutations, an accuracy estimate, or evidence of perfect coverage. The receipt records their scope and exact rejected counts separately from CLI acceptance checks. The suite does not establish Git snapshot behavior, package-manager resolution, or universal filesystem/race correctness.

The Go workspace control distinguishes two parsed modules, two membership edges, and one local replacement from the `go.work` configuration. A POSIX .NET control checks that two distinct selected filenames normalizing to one project identity retain exact metadata and disclose a lower bound for the parsed population.

The nonregular-file control uses an outward-pointing symlink. If the host cannot create symlinks, the receipt records a skip. Literal POSIX backslash/control filenames and the .NET source-alias control are skipped on Windows and disclosed separately; Unicode and space-containing paths still run. FIFO, socket, and device behavior are outside this harness's coverage.
