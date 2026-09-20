# Container resource characterization

This harness checks report correctness while Docker applies cgroup-v2 memory
and CPU limits. It does not change dircue's runtime defaults or add resource
flags. Run it on an otherwise idle Linux/arm64 Docker engine; an ARM Mac with
Docker Desktop supplies a Linux VM, whose storage and scheduling differ from
a native Linux runner.

The six concurrent cases are:

| Case | Input and operation |
| --- | --- |
| Go directory | Pinned Cobra checkout; legacy language JSON |
| Talend directory | 2 GiB synthetic fixture; generated-source exclusion; language JSON |
| XML logs | 1.1 GiB synthetic XML log plus C#; language JSON |
| .NET graph | 2,048 projects in a packed Git repository; graph JSON |
| .NET projects | Same selected packed revision; project JSON |
| Talend packed | Packed Git revision of the synthetic Talend fixture; language JSON |

The Talend and XML fixtures are layout/format approximations, not actual
Talend exports or MOVEit samples. See the [stress fixture description](../stress/README.md).

## Run

First prepare the acceptance fixtures and packed views using the stress
harness, and retain its complete `manifest.json` on the host. The volume is
mounted read-only. Preflight and postflight checks compare the manifest,
hash the selected flat payloads and packed objects, and reject unexpected
object packs, alternates, or loose objects. Git scans select the recorded
commit explicitly. Cobra's selected directory files are hashed before and
after, including untracked files; the receipt also records its Git revision.

Build a comparison binary with an explicit Linux/arm64 target:

```sh
GOOS=linux GOARCH=arm64 python3 tests/discovery/build_candidate.py \
  --output .cache/resources/dircue-linux-arm64 \
  --inputs .cache/resources/build-inputs.json
```

That helper deliberately uses a `0.3.0` version string for comparisons, not
release labeling. Its receipt records exact Go and embedded source inputs.
The resource harness records the Linux/arm64 target and binary SHA-256. Do not
edit the recorded source inputs between building and starting the harness.

Select an existing Python 3.12 Linux/arm64 image by its immutable local image
ID (`docker image inspect ... --format '{{.Id}}'`). No image is downloaded by
the harness. Use a new output directory for each campaign:

```sh
python3 tests/resources/run.py \
  --binary .cache/resources/dircue-linux-arm64 \
  --build-inputs .cache/resources/build-inputs.json \
  --manifest .cache/stress-results/fixture-manifest.json \
  --cobra .cache/corpus/cobra \
  --volume dircue-stress-v1 \
  --image sha256:REPLACE_WITH_EXISTING_IMAGE_ID \
  --output .cache/resources/results
```

If the retained fixture is stored in a different volume subdirectory, add
`--fixture-subdir NAME`; the default is `acceptance`.

The campaign runs six unrestricted references individually, then three batches
of six containers at 0.5, 1, and 2 cores per container. Each constrained scan
gets 512 MiB with swap disabled. A shared start barrier lets the receipt
verify that all six children actually overlapped. The final 32 MiB probe is
deliberately smaller and may fail; its result is separate from the 512 MiB
acceptance result. The default child timeout is 120 seconds; stdout is capped
at 16 MiB and stderr at 1 MiB. A missing supervisor receipt, nonzero process
exit, OOM event, timeout, truncation, invalid JSON, or reference difference
prevents acceptance. The harness removes only the containers it created.

## Evidence and limits

`results.json` records the image, binary and harness hashes, build inputs,
fixture validation, full Docker state, effective cgroup limits, before/after
memory and CPU counters, child timing, and result acceptance. Each case keeps
its raw stdout/stderr, supervisor receipt, and start-barrier receipt. Cgroup
memory includes the supervisor and accounted output cache; it is not dircue's
standalone peak RSS. Host Docker-client overhead is outside that cgroup.
Some cgroup-v2 kernels do not provide `memory.peak`. In that case the receipt
records it as null and reports the maximum observed `memory.current` from
the supervisor's polling loop. That sampled value is only a lower bound on
the true peak. The `memory.max` limit and OOM event checks remain in force.

One observation per configuration is a characterization, not a performance
distribution. No p95 or speedup claims follow from this matrix. Cache state
is uncontrolled and is warmed by validation and references. The harness
does not test a native structural worker, package import, or scc metrics.
See [resource budgets](../../docs/RESOURCE_BUDGETS.md) for operational limits.

