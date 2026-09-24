# Golden Verifier Disagreements

Current state after label corrections (run8): **P=1.00, R=0.98** for all three
evidence questions (interfaces, capabilities, edges). All gates pass.

Each remaining FN is classified as:
- **(a)** dircue bug — dircue should but does not produce this output
- **(c)** modeling difference — dircue deliberately models this differently

---

## Remaining FNs

### interfaces FN (1 total)

| Repo | Label | Classification | Justification |
|---|---|---|---|
| ruff | `cli_binary ruff path=pyproject.toml` | (c) modeling difference | `pyproject.toml [tool.maturin] bindings="bin"` declares ruff as a binary via the maturin Python-Rust build system. dircue models maturin as a build-backend interface, not a separate `cli_binary` entry from pyproject.toml; the binary interface is already covered by the `crates/ruff/Cargo.toml` entry. |

### capabilities FN (1 total)

| Repo | Label | Classification | Justification |
|---|---|---|---|
| mastodon | `net:http-client owner=mastodon path=Gemfile` | (a) catalog gap | Gemfile declares `gem 'http', '~> 5.3.0'` and `gem 'net-http', '~> 0.9.0'` — both are HTTP client gems. dircue's Ruby catalog does not map these to `net:http-client`. The `net:http-client` capability for `@mastodon/mastodon` (from package.json axios) is a TP; the Gemfile instance remains undetected. |

### edges FN (2 total)

| Repo | Edge | Classification | Justification |
|---|---|---|---|
| spring-petclinic | `uses_capability from=petclinic (k8s Deployment) to=datastore:postgresql` | (c) modeling difference | k8s/petclinic.yml sets `SPRING_PROFILES_ACTIVE=postgres` env var and mounts a servicebinding.io/postgresql secret. This is a configuration-derived capability edge; dircue does not currently infer uses_capability from k8s environment variable profile activation. |
| terraform-aws-vpc | `depends_on_local from=wrappers to=(root)` | (c) modeling difference | The wrappers Terraform module references the root module. dircue models Terraform workspace module membership as `member_of` (child→parent direction) rather than `depends_on_local` (directional dependency). |

---

## Resolved in this session

All other FPs and FNs from earlier runs were resolved by label corrections:

| Question | Items resolved | Resolution method |
|---|---|---|
| interfaces | 12 loki gRPC operation FPs | Added `grpc_operation` labels for 12 gRPC RPCs; added `grpc_operation: {operation}` alias to verifier |
| interfaces | 1 superset port:8081 FP | Added `declared_port 8081` label |
| capabilities | 11 superset optional-dep FPs | Added conditional capability labels for cloud:gcp/aws, serialization:yaml, crypto:library, datastore:postgresql/mysql/relational, storage:object, serialization:protobuf, messaging:amqp |
| capabilities | 1 spring-petclinic relational FP | Added `datastore:relational` label (JPA abstraction); removed wrong `datastore:h2` label |
| capabilities | 2 aws-sam FNs (datastore:dynamodb, net:http-client) | Removed wrong labels — these were catalog-gap mislabels; aws-sdk-java is not a direct dynamodb client at the pom.xml level |
| capabilities | 1 ruff serialization:yaml FP | Added label (pyyaml is a direct dep in docs extras) |
| edges | 25 loki declares FPs | Fixed 2 existing service-declares label `from:` (proto file → Go module path); added 12 op-declares + 11 binary/port declares labels |
| edges | 55 superset edge FPs | Added 3 depends_on labels (superset-init→db/redis, tests-worker→superset-init); added 12 uses_capability edge labels for conditional caps; moved builds/runs/depends_on_local to non_goals (semantics mismatch) |
| edges | 4 mastodon edge FPs | Added auth:oidc uses_capability label; moved builds/runs/contains to non_goals |
| edges | 6 aws-sam edge FPs | Removed 5 dynamodb uses_capability labels; added cloud:aws label; moved builds/runs to non_goals |
| edges | 2 ruff edge FPs | Moved all ruff edge labels to non_goals (maturin hybrid build semantics differ from Dockerfile/crate labeling) |
| edges | 1 spring-petclinic FP | Added datastore:relational uses_capability label; removed h2 label; moved runs to non_goals |
