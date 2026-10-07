# Assessment corpus

Six pinned public repositories check `analyze assessment` against labels derived without dircue. They cover cases that generated fixtures do not: a pnpm workspace (vite), a Yarn 4 workspace (jest), NuGet lockfiles with central package management (stock-indicators), and npm workspaces with independent nested lockfiles and hundreds of fixtures (playwright, npm-cli, aspnetcore).

CI never runs these checks because fetching needs the network. Fetch the pinned commits into the ignored `.cache/corpus`:

```sh
python3 tests/map_corpus/fetch_public.py --manifest tests/assessment/corpus/expectations.json --destination .cache/corpus --id vite --id jest --id stock-indicators --id playwright --id npm-cli --id aspnetcore
```

Then check a built binary:

```sh
python3 tests/assessment/corpus/check.py --binary .cache/assessment140/candidate --corpus-root .cache/corpus
```

`expectations.json` holds two kinds of labels. Count assertions come from `git ls-files` and file inspection; each records its method. Per-manifest npm association labels come from `label.py`, which asks npm itself which workspace root owns each project (`npm prefix --offline`) and then reads that root's `package-lock.json`. Projects npm cannot settle, and projects with Yarn, pnpm, Bun, or Rush evidence, are left unlabeled. Regenerate the npm labels after changing a pin:

```sh
python3 tests/assessment/corpus/label.py --corpus-root .cache/corpus --id playwright --id npm-cli --id aspnetcore --write
```

The labels encode one dircue rule that npm does not answer directly: a project without a lockfile is `missing` when it declares dependencies or workspaces, and `not_applicable` otherwise.

The stock-indicators NuGet missing count was corrected from five to six on 2026-10-06. The original label counted the five documentation examples but omitted `tools/application/Test.Application.csproj`. At the pinned commit that project declares package references, uses `Microsoft.NET.Sdk`, and has no lockfile. Its shared build props set only `NoWarn`, and its nearest package props disable central versions without changing the lock path. The correction and inspection method are recorded in `expectations.json`. A passing run against these corrected labels is regression evidence, not an independent blind accuracy estimate.
