# Saved-report follow-up planning

Added in 0.8.0.

```sh
dircue capabilities --json
dircue plan first-pass.json --module declarations --json
dircue plan first-pass.json --question content-formats --json
dircue plan first-pass.json --module focus --project services/api/api.csproj --json
dircue plan first-pass.json --module structure --input structural-worker --json
```

`plan` reads one bounded, schema-validated aggregate profile and creates an inspectable follow-up plan. It does not rescan the declared root, read evidence paths, inspect `PATH`, probe a structural worker, contact a service, or run any planned command. The caller selects questions or modules; repository text cannot add requests or change planner policy.

The initial questions are `content-inventory`, `project-declarations`, `environments`, `project-scope`, `source-availability`, `content-formats`, `code-metrics`, and `source-structure`. `dircue capabilities --json` returns the versioned registry used by the planner. This descriptor covers planner-supported modules only, not every dircue command or flag.

For the complete CLI catalog, use `dircue capabilities --cli --json`. The separate `--guide` view explains workflows in text or JSON, and `dircue capabilities --schema planning` exports the plan's JSON Schema for offline validation. These sibling views are listed by `dircue capabilities --help`; the default planner descriptor remains byte-compatible with 0.8.0. Use `dircue plan --help` for currently supported questions, modules, and examples without consulting this document.

## Reading a plan

The plan binds its decisions to the SHA-256 of the exact saved-report bytes, the aggregate schema version, and the capability provider and version. The profile's `root` is retained as declared evidence. It is never inserted into a command.

Plain-text plans also show each step's inert JSON argument array, unresolved inputs, inspection class, evidence quantities, and required revalidation. These are argument arrays, not shell commands to paste and execute. The JSON plan retains its existing contract.

Every planned command is structured `argv`, contains a literal `{source}` placeholder, and has `executable: false`. A consumer must revalidate the source identity, source boundary, and report freshness before replacing the placeholder. When retained modules agree on a Git tree or directory source, the template preserves that mode and the selected Git tree. Conflicting module provenance blocks the step. Missing provenance stays unavailable rather than silently becoming the current directory or `HEAD`.

Decisions distinguish these states:

- `proposed` has supporting retained evidence under the fixed decision table.
- `unknown` has no matching retained signal. It is not a claim that follow-up work is safe to skip.
- `blocked` is missing a required caller input, supported project scope, or consistent source identity.
- `already_present` applies only to complete retained evidence with compatible scope. A focus report is reused only for the exact requested primary project.
- `retained_partial` preserves incomplete evidence or a different retained scope and keeps the possible follow-up visible.

Focus planning accepts one primary `--project` in this initial release. The selector must be a confined root-relative path and must identify a supported parsed project in retained declarations. Structural planning requires the caller to state `--input structural-worker`; this records prerequisite availability without probing or trusting a path from the saved report. Project selectors are rejected unless focus is among the selected modules or questions. Caller inputs are likewise accepted only when a selected capability declares them; unknown names, misspellings, and inputs for another module fail instead of being silently ignored.

`already_present` means the requested complete module and compatible scope are retained in the supplied report. When that report lacks a consistent selected source identity, the decision explicitly qualifies reuse as retained evidence; it is not a provenance-complete claim about a checkout.

## Evidence and cost

Observation IDs are stable derivations of retained discovery or language evidence. Candidate paths are capped independently from candidate counts. This keeps a small manifest visible beside large data and prevents frequent artifact kinds from displacing a project candidate. Partial discovery, omitted candidates, empty language totals, and unsupported languages remain explicit uncertainties.

Costs report retained candidate file and byte quantities, the inspection class (`metadata`, `bounded-content`, `full-content`, or external worker), and whether an external process is required. These quantities do not predict runtime or memory, and they do not hide shared traversal or setup work. A first pass may cost more than an unconditional run on a small input; the planner makes no savings claim. For focus planning, the candidate quantity is the selected project manifest retained by discovery. It is not a count of the project's owned files or a prediction of the eventual focused population.

Planning is bounded to 64 selected questions/modules, one focus project, 16 supporting observations per step, 256 observations overall, 256-byte request keys, and 1 MiB of serialized output. Exceeded input or arithmetic bounds fail instead of wrapping counts or emitting an unbounded plan.

Git-backed plans use `--tree` with the exact retained tree object ID, followed by `--` before the source placeholder. `--rev` remains a commit selector; `REV:path` is rejected. Revalidate the source boundary and replace placeholders as distinct argument values, never through shell interpolation.
