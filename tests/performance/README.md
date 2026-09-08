# Repository correctness and performance

The corpus pins eleven public projects across Go, Python, JavaScript, Rust, Ruby, C, TypeScript, PHP, Java, and C#/.NET. Spring Framework, Roslyn, and ASP.NET Core cover large Java and .NET repositories. `corpus.json` records exact upstream object IDs and release or snapshot labels; `fetch.py` records resolved commit IDs and tracked-file counts. The harness reads repository contents without building projects, installing packages, or executing hooks.

The benchmark compares the complete compiled CLI against actual Ruby Linguist 9.7.0 using `--json --breakdown` on identical committed trees. Before timing each project, it requires exact language names, byte totals, percentage strings, and file sets. A mismatch prevents timing that project and fails the run. A slower candidate median also fails a timed run. This acceptance condition applies to the measured corpus and environment, not every possible repository.

Each tool gets three warmups and 20 recorded runs, alternating execution order. Raw wall time, user/system CPU, peak resident memory, and CPU utilization are retained. The report includes median, p95, coefficient of variation, throughput, source revisions, binary SHA-256, output hashes, reference gems, and Linux environment details. p99 and higher fields are observed order statistics; 20 runs cannot establish rare-tail latency. Caches are warm. Cold storage, network filesystems, other CPU architectures, and extended framework/ecosystem analysis are separate workloads.

## Reproduce

Requires Docker, Go 1.26.6+, Git, Python 3, and enough space for approximately 1.7 GiB of fetched checkouts plus a copy on a Docker volume and container images. Run from the repository root. Fetching uses the network; measured scans run with networking disabled. Public checkouts are shallow; the separate [stress suite](../stress/README.md) tests packed history and larger synthetic inputs.

```sh
make reference
python3 tests/performance/fetch.py
mkdir -p .cache/performance bin
# Select arm64 or amd64 to match the Docker engine, not necessarily the host.
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildvcs=false -trimpath \
  -ldflags '-s -w -X dircue/internal/cli.Version=0.1.0' \
  -o bin/dircue-linux-arm64 .
docker volume create dircue-benchmark-corpus
python3 tests/performance/receipt.py --candidate bin/dircue-linux-arm64
```

Copy the corpus to a Docker volume. This puts both tools on the same Linux filesystem and avoids Docker Desktop bind-mount overhead dominating every file read:

```sh
tar -C .cache/corpus -cf - . | docker run --rm -i --network none \
  -v dircue-benchmark-corpus:/corpus \
  dircue-linguist:9.7.0 tar -C /corpus -xf -
docker run --rm --network none \
  -v dircue-benchmark-corpus:/corpus:ro \
  -v "$PWD/bin/dircue-linux-arm64:/candidate/dircue:ro" \
  -v "$PWD/tests/performance:/harness:ro" \
  -v "$PWD/.cache/performance:/results" \
  dircue-linguist:9.7.0 \
  python3 /harness/compare.py --runs 20 --warmup 3
```

Use an otherwise idle engine. Keep the binary and corpus fixed throughout the run. `--project cobra` selects a project (repeatable); `--runs 0` performs correctness comparison without timing. The output JSON is written after each completed project, with full language outputs retained in a neighboring `comparison-details/` directory. Only reports containing `finished_at_utc`, `passed: true`, and `performance_passed: true` constitute a completed timed acceptance run. GNU time supplies target-child peak RSS, avoiding an inherited Python process memory floor; high-resolution wall measurements include the identical launcher overhead for both tools. CPU seconds have GNU time's 0.01-second precision. The harness requires Linux and executes in the reference container.

The named volume can be removed after testing with `docker volume rm dircue-benchmark-corpus`; this does not remove the fetched host checkouts. Preserve reports and build identity before replacing a candidate binary.

To validate the completed raw samples and generate a summary:

```sh
python3 tests/performance/record.py \
  --report .cache/performance/comparison.json \
  --receipt .cache/performance/build-receipt.json
```

Identity capture requires committed production source. Recording rejects partial runs, mismatched binary receipts, unexpected output differences, fewer than 20 samples, inconsistent medians, and slower candidate medians.

## Recorded v0.1 evidence

See [results](results/README.md) for the measurements, raw samples, and build receipt. The results apply to the pinned inputs, machine, and Linguist version tested.
