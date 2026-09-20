# Bifrost probes

These synthetic probes evaluate an external Bifrost binary. They do not implement a dircue integration or execute any fixture code. The source and integration assessment is in [BIFROST_EVALUATION.md](../../docs/BIFROST_EVALUATION.md).

Supply a verified Bifrost v0.11.5 binary and a disposable output directory:

```sh
python3 tests/bifrost/probe.py \
  --binary /path/to/bifrost \
  --output .cache/bifrost-probes
```

To run the Linux binary in an already available Linux image:

```sh
python3 tests/bifrost/probe.py \
  --binary /path/to/linux/bifrost \
  --output .cache/bifrost-probes-offline \
  --docker-image python:3.13-slim
```

The container uses the image's resolved local ID with `--pull=never`, no network, UID 65534, a read-only filesystem and source mount, one CPU, a 1 GiB memory limit, and 128 processes. Only a private cache and temporary filesystem are writable. The selected image and binary must have compatible architectures. Native execution explicitly disables semantic-pack acquisition but does not apply an OS network sandbox.

Each run creates a temporary Java/C#/JavaScript workspace, records the binary hash and build identity, executes bounded queries, writes raw stdout/stderr and a JSON receipt, then removes the workspace. The fixture source includes an external symlink canary, never a link to user files. The nine checks cover 13 invocations, including two expected CLI errors. One passing check deliberately reproduces a limitation: a relative explicit source follows that canary outside the workspace. It must not be interpreted as a confinement guarantee.

[results-v0.11.5.json](results-v0.11.5.json) records the reviewed binaries, input hashes, queries, output hashes, and observed checks. Temporary paths in command arguments are normalized. Raw output hashes identify the original run, so stderr containing temporary paths will differ in another run. This is a correctness receipt, not a cross-platform performance comparison.
