# Focused topology mutations

`topology.py` deliberately introduces attribution faults into production source through Go's `-overlay` option. It leaves the checkout unchanged. The operator catalog covers selected Procfile tokenization and ownership rules, Gradle membership resolution and qualification, and Aspire builder trust, project-wide aliases and malformed-input handling.

```sh
python3 tests/mutation/topology.py --output .cache/mutations/topology.json
python3 -m unittest discover -s tests/mutation -p 'test_*.py'
```

The harness first runs the focused baseline suite. Every mutant must then compile and fail its named regression test at an assertion. Surviving mutations, compilation failures, timeouts and infrastructure errors are distinct outcomes; all make the gate fail. A recovered parser panic deliberately checked by a test is an assertion failure. An unrelated unrecovered process panic is not counted as a kill.

A receipt records source hashes, exact edits, commands and outcomes. Existing receipt paths cannot be overwritten. Full Linux CI runs this gate and uploads the receipt. The fourteen operators test specific known risks; killing them does not establish a project-wide mutation score or complete defect coverage.

The separate `Mutation Testing` workflow runs pinned Gremlins on explicitly selected packages. It is manual and reports mutation statistics without an efficacy threshold. Command failures and missing receipts fail the workflow. It does not run on a schedule.
