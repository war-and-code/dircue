# Blind source-first holdout v1

This is a qualitative accuracy holdout frozen before any dircue invocation on either checkout. The labeler used only upstream source at the pinned commits, `docs/MAP.md`, and the declared inventory below. No map output and no per-repository result receipt was read. Full commits, URLs, file paths, and SHA-256 digests are in `labels.json`.

## Reproduce the source snapshots

```sh
mkdir -p .cache/blind-holdout-v1
for spec in \
  'gs-maven https://github.com/spring-guides/gs-maven.git c65883f80b35bac86bb2580944de9146e2c6a55a' \
  'flasky https://github.com/miguelgrinberg/flasky.git 3beedd640b9146b0bd65c8c2efc402b01798bc33'
do
  set -- $spec
  git clone --no-checkout "$2" ".cache/blind-holdout-v1/$1"
  git -C ".cache/blind-holdout-v1/$1" checkout --detach "$3"
done
```

Scan `gs-maven/complete` as its own tree; scan the `flasky` repository root. The checkouts are under 1 MB each at these pins. The `complete` sample is intentionally selected from the Maven tutorial's complete example because the top-level repository also contains separate `initial` and Kotlin examples.

## Oracle boundary and source labels

The scored oracle is limited to the files listed in `labels.json`. Paths below are relative to each upstream repository. A source fact is included only when an oracle file directly declares it or its implementation. Negative facts mean “no supported 1.0 map declaration in this oracle set”; they do not assert the absence of other behavior in the entire repository. `complete` coverage below means all map facts derivable from those files were enumerated by the labeler; it is not a claim that the whole checkout was exhaustively reviewed.

### `gs-maven` (`complete/`)

| Map category | Source-first labels |
|---|---|
| Components | `gs-maven`, one Maven component rooted at `complete/`. `complete/pom.xml:6-9` declares group `org.springframework`, artifact `gs-maven`, version `0.1.0`, and `jar` packaging. Java source and test files are enumerated in `labels.json`. |
| Deployables | None in the 1.0 vocabulary. The POM declares `jar` packaging and configures Maven Shade (`pom.xml:34-55`); 1.0's Maven archive observation is specifically WAR/EAR. No Docker, Compose, Kubernetes, or other deployable declaration appears in the oracle files. |
| Interfaces | None in the supported 1.0 vocabulary. `HelloWorld.main` (`complete/src/main/java/hello/HelloWorld.java:5-11`) is a Java console entry point; it is not a Spring Boot application, HTTP endpoint, or another supported declared interface. |
| Capabilities | None in the bounded capability catalog. `pom.xml:18-22` declares Joda-Time and `HelloWorld.java:3,7` imports and uses `LocalTime`; this is a library use, not a catalogued service capability. `pom.xml:24-30` is test-scope JUnit and is excluded from runtime capabilities. |
| Relationships | None. There is one component, no local project dependency, and no supported deployable/interface/capability endpoint in this slice. The call to `Greeter.sayHello()` (`HelloWorld.java:9-10`) is ordinary in-component code, not a map relationship. |
| Coverage | Components: complete (one declared POM). Deployables, interfaces, capabilities, and relationships: complete for the supported 1.0 declarations visible in the four hashed files; no positive observation is intentionally left unknown. This is the full `complete/` subtree's relevant source inventory, not the tutorial repository's other examples. |

### `flasky` (repository root)

