# Assessment 1.5.0 acceptance discrepancies

No reference implementation is used for this suite's non-MSBuild structural
expectations: they are direct consequences of the literal synthetic fixture
contents and are asserted as API behavior. The optional .NET SDK oracle is
restricted to controlled temporary fixtures, does not run restore/build, and is
not a substitute for the candidate's documented static evidence boundaries.

| ID | Status | Scope | Resolution |
|---|---|---|---|
| DISC-001 | ACCEPTED | NuGet conditions, property expansion, and out-of-tree imports can change effective MSBuild inputs | The candidate reports selected-source static evidence and qualification. The optional SDK check queries only hand-authored fixtures for assignment/import order, anchored import paths, four recognized implicit SDK forms, and explicit/dynamic target-import controls. It performs no restore/build and is intentionally not a general MSBuild evaluator. |
| DISC-002 | CORRECTED FIXTURE | The first shared-input counterexamples used bare `Project` XML but expected SDK imports | Controlled SDK queries showed that bare projects do not automatically import `Directory.Build.props` or `Directory.Build.targets`. Fixtures intended to test implicit inputs now declare the supported SDK; a separate bare-versus-SDK counterexample guards the difference. The expected shared-input behavior was not generalized to bare projects. |
