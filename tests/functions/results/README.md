# Local function-evidence results

Captured September 19, 2026 on macOS ARM64. These are development-candidate
results, not a release certification or a performance guarantee.

The core SHA256 is
`7c950ff5baa3870fc6fe332f6e98f3443877867bb3852149e1b20609362f43a2`.
The new worker SHA256 is
`5f9a06323fb30b93dbc7e98a52bd74bfd108da009931548775a8102f015919a5`.
The released v0.3.0 core and worker hashes, candidate build metadata, observed
source hashes, input commits and all raw samples are in the receipts.

The protocol receipt passed 67 comparisons of default behavior, 21 opt-in
language fixtures, 11 counterexamples and the actual CLI source/metric checks.
The benchmark passed output comparisons for all eight cases. Existing default
reports matched byte-for-byte. Opt-in retained the existing structural fields;
the cap case also changed the parent status to `partial`, with concrete omitted
function counts.

Median CLI wall time from three rotating warm runs, in milliseconds:

| Input | Released default | Candidate default | With functions | Default / enriched JSON bytes |
| --- | ---: | ---: | ---: | ---: |
| Small Java | 23.07 | 24.94 | 26.13 | 16,410 / 44,210 |
| Small C# | 30.60 | 30.18 | 32.77 | 16,409 / 44,212 |
| Small Python | 21.79 | 22.46 | 25.41 | 15,687 / 47,288 |
| 140 Python functions | 22.56 | 23.18 | 59.32 | 15,667 / 497,886 |
| Spring StringUtils.java | 26.60 | 27.24 | 46.79 | 16,638 / 266,581 |
| Roslyn CSharpSyntaxTree.cs | 28.14 | 28.56 | 48.45 | 16,543 / 258,265 |
| Flask app.py | 26.39 | 27.41 | 38.34 | 15,856 / 153,287 |
| Three small languages together | 36.60 | 36.39 | 42.77 | 26,246 / 111,693 |

Function evidence increases report size and adds work. This sample does not
establish the significance of small default-path timing differences. The cap
case retains 128 of 140 provider spaces and reports 12 omissions. Spring, Roslyn
and Flask retain 64, 62 and 35 spaces respectively without omissions.

CLI maximum RSS ranged from 25.5–33.9 MiB without function evidence and
26.1–36.5 MiB with it across these cases. Separately measured standalone worker
maximum RSS ranged from 2.8–6.8 MiB without evidence and 2.9–7.8 MiB with it.
Those are separate process measurements, not simultaneous process-tree peaks.
Do not add them. The full sample values are retained, not just these ranges.

Timing was coordinated with other agents to avoid builds and test workloads.
The host was not isolated from operating-system activity. No CPU or memory
limits were imposed. All selected source blobs were under 1 MiB, and the warm
runs include startup cost. Results do not extrapolate to entire repositories.
The initial attempt exposed an overly strict harness comparison: truncation
correctly qualifies the parent as partial. An intermediate successful run was
superseded after adding a concrete-omission guard. Both attempts remain in
ignored local cache; only the final complete run is retained here.

Verify the recorded reports and sample summaries without running binaries:

```sh
python3 tests/functions/verify.py \
  --protocol tests/functions/results/protocol-macos-arm64.json.gz \
  --benchmark-directory tests/functions/results/macos-arm64-cost
```

The verifier checks artifact hashes, source fixtures, default equivalence,
source identity, function populations and summary calculations. It cannot
retroactively prove host isolation or reconstruct timing-dependent stdout
for every native sample from a digest.
