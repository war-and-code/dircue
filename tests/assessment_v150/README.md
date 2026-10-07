# Assessment 1.5.0 acceptance subset

This suite checks selected structural-assessment behaviors against hand-labeled synthetic repositories: project populations, explicit group membership, local references and uncertainty, entry points, and bounded measurements. It is an acceptance subset rather than complete conformance to the supported build systems.

Run it against an already-built candidate binary:

```sh
python3 tests/assessment_v150/run.py --candidate bin/dircue
```

The runner creates temporary fixtures and writes raw results to `.cache/assessment150/receipt.json` by default. It never clones repositories or builds fixture projects. Optional SDK queries run only on generated MSBuild fixtures. If `--dotnet` is omitted, the runner uses `.cache/assessment150/dotnet-sdk/dotnet` when present; an SDK is not required for the offline checks or CI.

## Fixture provenance

Project files and structural expectations are defined in `run.py`. File and byte totals come from the fixture writer's explicit content map before the candidate runs. The inflation pair adds vendor data and generated C# files marked by `.gitattributes`. Relationship expectations follow the literal declarations; a candidate result does not replace an expectation.

## Oracle availability

The optional oracle uses `dotnet msbuild -getProperty/-getItem` on controlled temporary projects. It does not restore packages or invoke build targets. Sanitized expectations are in `oracle_expected.json`; raw output stays under the ignored `.cache/assessment150/`. The queries establish only the tested assignment, import, and SDK behavior. They do not establish general MSBuild or NuGet restore compatibility.
