# Independent source labels: flask-on-docker

`flask-on-docker.json` is an independent, source-first oracle for upstream
`testdrivenio/flask-on-docker` at commit
`c51257d182ad9c1e1b5f6addb633f74a358c0f71`.

The oracle was built from the public `docs/MAP.md` vocabulary and the raw files
listed in `oracle_files`. Each record's `why` names source-relative paths and
line numbers; `oracle_files` binds those facts to SHA256 hashes of the exact
upstream bytes. The extra `manage.py`, `project/config.py`, and `entrypoint.sh`
files support the bounded project, database, and launch observations.

The records include all supported facts established from those oracle files
that could be labeled without inspecting dircue output. The oracle does not
cover the full upstream tree. In particular, other Compose and Docker
definitions are outside its file set. For that reason, each category has
`evaluation_scope: targeted_recall_only` and partial coverage: these labels can
support targeted recall, but they do not establish a complete expected set for
precision scoring. Absence from this file is not a negative label.

The PostgreSQL capability means the source supports a PostgreSQL configuration:
Compose declares a PostgreSQL service, the Python dependencies include its
driver, and the entrypoint has a conditional readiness check. The app config
defaults to `sqlite://`, so the oracle does not claim that the checked-in
default selects PostgreSQL. The port label uses the container-side `5000` from
the Compose mapping `5001:5000`.

This oracle was committed before any dircue execution. It does not include
generated map output, implementation-derived conclusions, or labels copied
from another corpus entry.
