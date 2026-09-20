# Declaration boundary tests

Run from the repository root:

```sh
go test ./tests/declarations_adversarial -count=1 -timeout=90s
go test -race ./tests/declarations_adversarial -count=1 -timeout=90s
```

The suite checks parser rejection of deep or ambiguous manifests, bounded glob
semantics, deterministic manifest selection after the document cap, cancellation
at the selected-source reader, and static handling of mixed ecosystem declarations.
A temporary Git fixture checks a selected commit against dirty and untracked
working-tree manifests. When the platform permits creating symlinks, it also checks
that an outside manifest is omitted and its contents do not enter the report.

Fixtures are generated locally. They install no packages, invoke no project
commands, and access no network. The Git fixture uses an in-process library.
These tests establish the specified boundary behavior; they do not promise a
universal memory ceiling, latency bound, or atomic snapshot of a live directory.

For constrained execution, cross-compile the test binary for the container's native
architecture, mount only that binary read-only, disable networking, and use explicit
CPU, memory, process, temporary-storage, and test-time limits. Record the source
snapshot, binary digest, toolchain, container image digest, command, and exit status.
Run emulated architectures separately and label them as emulated; their results do
not replace tests on native hardware.

If an amd64 container on an ARM host crashes in Go's runtime, preserve the failure
and test a native build. The Go project has recorded this symptom with older
Docker/emulation installations ([Go issue 76985](https://github.com/golang/go/issues/76985)).
A native amd64 CI run must still pass. Diagnose the environment before changing
application code, compiler flags, or the release toolchain.
