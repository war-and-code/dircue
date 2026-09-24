# Static .NET graph acceptance

`verify.py` compares project references with an independent XML parser, then checks graph degrees, weak components and cycles using breadth-first search and bitset transitive closure. It reads selected Git blobs for repository cases and ordinary files for directory cases. It does not execute MSBuild, restore packages or run checkout scripts.

The XML oracle checks literal `ProjectReference` paths and explicit ancestor conditions. Expressions remain unresolved. References inside targets and `Choose` containers are counted but excluded from the oracle's certainty assessment. Every included edge in the recorded corpus results also has an independently checked unconditional literal declaration. The graph calculations cover every included edge. `test_oracle.py` verifies that incorrect declarations, degrees, components and cycles fail those checks.

The real corpora use the commits pinned in `tests/performance/corpus.json`. The synthetic case is the original acceptance fixture from `tests/stress/generate.py`: 2,048 projects, 4,094 references, wide fan-out, nested directories and a cycle spanning every project. Its independent generator edge list must match exactly. The small fixture adds conditional and missing references and isolated components.

| Corpus | Project nodes | Included edges | Weak components | Cyclic components | Status |
| --- | ---: | ---: | ---: | ---: | --- |
| Roslyn | 508 | 1,605 | 140 | 1 | Partial |
| ASP.NET Core | 605 | 128 | 496 | 0 | Partial |
| Synthetic 2,048 | 2,048 | 4,094 | 1 | 1 | Complete |
| Small fixture | 4 | 3 | 3 | 2 | Partial |

These are static declaration graphs. ASP.NET Core has numerous property-based references, and shared configuration declarations do not establish which projects import them. Its 128 included edges therefore do not describe the complete evaluated build graph. The receipts preserve conditional, unresolved, missing and excluded observations rather than treating them as absent dependencies.

Run from the repository root after preparing pinned corpora and a candidate build-input record:

```sh
python3 -m unittest discover -s tests/projects/graph -p 'test_*.py'
python3 tests/stress/generate.py --profile acceptance \
  --scenario dotnet-graph --output .cache/graph-fixture
python3 tests/projects/graph/verify.py \
  --binary .cache/next-sprint/dircue-integrated \
  --build-inputs .cache/next-sprint/integrated-build-inputs.json \
  --cache .cache/graph-reports --output .cache/graph-correctness.json \
  --synthetic-expected .cache/graph-fixture/dotnet-graph/graph.json \
  roslyn=git=.cache/corpus/roslyn \
  aspnetcore=git=.cache/corpus/aspnetcore \
  synthetic2048=directory=.cache/graph-fixture/dotnet-graph/flat \
  small=directory=tests/projects/graph/dotnet
```

`benchmark.py` measures the complete `analyze projects --json` and `analyze graph --json` commands with the same binary, source and environment. Each gets one warmup and three measured runs in alternating order. Graph output includes the project report, so the difference includes graph construction and additional JSON serialization. RSS is the child process maximum reported by `/usr/bin/time`; wall time includes process launch and captured output. Three local warm samples support a median and range, not a production p95 or a cold-cache claim.

The JSON receipts in `results/` bind the candidate binary, build inputs, harnesses and corpus manifests. Their source commit is the build's actual recorded base, with its dirty state and exact file hashes retained; later commits do not retroactively change that provenance.

```sh
GOMAXPROCS=2 python3 tests/projects/graph/benchmark.py \
  --binary .cache/next-sprint/dircue-integrated \
  --build-inputs .cache/next-sprint/integrated-build-inputs.json \
  --output .cache/graph-performance.json \
  roslyn=git=.cache/corpus/roslyn \
  aspnetcore=git=.cache/corpus/aspnetcore \
  synthetic2048=directory=.cache/graph-fixture/dotnet-graph/flat
```

The recorded September 19, 2026 run used an Apple M1 Max with 32 GiB RAM, Go 1.26.6 and `GOMAXPROCS=2`. Other project fuzzing, builds and container measurements were paused during the measurement window.

| Corpus | Projects median | Graph median | Difference | Graph peak RSS |
| --- | ---: | ---: | ---: | ---: |
| Roslyn | 7.038 s | 7.060 s | +21 ms | 369.4 MiB |
| ASP.NET Core | 2.313 s | 2.327 s | +14 ms | 217.3 MiB |
| Synthetic 2,048 | 0.539 s | 0.583 s | +44 ms | 67.8 MiB |

The small differences on the real repositories are within the scale of sample variation. The synthetic case isolates a much denser graph and showed an 8.2% median increase. These measurements include inventory and project analysis as well as graph construction.
