# prototypes

Feasibility experiments and pre-production drivers. These are
prototypes, not shipped features; nothing here is part of the 1.0
compatibility contract, the release archives, the Docker image, or
ordinary CI.

Current prototypes:

- [`entrypoints/`](entrypoints/README.md): feasibility experiment for
  HTTP-declaration evidence using the existing BCA-owned parse.
- [`structural/`](structural/README.md): the original structural
  worker prototype. The worker source in
  `prototypes/structural/worker` also serves the shipped structural
  adapter; see [docs/STRUCTURE.md](../docs/STRUCTURE.md) for the
  supported worker packaging and interface.

Prototype status is preserved in the subdirectory READMEs when there
is a shipped counterpart. A prototype does not imply an intended
production rollout on any specific schedule; graduation happens when
the shipped surface is designed, tested, and covered by the
compatibility promise.