## Recorded run

The [Linux/arm64 receipt](results-linux-arm64.json) records the September 19,
2026 run on Docker Desktop. All 18 constrained reports matched their respective
unrestricted references. The .NET project and graph modules reported complete
for these fixtures. No constrained success case recorded an OOM event.

| CPU quota per container | Reports matching | Six-child overlap | Largest sampled cgroup memory | Longest child wall time |
| --- | --- | --- | --- | --- |
| 0.5 cores | 6/6 | 0.183 s | 174.12 MiB | 42.694 s |
| 1 core | 6/6 | 0.144 s | 172.63 MiB | 18.573 s |
| 2 cores | 6/6 | 0.145 s | 174.21 MiB | 13.180 s |

These are single observations, including the supervisor's capture overhead.
The memory figures are sampled lower bounds because this kernel does not
provide `memory.peak`. The 32 MiB negative probe ended with Docker's OOM flag
and exit 137; its supervisor also died, so no final cgroup sample or child exit
receipt exists. The harness rejected the captured output.

The receipt includes the exact build inputs and hashes of retained raw outputs.
The raw files remain in the local campaign output directory; they are not
copied into the repository. Reproduction generates a new set for comparison.

Run the harness's failure-acceptance and subprocess-bound regressions with:

```sh
python3 -m unittest discover -s tests/resources -p 'test_*.py' -v
```

## Final-reader follow-up: September 20, 2026

The [separate follow-up receipt](results-linux-arm64-20260920.json) tests the
final bounded reader, including its growth-cap correction. The September 19
receipt above is unchanged. This is a fresh resource-acceptance campaign;
it does not compare performance with the earlier executable.

All **18 constrained reports matched six fresh unrestricted references** from
the same executable. Every batch showed six-child overlap, and no 512 MiB case
reported an OOM event, timeout, output truncation, nonzero exit or stderr.
The .NET project and graph reports were complete for these fixtures.

| CPU quota per container | Reports matching | Six-child overlap | Largest sampled cgroup memory | Longest child wall time |
| --- | --- | --- | --- | --- |
| 0.5 cores | 6/6 | 0.214 s | 179.29 MiB | 48.154 s |
| 1 core | 6/6 | 0.203 s | 178.85 MiB | 18.269 s |
| 2 cores | 6/6 | 0.170 s | 177.48 MiB | 13.258 s |

The kernel again lacked `memory.peak`; these memory observations are sampled
lower bounds, include the Python supervisor and captured output, and are not
standalone dircue RSS. Each configuration was observed once. The 32 MiB probe
was killed by the cgroup limit with exit 137; its supervisor also died, and
the harness rejected the output. No new runtime defaults or resource flags
were introduced.

The Linux/arm64 executable was cross-compiled with the installed Go 1.26.6
using `CGO_ENABLED=0`, read-only module selection, and disabled dependency and
automatic toolchain downloads. It reports `0.3.0` only for this harness's
comparison convention. Its SHA-256 is
`24bff8fca22aa054d5316317e9dcb91eac693e6a24052ed71c24f610f6a955a5`.
All 321 inputs shared with the frozen final macOS build matched exactly;
the additional Linux-specific go-git source matched the pinned v0.6.0 release.
The receipt records the full selected source hashes, exact build command,
Go build metadata, compiler/tool hashes and frozen-build receipt hash.

Reproduction uses the **Run** commands above with a fresh output directory,
the selected Linux/arm64 image ID, and the retained fixture manifest. This run
used fixture subdirectory `acceptance-final`, with manifest SHA-256
`d7922edd9cbb84976e6ce56afab21dc625e319ea1db1565f0f31f43e78342ff9`.
It used the same immutable Python image and fixture identities as the earlier
campaign. The local volume name is replaced with a placeholder in exported
metadata; supply the volume that contains your verified fixtures.

After the campaign, verification rechecked the executable, source, harness and
tool hashes, fixture identities, each retained stdout/stderr hash, exact
reference bytes, effective memory/swap/CPU limits and module completion.
The public receipt retains hashes of the raw reports and artifacts, while
binaries and full outputs stay in the ignored local campaign directory.
No competing local benchmark ran, but ordinary host and Docker VM activity
was not controlled. These observations establish acceptance for this matrix,
not a general memory ceiling or performance distribution.
