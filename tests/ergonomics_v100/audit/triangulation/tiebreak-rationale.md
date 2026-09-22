# Independent tiebreak applicability notes

Baseline 121027f44f014ac8d0a003a451a04b080a5ffe88. No prior raw scores were read. Evidence comes from baseline git-show source/tests and released-binary probes. No production edits, builds or heavy tests.

RN-TB-001: Add an output-schema class to the rubric. Static output metadata has no first invocation or misspelling interface, so intuitiveness and intent inference are n/a-as-perfect. Schema discovery and component/envelope scope still matter to self-documentation and parseability. Do not conflate these contracts with configuration schemas accepting user input.

RN-TB-002: Add help/group class guidance. Help prose does not promise report JSON; data-emitting child commands are scored separately. Missing JSON command indexes are not automatically product defects.

RN-TB-003: Read-side analysis using an explicitly selected trusted executable is not an irreversible mutation merely because arbitrary caller code can do arbitrary things. The read-side safety rule applies without inventing delete/rollback/lease controls. This is not a sandbox claim.

RN-TB-004: Plain stderr with exit 1 and empty stdout earns partial error parseability (500). It provides failure detection but no typed diagnostic or categorical status. This is not a recommendation to add error JSON.

RN-TB-005: The rubric phrase “Pass 1 (no tests yet)” is conditional on absent tests. This mature project has meaningful CLI/schema assertions. Not executing them during a profiling window does not mean they do not exist. Full goldens, help snapshots and version-gated drift coverage were not inferred merely from positive schema validation.
