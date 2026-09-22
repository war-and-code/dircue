# Design principles

These principles guide changes to dircue. The [capability guide](CAPABILITIES.md)
describes what is implemented today; proposed work belongs in
[GitHub Issues](https://github.com/war-and-code/dircue/issues).

## Be useful in an unfamiliar directory

Profile source code, configuration, data, artifacts, and other computer content.
Git metadata can add context, but ordinary directory profiling must remain
useful without it. Produce observations that people and other tools can use
for many purposes, without assuming one particular downstream workflow.

## Keep the first useful answer inexpensive

Preserve the fast language-only path. Let callers start with lightweight
observations and explicitly request deeper analysis when it is useful.
Installing another executable or dependency must not silently enable a new
analysis stage. A module's value should justify its startup, I/O, CPU, memory,
and distribution costs.

## Combine simple defaults with deliberate control

Offer useful defaults and consistent controls for advanced use. Separate
three decisions:

- **Analysis scope:** which sources and modules to inspect.
- **Execution policy:** how to schedule and allocate resources for that work.
- **Evidence presentation:** which details to retain and how to display them.

An execution preference should preserve the requested answer while trading
time for resources. Skipping files changes coverage and must be reported as
such. Distinguish cooperative memory targets from externally enforced limits,
and account for optional child processes when describing their scope.
If presets are added, expose their effective settings and define how explicit
options override them. More flags are useful only when they express meaningful
choices.

## State what the evidence supports

Keep filename hints, parsed declarations, measured properties, and inferred
relationships distinguishable. A manifest does not prove a successful build;
a complexity metric does not establish overall software quality. Attach
source locations, provenance, definitions, and limits where they help a
consumer assess an observation. Treat repository documentation as input to
evaluate rather than authoritative ground truth.

## Make incomplete knowledge visible

Distinguish absent, unsupported, unavailable, uninspected, and invalid inputs.
Report each module's coverage and relevant omission reasons. An empty language
result does not establish an empty directory. Bounded evidence must not be
presented as the complete population, and a partial result must not masquerade
as a complete one. Preserve these distinctions when comparing saved reports.

## Respect input boundaries and caller intent

Use the selected Git tree or directory consistently. Profiling must not run
the inspected repository's build scripts or commands by default. External
execution or network integrations require explicit selection and a documented
scope. Repository-owned hints must not silently disable analysis required by
trusted caller policy. Keep ordinary local profiling usable offline and
without telemetry.

## Preserve contracts and measure tradeoffs

Protect supported CLI invocations, JSON layouts, exit semantics, and the
documented Linguist compatibility contract. The
[proposed 1.0 compatibility policy](COMPATIBILITY.md) names the specific surfaces
frozen for the 1.x line and how additive changes and deprecations are
versioned. Keep diagnostics separate from machine-readable stdout. Test
changes against independent references and representative inputs, including
awkward and large cases.

Measure performance rather than inferring it from implementation choices.
Record the workload, source versions, environment, and both benefits and
costs. Lower allocation does not necessarily mean lower peak RAM; a faster
fixture does not establish a universal speedup. A proof establishes its stated
property under its assumptions, not the correctness of an entire executable.

## Make capabilities discoverable and maintainable

Provide clear help, examples, versioned schemas, and machine-readable
capability information that serves both people and automated callers.
An installed binary should explain its workflows, output contracts, limits,
and recovery paths offline. Repository documentation can provide more detail,
but ordinary use should not depend on visiting it. Keep the built-in reference
aligned with actual commands and test its examples and contracts.
Disclose optional dependencies and effective settings. Reuse upstream work
where it fits, preserve its licenses and attribution, and keep updates
reproducible. Support from an upstream library becomes a dircue capability
only after its integration and limits have been verified.
