# Golden corpus: remaining disagreements

The disagreements between dircue's map and the blind labels in `golden_expectations.json` that remain after the final run. Each has a class:
- **(a) dircue gap:** the source declares the fact, and the map misses it;
- **(c) modeling boundary:** a relationship the 1.0 map does not model.

Corrected label errors are recorded in `golden_expectations.json` (`corrections`), not here.

| Repository | Kind | Item | Class | Why |
|---|---|---|---|---|
| ruff | interface FN | `ruff` CLI binary declared by `pyproject.toml` (`[tool.maturin] bindings = "bin"`) | (a) | The Rust binary is found from `crates/ruff/Cargo.toml`; the maturin binary binding in `pyproject.toml` is not read. |
| ruff | edge FN | `Dockerfile` builds the root Cargo component | (a) | The final stage copies `/ruff` from the build stage, but the build-stage `cp` uses a dynamic target path (`target/$(cat rust_target.txt)/release/ruff`). The artifact flow is not statically verified, so no build edge is emitted. |
| spring-petclinic | edge FN | the `petclinic` Kubernetes Deployment uses PostgreSQL | (c) | The Deployment selects the PostgreSQL profile through its environment. The map attributes capabilities to components, not to individual workloads. |

Follow-ups: the (a) items are tracked for a later release. The (c) item is a documented modeling boundary.
