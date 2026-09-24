# Golden corpus: remaining disagreements

The disagreements between dircue's map and the blind labels in `golden_expectations.json` that remain after the final run. Each has a class:
- **(a) dircue gap:** the source declares the fact, and the map misses it;
- **(c) modeling boundary:** a relationship the 1.0 map does not model.

Corrected label errors are recorded in `golden_expectations.json` (`corrections`), not here.

| Repository | Kind | Item | Class | Why |
|---|---|---|---|---|
| loki | edge FN | `cmd/loki/Dockerfile` builds `loki` | (a) | The Dockerfile copies the repository (`COPY . /src/loki`) and runs `make loki`. Its build context is set by the Makefile, not the Dockerfile, so the Dockerfile's location does not identify the root module statically. |
| loki | edge FN | `cmd/logcli/Dockerfile` builds `loki` | (a) | Same as above. |
| ruff | interface FN | `ruff` CLI binary declared by `pyproject.toml` (`[tool.maturin] bindings = "bin"`) | (a) | The Rust binary is found from `crates/ruff/Cargo.toml`; the maturin binary binding in `pyproject.toml` is not read. |
| ruff | edge FP | `Dockerfile` builds the root Python package `ruff` | (a) | The root Dockerfile builds the Cargo workspace (`COPY crates`, `cargo zigbuild`). The Python package that shares the root directory is linked by co-location; that edge is `partial` (`dockerfile_co_located_with_multiple_components`). |
| spring-petclinic | edge FN | the `petclinic` Kubernetes Deployment uses PostgreSQL | (c) | The Deployment selects the PostgreSQL profile through its environment. The map attributes capabilities to components, not to individual workloads. |
| terraform-aws-vpc | edge FN | `wrappers` depends on the root module (`source = "../"`) | (a) | Terraform `module` source references between local modules are not yet edges. |

Follow-ups: the (a) items are tracked for the next minor release. The (c) item is a documented modeling boundary.
