# research

Optional research experiments that are not part of the shipped dircue
binary, the wheels, the Docker image, ordinary CI, or the 1.0
compatibility contract. Each subdirectory carries its own README with
scope, dependencies, and reproduction notes.

Current experiments:

- [`bend-aggregation/`](bend-aggregation/README.md): checked models of
  dircue's bounded aggregation logic written in
  [Bend 2](https://github.com/bendlang/bend). This is a
  confidence-building experiment on top of the production Go and Rust
  tests. It adds no runtime dependency to dircue.

Nothing under `research/` is required to build or run dircue. When a
research artifact graduates into a shipped feature, it moves out of
this directory and gets its own place under `pkg/`, `internal/`,
`docs/`, or a dedicated tests tree.
