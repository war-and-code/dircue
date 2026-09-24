# Fresh blind map labels — 1.0.0 final check

These labels were written before the 1.0.0 holdout repositories were unblinded and before any dircue map output for these repositories was read. They serve as a genuinely unseen final check on the accuracy of the 1.0.0 map implementation.

**dircue was not run. No dircue output was inspected.**

## Repository selection

Four public repositories were selected to stress the feature areas being finalized in PR #143 (Maven WAR packaging, Dart path: dependencies) while complementing the existing golden and holdout corpora. All four are absent from:

- `tests/map_corpus/golden_expectations.json` (aws-sam-java-rest, loki, mastodon, ruff, spring-petclinic, superset, terraform-aws-vpc)
- `tests/map_corpus/holdout_labels/` (appflowy-editor, owasp_java, owasp_python, cobol)
- `tests/map_corpus/public_expectations.json`
- `tests/atlas/corpus.json`

| ID | Repository | Commit (pinned) | Focus area |
|---|---|---|---|
| `openmrs-core` | OpenMRS/openmrs-core | `a4d278a8772faf16da132a958135026d330108e2` | Maven multi-module WAR with `finalName`, multi-database, Compose |
| `flask-realworld` | gothinkster/flask-realworld-example-app | `4b95fb2227dfeb5dd1a45d89b2bf48630b93fd28` | Flask app (not library), Procfile, PostgreSQL + JWT |
| `bloc` | felangel/bloc | `b9be1e252ee24ffbe9f2fb629edc7624b2cfbf05` | Dart/Flutter monorepo, path: dependency chain, HTTP client capability |
| `node-realworld` | gothinkster/node-express-realworld-example-app | `30b68e1e881462b2f4164ea09ab4c4f5699c7b0b` | npm/Node.js app, Prisma PostgreSQL, JWT, npm start |

### Why these repositories

**openmrs-core** is a mature, actively maintained Java electronic health record system. It is a Maven multi-module project with a single `<packaging>war</packaging>` module (`webapp/`) that sets `<finalName>${webapp.name}</finalName>` (resolved to `openmrs`). The `api/` module declares runtime JDBC drivers for MySQL, MariaDB, and PostgreSQL, and the Hibernate Search Elasticsearch backend. The `docker-compose.yml` includes a MariaDB service with `depends_on` wiring and port declarations. This exercises: Maven WAR detection with `finalName`, multi-module membership, multiple relational + search capabilities, and Compose edge detection.

**flask-realworld** is a Flask web application (the gothinkster RealWorld backend), not a library. It uses Pipfile and `requirements/prod.txt` (no `setup.py`/`pyproject.toml`), a Procfile for Heroku/gunicorn deployment, SQLAlchemy over PostgreSQL (`psycopg2`), and Flask-JWT-Extended for authentication. This exercises: Python project detection without a formal package manifest, Procfile deployment vocabulary, and Flask-application capability extraction.

**bloc** is the felangel/bloc Dart/Flutter monorepo. The main `packages/` libraries (bloc, flutter_bloc) use pub.dev version references between each other — not `path:` references. The `examples/flutter_weather` example app, however, contains a three-package chain linked by `path:` declarations: `flutter_weather → weather_repository → open_meteo_api`, and `open_meteo_api` declares `http: ^1.0.0`. This exercises: Dart `path:` dependency detection across nested example directories, and HTTP client capability via the Dart http package.

**node-realworld** is the gothinkster Node.js RealWorld backend, modernized to use Prisma ORM with PostgreSQL (replacing the original Mongoose/MongoDB). It is an Nx-managed single-component npm application with `scripts.start: "nx serve"`, `@prisma/client`, `jsonwebtoken`, `express-jwt`, and `axios`. The Dockerfile builds and runs the application. This exercises: npm start interface detection, Prisma-schema-declared PostgreSQL capability, JWT and HTTP client capability from npm dependencies.

## Label methodology

Each label file follows the golden corpus schema (`verify_golden.score_repo`-compatible). For every repository, 3–5 oracle files were selected from: the primary project manifest, a container/compose definition, a configuration file declaring datastores, and an entry-point source file.

Labels are exhaustive within the oracle files: every component, deployable, interface, capability, and edge that the selected files statically establish is included. Precision is measurable because no label was added from files outside the oracle set.

Every label has a `why` field citing a file and line number. Where MAP.md vocabulary is ambiguous (e.g. Procfile `web` process, Dockerfile `ENV PORT` vs `EXPOSE`), notes are included.

No repository was cloned after reading any dircue output. Repos were cloned with `git clone --depth=1` and the commit SHA was recorded with `git rev-parse HEAD`.

## Label statistics

| Repository | Components | Deployables | Interfaces | Capabilities | Edges |
|---|---|---|---|---|---|
| openmrs-core | 3 | 3 | 2 | 3 | 8 |
| flask-realworld | 1 | 1 | 0 | 2 | 2 |
| bloc | 5 | 0 | 0 | 1 | 3 |
| node-realworld | 1 | 1 | 1 | 3 | 4 |

## Vocabulary notes

- **Procfile `web` process**: No explicit Procfile deployable kind exists in MAP.md. The Procfile `web: gunicorn ...` entry is labeled as `kind: service` (the closest Compose-service analog). dircue may not recognize Procfiles; if so, this is a coverage gap to note.
- **Maven sibling deps as `depends_on_local`**: Maven multi-module projects resolve intra-project dependencies via the build reactor, not via explicit `path:` syntax. `depends_on_local` is used for the `openmrs-webapp → openmrs-api` dependency since both are members of the same multi-module build and the dependency is declared in the webapp POM. This vocabulary choice may need alignment with how dircue labels Maven intra-project deps.
- **Dockerfile `ENV PORT` vs `EXPOSE`**: MAP.md requires `EXPOSE` for a declared_port. The node-realworld Dockerfile uses `ENV PORT=3000` without `EXPOSE`; no declared_port interface is labeled for it.
- **MariaDB as `datastore:mysql`**: openmrs-core's `api/pom.xml` declares both `mysql-connector-j` and `mariadb-java-client`; both are labeled under `datastore:mysql` because MariaDB uses the MySQL wire protocol. There is no `datastore:mariadb` catalog entry.
- **`@prisma/client` → PostgreSQL**: The npm package `@prisma/client` does not uniquely identify a database; the database is declared in `src/prisma/schema.prisma` (`provider = "postgresql"`). The capability label cites the schema file, not the npm manifest.

## Corpus provenance

Labels were written by an independent labeling agent from source files only. dircue was not built, run, or invoked in any form. No dircue map output (JSON or summary) was read at any point. Repository clones were made into the session scratchpad at `$S/pr143/fresh/repos/` and were not shared with any other agent or scoring process before this commit.
