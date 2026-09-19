# Combined profiling checks

This synthetic C#/Java directory checks that independently selected modules can coexist without changing existing output fields. It contains two .NET projects, one project reference, three source files, and npm/NuGet configuration with conspicuous synthetic credential markers. It does not restore, build, or execute any fixture project.

The baseline requests projects, scc metrics, per-file structural output, and the supplied native worker. The enriched command additionally requests discovery, graph analysis, existing Syft report import, caller-supplied rules, registry declarations, and function evidence. The Syft input is the retained native 1.52.0 fixture; no Syft executable runs.

The checks establish:

- The eight requested profile fields coexist under schema 1.3.0.
- One and eight file workers produce exactly equal JSON reports.
- Removing the explicitly added fields leaves the complete schema-1.2 baseline unchanged.
- Registry output omits the credential marker while retaining two origins.
- Rules match three source files; the graph has two project nodes; native function evidence is present.
- A one-byte file limit prevents registry reads and reports partial coverage.
- A refused tree marks registry and rule analysis skipped.
- All five actual CLI reports validate against the current profile schema.

These are composition checks over a small synthetic directory, not coverage of every input or a performance measurement. Matching source hashes identify the fixture bytes. Reports contain the absolute fixture path used for that run; relocating the fixture changes that path without changing the comparison within a run. Package attribution remains unverified because this fixture does not assert a Syft report-to-tree binding.

## Reproduce

Use a candidate binary and its matching function-capable native worker. Choose a fresh output directory:

```sh
python3 tests/compatibility_next/composition/run.py \
  --candidate /path/to/dircue \
  --worker /path/to/dircue-structural-worker \
  --output .cache/composition-check
python3 tests/compatibility_next/composition/verify_schema.py \
  --output .cache/composition-check
```

The schema helper uses an ephemeral Go test overlay and the project's existing schema dependency. It does not modify runtime or schema files. Its only added test reads the five produced reports. The fixture's registry credentials are synthetic and must never appear in output.

## Retained run

`results/` retains the original audit's five JSON reports, stderr, binary/input hashes, and schema result. Candidate SHA-256 was `0a0587135b7f906db7c2add58623dd4a3908e6d236b8b95cee79fd5941f99214`, built with the comparison-only version label 0.3.0. The native worker hash and input hashes are in `composition-receipt.json`.

The original schema helper initially selected its rules-policy JSON along with profile reports because both have `schema_version`. That audit-only filter was corrected to select reports by their required `root` field; all five CLI reports then validated. No runtime or schema change resulted from that harness mistake. The original runner and corrected overlay are retained beside the receipts; the reusable harness above uses canonical fixture paths.

`results/canonical/` retains the successful rerun using these checked-in fixture and harness paths. All five reports passed the same schema test. `verification.json` records the source-input and fixture checks; `manifest.json` hashes the retained harness, fixtures, and results.
