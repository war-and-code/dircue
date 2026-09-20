# Running with resource budgets

Use operating-system limits when a profiling job must share a machine with
other workloads. Dircue does not currently expose a hard memory-budget or CPU
quota flag. `--workers` controls scanner concurrency; it does not limit all
process memory or CPU use.

For Linux containers, this example assigns one scan a 512 MiB memory limit,
disables swap for that container, and caps CPU bandwidth at half a core:

```sh
docker run --rm --pull=never \
  --memory=512m --memory-swap=512m --cpus=0.5 \
  --network=none --read-only --user=65534:65534 \
  --cap-drop=ALL --security-opt=no-new-privileges --pids-limit=128 \
  --mount "type=bind,src=$PWD/dircue-linux-arm64,dst=/usr/local/bin/dircue,readonly" \
  --mount "type=bind,src=$PWD/checkout,dst=/input,readonly" \
  YOUR_EXISTING_LINUX_ARM64_IMAGE \
  /usr/local/bin/dircue --json /input > report.json.tmp
```

Use a binary and image matching the runner's architecture. The input must be
readable by the chosen container user. Keep the command's exit status and
validate the complete JSON before accepting or renaming the temporary output;
a file's presence does not establish successful completion. A process killed
for exceeding a budget may leave empty or incomplete output.

Docker's equal memory and memory-plus-swap settings disable container swap.
Its CPU quota limits scheduled CPU time over a period, rather than restricting
the process to one particular processor. Six containers with the example's
settings have a combined configured memory allowance of 3 GiB and CPU quota of
three cores. Allow additional capacity for the host, container runtime, and
other jobs. [Docker resource constraints](https://docs.docker.com/engine/containers/resource_constraints/)

## What the limits mean

On cgroup v2, `memory.max` applies to the cgroup, including descendants and
accounted cache and kernel memory. If reclaim cannot satisfy the limit, the
kernel can kill a process in that cgroup. The kernel documents circumstances
where usage can temporarily exceed the boundary, so this is not a promise
that every sampled value will remain below exactly 512 MiB. `memory.peak`
records the group's peak; `memory.events` distinguishes pressure and OOM
events. These are different from one process's RSS. `cpu.stat` exposes CPU
usage and throttling. [Linux cgroup v2 interfaces](https://docs.kernel.org/admin-guide/cgroup-v2.html)

`GOMEMLIMIT` is an optional, soft Go runtime memory target. It is not a hard
RSS limit and does not cover all memory charged to a container or native child
processes. A target too close to the container ceiling can still fail; one
too small for the workload can increase garbage collection and slow execution.
Choose it from measurements of the actual workload rather than treating it as
an alternative to external enforcement. [Go garbage collector guide](https://go.dev/doc/gc-guide#Memory_limit)

For example, this requests a 256 MiB Go runtime target and four file workers:

```sh
GOMEMLIMIT=256MiB dircue --workers 4 --json /path/to/checkout
```

These settings tune execution without requesting fewer files or different
analysis. They can trade speed for memory, but their effect depends on the
workload. The target is not a promise that process RSS stays below 256 MiB.
In particular, `--max-file-bytes` is a different kind of control: it changes
coverage by skipping larger files, and is not a substitute for a memory target.

Keep optional native workers within the same externally limited job when
their memory and CPU should share that budget. A worker running elsewhere
needs its own limits. This test campaign does not characterize BCA workers,
package imports, scc counting, every supported platform, or arbitrary input
sizes.

## Reproducible characterization

The [resource harness](../tests/resources/README.md) runs six scans together
at CPU quotas of 0.5, 1, and 2 cores per container, with 512 MiB memory and no
swap. Its cases cover a pinned Go repository, synthetic Talend-shaped content,
XML logs, and a packed .NET project graph. Each result must match the complete
JSON from an individual unrestricted reference run of the same binary and
input. Successful JSON can still contain a module's explicit partial status;
the harness preserves those statuses rather than relabeling them complete.

The measured cgroup includes the Python supervisor, dircue, their descendants,
and accounted output-file cache. Input and binary mounts are read-only; only
the probe's result directory is writable. Output and wall time are bounded.
When the kernel does not expose `memory.peak`, the receipt says so and records
a sampled memory maximum, which can miss short-lived peaks.
The harness also tries an intentionally insufficient container memory budget
and refuses reports from failed processes, even when captured bytes happen to
form valid JSON.

These measurements characterize particular jobs. They do not establish a
universal minimum memory budget or throughput guarantee. File count, Git
packing, enabled modules, output volume, and concurrent host work all matter.

The [September 19, 2026 Linux/arm64 receipt](../tests/resources/results-linux-arm64.json)
records 18 matching reports and no OOM events in the 512 MiB batches. All six
children overlapped in each batch. The largest sampled cgroup memory value
was 174.21 MiB; the kernel did not expose a true peak counter. The separate
32 MiB probe was OOM-killed with exit 137, including its supervisor, and its
output was rejected. These observations apply to the recorded development
binary and fixture hashes.

A [September 20 follow-up](../tests/resources/README.md#final-reader-follow-up-september-20-2026)
repeated the matrix after the bounded-reader optimization. All 18 constrained
reports again matched their unrestricted references. This validates those
workloads under the selected limits; the two campaigns are not a controlled
performance comparison.

Future cooperative admission and resource flags remain tracked in
[#1](https://github.com/war-and-code/dircue/issues/1) and
[#2](https://github.com/war-and-code/dircue/issues/2).