| Map category | Source-first labels |
|---|---|
| Components | One Python/Flask application rooted at `.`. Root `requirements.txt:1-3` includes `requirements/heroku.txt`; the source entry point creates `app` (`flasky.py:20`) and `app/__init__.py:20-23` constructs a Flask app in `create_app`. |
| Deployables | Docker image from the root `Dockerfile` (`Dockerfile:1-21`); Compose services `flasky` and `mysql` (`docker-compose.yml:2-12`). The Dockerfile exposes container port 5000 (`Dockerfile:20`). `Procfile:1` declares a Heroku `web` process; it is recorded as source evidence but is not counted as a 1.0 deployable because the map vocabulary does not model Procfile deployables. |
| Interfaces | Declared container port 5000 from `Dockerfile:20` and the Compose binding `8000:5000` (`docker-compose.yml:3-6`); the Compose interface label names the container port `5000`, while host port `8000` remains source context. No URL routes are promoted to separate interface records by the documented 1.0 interface vocabulary. The Flask app factory is called from `flasky.py:20`; the `Flask(...)` constructor itself is inside `create_app` (`app/__init__.py:20-23`), so it is not a module-level Flask-object assignment. |
| Capabilities | Relational persistence via SQLAlchemy (`requirements/common.txt:14,26`; `app/__init__.py:5,13`). PostgreSQL is declared by `psycopg2` (`requirements/heroku.txt:1-3`). MySQL is declared by `PyMySQL` in the Docker requirements (`requirements/docker.txt:1-3`) and the Compose `mysql` service (`docker-compose.yml:11-14`). The configuration also permits SQLite fallback URLs (`config.py:29-44`); this is recorded as an explicit source fact but its map category is unknown and is not forced into a positive capability label. |
| Relationships | Compose service `flasky` has build context `.` (`docker-compose.yml:3-4`), linking the declared service build to the root component. `uses_capability` relationships connect the component to relational, PostgreSQL, and MySQL declarations, grounded in the paths above. The Compose `links` declaration to `mysql` (`docker-compose.yml:3-10`) is retained as source context; it is not labeled as `depends_on`. No SQLite relationship is scored because category mapping is unresolved. |
| Coverage | Components: complete (one root requirements manifest and one app source tree in this bounded project). Deployables: partial because Compose/Docker declarations are pinned but the Procfile has no 1.0 deployable kind. Interfaces: partial because only declared ports are in the supported vocabulary; application route semantics are out of scope. Capabilities: partial because SQLite configuration has a source declaration but no confidently assigned map category. Relationships: partial because Compose `links` is explicit but is not equivalent to supported `depends_on`. These limits are retained rather than scored as false absences. |

## Interpretation limits

This is a bounded qualitative oracle, not a numerical precision/recall fixture. The labels name source grounded facts but do not include every normalized output property and identity field required for an exact score; use them for source-by-source qualitative adjudication only. The Java slice is deliberately exhaustive and small. For Flasky, only the eleven hashed oracle files contribute to graph precision/recall; tests, migrations, templates, and other application files are outside the denominator. Coverage statuses are stated with the limitation of each question. The known ambiguities (Procfile, SQLite category, and Compose `links`) are explicit unknowns/partials, not silently counted as supported facts. A reviewer should adjudicate each source fact against the post-freeze map and avoid computing or aggregating precision/recall from these qualitative labels.

## Separate exact-category fixture

`flask-on-docker.json` is a `verify_golden.py`-compatible manifest for the pinned `testdrivenio/flask-on-docker` repository. It is source-first but implementation-aware; an independent labeler should finish before any map is generated. The exact oracle has four files (`docker-compose.yml`, `services/web/Dockerfile`, `services/web/requirements.txt`, and `services/web/project/__init__.py`) and each digest is pinned. The exact precision denominator is three deployables. Interfaces, components, capabilities, and edges are labeled for targeted recall only; their verifier scores must not be presented as exact precision. The separate production Compose/Nginx source facts are also targeted recall only and have hashes under `supporting_files`.

Fetch the source at the frozen commit with:

```sh
git clone --no-checkout https://github.com/testdrivenio/flask-on-docker.git .cache/blind-holdout-v1/flask-on-docker
git -C .cache/blind-holdout-v1/flask-on-docker checkout --detach c51257d182ad9c1e1b5f6addb633f74a358c0f71
```

Use `verify_golden.py --labels tests/map_corpus/blind_holdout_v1/flask-on-docker.json --maps <maps-dir> --no-gate` after the independent labeler and first map run. Read only deployables as an exact precision measurement; the manifest keeps `evaluation_scope` separate from source-based map coverage statuses.
