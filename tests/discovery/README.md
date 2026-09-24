# Metadata discovery measurements

Metadata discovery took less time and peak process memory than `analyze all
--projects` in these seven diagnostic cases. The candidate's existing language
and project commands produced **byte-identical JSON** to released dircue 0.3.0
on every input, including committed Git snapshots. These results do not promise
that adding a discovery pass saves time in a larger workflow: discovery does
less work, and any selected follow-ups still have their own cost.

Measured September 19, 2026 on an Apple M1 Max, macOS arm64, with 8 scanner
workers. Each command had one warmup and three measured repetitions, rotating
command order. Times below are medians in seconds.

| Input | Released languages | Candidate languages | Released projects | Candidate projects | Discovery |
|---|---:|---:|---:|---:|---:|
| XML only, 2 GiB | 0.0299 | 0.0305 | 0.0593 | 0.0597 | 0.0214 |
| XML plus a .NET project | 0.0296 | 0.0294 | 0.0588 | 0.0561 | 0.0216 |
| Small Go source collection | 0.0244 | 0.0243 | 0.0245 | 0.0250 | 0.0213 |
| Spring, directory | 1.4165 | 1.4446 | 1.4654 | 1.4325 | 0.9533 |
| Spring, Git | 1.6353 | 1.6260 | 1.6315 | 1.6233 | 1.2485 |
| Roslyn, directory | 3.2154 | 3.1361 | 3.1310 | 3.1682 | 2.7696 |
| Roslyn, Git | 6.6194 | 6.6121 | 9.4739 | 9.5323 | 3.9653 |

“Languages” uses the legacy `--json` command. “Projects” uses
`analyze all --projects --json`; it also classifies languages and parses
supported build declarations. Discovery uses `analyze discovery --json` and
collects filename/extension evidence without reading file payloads for
classification. All commands use `--tree-size 1000000` and an explicit
`--source directory` or `--source git`.

The largest single-process peak RSS among the three samples, in MiB:

| Input | Released projects | Candidate projects | Discovery |
|---|---:|---:|---:|
| XML only, 2 GiB | 76.4 | 84.7 | 25.0 |
| XML plus a .NET project | 69.3 | 72.2 | 24.9 |
| Small Go source collection | 36.2 | 36.4 | 25.6 |
| Spring, directory | 68.3 | 70.5 | 37.2 |
| Spring, Git | 172.2 | 170.9 | 42.5 |
| Roslyn, directory | 99.1 | 94.6 | 36.5 |
| Roslyn, Git | 361.1 | 360.9 | 74.4 |

## Inputs and correctness

The XML and Go fixtures reuse the deterministic generator in
[the staged-analysis harness](../staged_analysis/README.md). Both XML cases
contain 128 fully written XML files of exactly 16 MiB, totaling
2,147,483,648 bytes. Full SHA-256 checks establish their content; they are not
sparse or NUL-filled substitutes. They are synthetic data, not samples from any specific
vendor output. The mixed case's small `App.csproj` remains present in discovery
beside the XML. The Go fixture's `go.mod` remains present too. Filename evidence
does not prove either manifest's validity or identify the XML as logging data.

Spring and Roslyn use the revisions in [the corpus manifest](../performance/corpus.json),
with 11,347 and 35,220 regular files respectively. Both have packed Git objects;
the receipt records their object-store statistics. No repository code is built
or executed. Directory content is hashed before and after measurement, excluding
`.git` directories and non-regular files. Git inventories use committed object
identities and sizes. Independent inventories must equal discovery's exact
file/byte totals, and exclusive category totals must equal the inventory.

Roslyn finds 508 manifest candidates. Its report retains the lexically first
256 and records 252 omitted candidate paths, with `status: partial`; counts
still cover the full inventory. This is intentional bounded evidence, not a
complete list of project paths. Separate evidence budgets preserve shared
configuration and artifacts. A consumer needing exhaustive evidence must
handle the partial status rather than treat the retained paths as exhaustive.

Every timed output must equal its warmup output. Released/candidate language
outputs must match byte for byte, as must released/candidate project outputs.
The [compressed receipt](results/macos-arm64.json.gz) retains all timing samples,
input hashes, commands, binary hashes and exact local Go/embed build-input
hashes. Its 35 adjacent compressed reports preserve the actual process output.

## Reproduce

Use macOS with the pinned source checkouts and generated fixtures already
available. Extract the released macOS arm64 archive to a separate directory.
The measured released binary has SHA-256
`ff0d723411657a61dc4385c84fc011c1392ce7696066fc13f1c663b1c3967ade`.

```sh
python3 tests/discovery/build_candidate.py \
  --output .cache/discovery/dircue \
  --inputs .cache/discovery/build-inputs.json
python3 tests/discovery/benchmark.py \
  --baseline /path/to/released-0.3.0/dircue \
  --candidate .cache/discovery/dircue \
  --build-inputs .cache/discovery/build-inputs.json \
  --fixtures .cache/staged-analysis/fixtures \
  --corpus-root .cache/corpus \
  --output .cache/discovery/results/macos-arm64.json.gz
python3 tests/discovery/verify.py \
  --report .cache/discovery/results/macos-arm64.json.gz
```

The builder uses the same release flags, including a comparison-only `0.3.0`
version string. It records the actual source inputs; it does not produce a new
release. Keep those inputs unchanged during measurement. The measured candidate
SHA-256 is `e69a88a92a812c7402db82840344a8129c9862b57f157f5a526280590e490423`.
It was built from the developing discovery/graph state before package-import
CLI integration. The results apply to that binary, not every later build.

The verifier checks retained reports, hashes, samples, compatibility and
inventory arithmetic. Add `--verify-corpus` to rehash the original input paths.
The measurement harness also verifies the full XML fixture content and checks
that input and candidate hashes remain unchanged through the run.

## Limits

This is a small warm-cache diagnostic, not statistically strong performance
acceptance or a universal regression bound. Core medians vary in both
directions; the run does not establish zero overhead when discovery is disabled.
Other project builds and heavy tests were paused, but documentation/schema work,
a brief skill metadata/digest scan and unrelated host activity were not controlled.

Wall time includes process launch and the `/usr/bin/time` wrapper, excluding
Python JSON parsing and fixture verification. Peak RSS is the largest CLI
process measurement; it excludes the Python harness. No CPU or memory limits
were enforced. No Syft, scc metrics, native structural worker, Bifrost, build or
restore process was invoked.

`classification_bytes_read: 0` describes discovery's payload-classification
work. Existing bounded attribute reads and Git object-storage reads still occur;
Git may reconstruct packed deltas. This is not a claim of zero disk reads.
Neither a complete metadata result nor a missing candidate establishes that
further analysis is safe to skip.
