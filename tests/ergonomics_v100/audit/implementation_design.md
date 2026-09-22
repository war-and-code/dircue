# Dircue 1.0 ergonomics implementation design

Reviewable proposal, not an implementation or completed audit. Baseline: released
0.8.0 at `121027f44f014ac8d0a003a451a04b080a5ffe88`. Runtime observations used the
released binary already installed at `.cache/release080/installed/dircue`, against
the existing tiny Cargo fixture. This formal audit design was copied from the reviewed ignored scratch proposal after bootstrap. It records intended changes and requests no release, branch, commit, or publication.

## Constraints and measured baseline

Preserve exact successful legacy JSON bytes, selected sources, optional module
activation, and handled-error exit status 1. Preserve bare invocation: it remains
language analysis of the current directory. Existing `capabilities --json` retains
its planner-only 1.0.0 descriptor, kind, schema, module list, and serialized body.
The additional views below are explicit flags on that existing command. There
are no new root command names, no aliases that execute guesses, no automatic
module activation, and no scans, host-tool probes, network, or telemetry in the
new introspection views.

The baseline already supplies: 18 analyzers, meaningful help/examples on 17 of
those leaves, JSON on every data-producing command, versioned aggregate schemas,
explicit provenance and omissions, `analyze all` for selected modules in one
scan, and inert planning with structured argv. Repeated legacy, aggregate,
discovery, and capability output was byte-identical on the fixture. Non-TTY
invocations with NO_COLOR, CI, TERM=dumb, and SOURCE_DATE_EPOCH stayed clean and
noninteractive. These are strengths to preserve, not features to duplicate.

Observed gaps: unknown flags have no suggestions; `analyze declaration` prints
the generic 18-mode chooser; missing plan/compare arguments report only counts;
saved-report help advertises inherited scan flags that it rejects; root and
planner help lack workflow examples; plain plans omit their actionable steps;
schema files cannot currently be obtained from the executable.

## Ten coherent recommendations

The numbering is a proposal for implementation tracking, not a claim of applied
work or a numerical quota. Merge items if their implementation is inseparable.

| ID | Change and user outcome | Primary files | Objective regression evidence |
| --- | --- | --- | --- |
| R-001 | Root/analyze/explain help teaches the first scan, source policy, legacy versus aggregate output, and introspection route | internal/cli/cli.go, extended_help.go, explain.go | Root and group help contain executable workflow examples, 0/1 contract, partial-result caveat; explain has fresh and saved examples |
| R-002 | Unknown long flags receive bounded, command-local spelling hints; no guessed action runs | internal/cli/diagnostics.go (new), cli.go | `--jsno`, `--jason`, `--structural-wroker` fail 1 with empty stdout and correct flag hint; distant/ambiguous names receive no arbitrary suggestion |
| R-003 | Analyzer/name confusion produces safe canonical guidance, preserving path semantics | diagnostics.go, cli.go | `analyze declaration` and missing bare `languages` give actionable forms; real/explicit paths with those names still scan identically |
| R-004 | Saved-report argument/help surfaces expose applicable options and exact input requirements | planning.go, compare.go, help helper if needed | Missing report args give synopsis/example; plan/compare/capabilities help no longer advertises rejected analysis flags; rejected flags still fail |
| R-005 | Planner selection and optional-worker errors identify the exact selector/prerequisite | planning.go, diagnostics.go, cli.go | Bad module/question/input/scope gives precise bounded explanation; library sentinel equality unchanged; missing worker names `--structural-worker` |
| R-006 | Opt-in CLI capability view exposes the actual Cobra grammar and stable process/source/output contracts | internal/cli/capability_cli.go (new), planning.go; dedicated schema | Commands and flags match runtime help registrations, are sorted, and disclose restrictions; old default descriptor byte-identical |
| R-007 | Opt-in offline schema export supplies a standard self-contained compound schema with correct resource identities | schema/embed.go (new), embed tests, planning.go | Exported/source schemas agree on valid and deliberately invalid reports, including nested external-resource refs; compiler network loader never called |
| R-008 | Opt-in in-tool guide presents safe staged workflows and interpretation rules in text or versioned JSON | internal/cli/capability_guide.go (new), planning.go | Guide distinguishes committed Git/current directory, legacy/aggregate, partial status, inert templates, and explicit optional worker; same static guide feeds both formats |
| R-009 | Plain plans disclose bounded inert steps, unresolved prerequisites, cost class, and revalidation | planning.go | Text contains argv JSON arrays and non-executable label, preserves withholding of untrusted root, propagates writer failure; JSON byte-identical |
| R-010 | Pin and document the combined automation contract, including existing schema edge cases and compatibility corpus | cli/schema tests, README.md, docs/PLANNING.md, focused audit regressions once authorized | Genuine baseline fails each new behavior check; candidate passes; baseline/candidate unchanged outputs match on legacy/path/source/optional-module fixtures |

R-010 is contract hardening with existing-output defects addressed, not a second
copy of tests for the other nine changes. If no substantive schema discrepancy
remains after R-007, describe it as cross-cutting validation rather than inflating
the substantive-change count.

## R-001/R-004: help and command registration

Set root Example and extend Long with a compact automation paragraph:

```
  dircue --json /checkout
  dircue analyze discovery --source directory --json /content
  dircue analyze all --declarations --json /checkout
  dircue capabilities --cli --json
  dircue capabilities --guide
```

State that exit 0 means the command succeeded, not necessarily complete module
coverage; warnings are stderr; handled errors are exit 1 with empty data stdout
where no data was emitted. Empty language statistics do not establish an empty
directory. Do not promise atomic stdout in the presence of a writer failure.

The analyzer group should point to metadata discovery, legacy languages, and
explicitly selected aggregate profilers without changing its missing-mode
failure to an implicit scan. Add fresh/saved explain examples. Existing analyzer
leaf descriptions already explain boundaries well; avoid blanket rewrites.

For plan help, derive valid module/question strings from the existing capability
registry at help invocation; do not maintain another list. Examples:

```
  dircue analyze discovery --json /checkout > first-pass.json
  dircue plan first-pass.json --module declarations --json
  dircue plan first-pass.json --question content-formats --json
  dircue plan first-pass.json --module structure --input structural-worker --json
```

For saved-report help, prefer a help rendering view which filters inherited scan
flags rather than marking shared pflag.Flag objects hidden (which could affect
other commands in this Execute instance). Keep JSON and help visible. The
rejection policy belongs to a shared `analysisFlagNames` list used by current
validation and the CLI catalog so docs and behavior cannot drift. Source-reading
`analyze explain` is conditional: list its scan flags with an explicit
`--report` restriction, not globally hide them. Preserve flags accepted before
or after positional paths and command tokens.

Suggested Args errors, generated with fixed canonical syntax and no untrusted
path reflection:

```
plan requires one saved aggregate report; use: dircue plan report.json --module declarations --json
compare requires two saved aggregate reports; use: dircue compare base.json head.json --json
```

Read errors retain privacy-safe wording and teach the input shape:

```
cannot open saved report; supply a readable regular aggregate JSON report, for example one saved by: dircue analyze discovery --json /checkout
```

Retain role labels for comparison base/head. Do not append filesystem internals,
report bytes, raw validation snippets, or the report's declared root. Use fixed
examples and explain that `/checkout` is a placeholder; never execute them.

## R-002/R-003/R-005: error helpers and strict inference boundary

Proposed helpers, owned by internal/cli:

```go
func flagErrorWithHint(cmd *cobra.Command, err error) error
func nearestUniqueName(input string, candidates []string) (string, bool)
func analysisSelectionError(cmd *cobra.Command, args []string) error
func missingPathCommandHint(cmd *cobra.Command, args []string, err error) error
func diagnosticValue(value string) string
func validatePlanningSelectionForCLI(selection planning.Selection, caps capabilities.Descriptor) error
```

Register `root.SetFlagErrorFunc(flagErrorWithHint)`. Obtain candidates from the
effective actual pflag sets for the resolved command. The hint must refer only
to options accepted in that context; saved-report rejected inherited flags are
not valid suggestion targets. Adjacent transposition counts as one typo;
bounded single insertion/deletion/substitution also counts as one. This handles
`jsno` and `jason` without a broad fuzzy threshold. A unique nearest candidate
is required; ties produce no guessed spelling. Work is bounded to short flag
names and runs only on failure. Never rewrite args, rerun a command, or emit
success for a typo.

Suggested exact form:

```
unknown flag: --jsno; did you mean --json? See: dircue analyze discovery --help
unknown analysis "declaration"; did you mean: dircue analyze declarations --json /path/to/source
choose an analysis; for a metadata inventory use: dircue analyze discovery --json /path/to/source; for language statistics use: dircue analyze languages --json /path/to/source
```

Use the actual command path for the help reference. Preserve the original
parser error through wrapping when appropriate. Bound/escape reflection before
constructing the complete user-facing message; `%q` alone is not a size bound.
`diagnosticValue` should truncate to at most 256 valid UTF-8 bytes/runes under a
documented policy, then use `terminalValue` and quote the result. Invalid UTF-8,
ESC, LF, CR, U+202E, and other Unicode Cf characters must never reach terminals
literally. The original pflag error may itself contain attacker-controlled text:
sanitize its rendered message too rather than appending a safe hint to unsafe
prose. Prefer a tiny wrapper retaining Unwrap() if preserving the original error
identity is necessary.

Root command-like path hints are appended only after the current scan returns
an error identifiable as missing path (`errors.Is(err, os.ErrNotExist)`). Further
conditions: one bare relative token; no slash/backslash, leading dash, controls,
or `--` separator; candidate is an exact analyzer name or a unique near root
command. Existing paths, symlinks, denied paths, `./languages`, absolute paths,
`-- languages`, and malformed/unsafe tokens never enter this hint path. The
original failure remains a failure. It is also acceptable to defer this rare
root-level hint entirely rather than weaken path semantics.

CLI planner validation names unsupported `--module`, `--question`, `--input`,
and invalid `--project` combinations before report I/O. Valid vocabularies come
from the existing descriptor. Examples:

```
invalid planning input: unknown --module "declaration"; did you mean --module declarations? List supported modules with: dircue capabilities --json
invalid planning input: --project requires --module focus or --question project-scope
invalid planning input: --input structural-worker applies only when structure is selected
planning limit exceeded: --project accepts one primary project
structure requires --structural-worker /path/to/dircue-structural-worker; select a trusted matching worker explicitly
```

Leave `pkg/planning` validation and exact exported sentinel values unchanged.
CLI errors may wrap those sentinels; deliberately update CLI-only tests to
`errors.Is`, retain library equality tests. Existing lower-level bound/path
checks remain authoritative and cannot be weakened by helpful prevalidation.
Do not claim or test that a supplied worker is trusted by merely existing.

## R-006: machine-readable CLI contract

New selectors on capabilities are mutually exclusive by flag presence:
`--cli`, `--schema NAME`, `--guide`. An explicitly empty schema name fails with
available names. Explicit false flags should have documented treatment; using
Changed-based exclusivity is least surprising for incompatible requests.
`--json` selects JSON for CLI/guide views. Schema view always emits JSON because
its product is a JSON Schema; `--json` is accepted and documented as redundant.
No selector preserves the old code path and exact old JSON.

Suggested CLI view shape, kept separate from pkg/capabilities.Descriptor:

```go
type CLIContract struct {
    SchemaVersion string // 1.0.0, separate from planner descriptor
    Kind string          // dircue-cli-capabilities
    ProviderVersion string
    Commands []CommandContract
    ExitCodes []ExitContract
    SourceSelection SourceContract
    Environment []EnvironmentContract
    OutputContracts []OutputContract
    SchemaResources []SchemaResource
}
type CommandContract struct {
    Path []string        // e.g. ["dircue", "analyze", "discovery"]
    Usage string
    Summary string
    Examples []string
    PositionalArguments []ArgumentContract
    Flags []FlagContract
    Restrictions []RestrictionContract
    OutputVariants []string
}
type FlagContract struct {
    Name string
    Shorthand string
    Type string
    Default string       // DefValue, never the current mutated flag value
    Inherited bool
    Required bool
    AllowedValues []string // only actual enum constraints
}
```

JSON tags should be conventional snake_case; use empty arrays instead of null.
Do not include arbitrary parser internals. Enumerate command paths, usage,
short/long descriptions, examples, and pflag type/default/shorthand from the
actual fully constructed Cobra tree when --cli runs. Initialize standard help
registration before enumeration so catalog/help alignment is testable. Never
clone the CLI grammar in a static second command list.

Cobra Args closures do not introspect cardinality. A small command annotation
at its registration site can provide positional roles/cardinality, output
variant IDs, and restrictions that the framework cannot reveal. Flag enums and
conditional relationships likewise need explicit metadata co-located with
validation. Do not falsely infer an enum by parsing prose or infer a full
contract from command names. Catalog rejected inherited flags as restrictions
or omit them from accepted flags consistently with help; JSON remains accepted.

Exit contract is exactly 0=successful command, 1=handled error. Distinguish
successful partial reports, legacy tree-limit empty results plus warning, and
process termination behavior; do not invent new exit categories. No color or
interactive prompt options exist because rendering is unstyled/noninteractive.
No tool-specific environment configuration exists. GOMAXPROCS affects automatic
workers; SystemRoot affects Windows worker startup only. NO_COLOR/CI/TERM do not
alter behavior because no styling/prompts exist. SOURCE_DATE_EPOCH is unnecessary
for outputs with no clock fields. Do not claim env variables are parsed if they
are merely irrelevant under these invariant behaviors.

Output catalog must distinguish: legacy directory language map; standalone
single-file JSON; findings arrays; aggregate profile envelopes; comparison;
planner capabilities; plan; new CLI/guide views. Several existing schemas
describe a module *inside* an aggregate, not the whole standalone command's
envelope: store the JSON Pointer and envelope relationship explicitly. Mark
single-file JSON schema unavailable unless a separately reviewed exact schema
is added; never link it to the directory-language schema. Profile schema
represents multiple versions by selected modules, not simply "latest only".

## R-007: exported JSON Schema resources

Add a small `schema` Go package that embeds existing `*.schema.json` bytes. It
must not import CLI/scanner or compile schemas at initialization. Existing
external schema_test tests may continue to import CLI without an import cycle.
Suggested API:

```go
func Names() []string
func Export(name string) ([]byte, error)
```

Names are stable logical names from embedded file names (profile, capabilities,
planning, comparison, languages, findings, and existing module resources). A
name is an allowlisted logical identifier, never a filename/path opened from
the filesystem. Name resolution happens before parsing. Return fresh bytes or
immutable cached bytes; no mutable shared map escapes. Sort deterministic
resource order. Do not hydrate, fetch, compile, or decode schemas at startup.

For standard compound output, copy the selected resource as the root and give
it a deterministic absolute resource ID such as
`https://dircue.invalid/schema/profile.schema.json` (identifier only, never a
network address to fetch). Recursively collect external relative $ref targets
within the embedded registry. Give every embedded dependency its matching
absolute $id and place it under a reserved root $defs key. Leave that resource's
internal `#/$defs/...` references intact; its own $id establishes the right
resolution scope. Existing relative `formats.schema.json` references then
resolve to the dependency's embedded absolute ID. Do not flatten definitions
or rewrite every local fragment to the outer root.

Check the reserved namespace for collisions; preserve all original root $defs,
annotations, and constraints. Reject unknown/nonlocal referenced resources
rather than attempting I/O. If future source schemas acquire $id, explicitly
verify compatibility or translate identities consistently; silently replacing
an existing base URI is unsafe. The implementation is accepted only after the
project's validator compiles the compound with a denied network loader and
representative outputs validate equivalently to the source schema graph. A
nonstandard resource-list envelope is a fallback only if named/documented as a
bundle, not falsely presented as an ordinary directly usable JSON Schema.

Observed existing discrepancy to fix narrowly: languages.schema.json currently
requires a decimal percentage and rejects the documented Linguist string
`"NaN"` for attributed empty files. Extend that schema to allow exactly this
known string alongside its existing decimal pattern, and validate an actual
legacy CLI output fixture. Do not change the legacy output to satisfy schema.

## R-008: one guide model, two renderings

Keep guide content in a static structured value built only when selected:
version/kind, short workflow sections, example argv arrays, caveats, schema
discovery reference. Text renders the same content. Include source selection,
lightweight discovery then caller-selected modules, legacy versus aggregate
JSON, schema/version/status/omission checks, valid saved-report examples,
comparison meaning, explicit structural worker trust, and plan revalidation.
No commands are run, no root content is inserted, and no recommendation
silently enables expensive analysis. All JSON writers return errors.

## R-009: plain plan rendering

Preserve the current identity/decision header, then render each bounded step:

```
Step: metrics (unknown)
  Inspection: full-content; external process: false
  Candidate evidence: 0 files, 0 bytes (not a runtime or memory estimate)
  Unresolved inputs: source
  Inert argv (not executable): ["dircue","analyze","metrics","--json","--","{source}"]
  Revalidation required: source identity, source boundary, report freshness
```

Use the actual plan fields, not these illustrative values. JSON-marshal argv
arrays rather than shell-joining tokens; escape Unicode terminal format/control
characters after JSON rendering too (JSON escapes do not cover every Cf code
point). Text never renders the report's declared root as a runnable path. Steps
are bounded by planning limits, so no extra unbounded list is introduced. Keep
all status/reasons as retained evidence, and display actual unresolved inputs
and cost qualifications. Return every Fprintf/Fprintln/Encode writer error.
The JSON path and planning.Build output stay byte-for-byte unchanged.

## Initialization and cost control

The ordinary scan path should acquire only small command registrations and
fixed help strings. No schema JSON parse, guide model construction, command
catalog traversal, source/schema compilation, new subprocess, or environment
inspection runs on successful default scans. Avoid a global init that parses
embedded data or a package-level precomputed descriptor. Error suggestions run
only after failure. If descriptor construction is measured significant, use
RunE-local construction rather than speculative caching across Execute calls.
Embedded schema bytes necessarily add binary/rodata size; measure that honestly
and avoid claiming zero overhead. Parent owns controlled performance checks.

## Regression matrix and ownership

Production ownership must be granted serially by root before edits. Suggested
sequence: root reviews design; one implementation owner changes CLI/help/error
files; one later owner adds schema export; root integrates introspection and
re-runs compatibility/performance gates. Shared-file edits never overlap.

Required meaningful tests:

1. Preserve baseline/candidate bytes for `--json`, `--breakdown --json`, language
   analyzer, empty and attributed-empty trees, file mode, aggregate defaults,
   selected modules, old capabilities JSON, and plan JSON.
2. Existing exact paths named analyze/help/compare/plan/capabilities/languages,
   near names analyse/declaration, dash-leading paths and spaces work with `./`,
   absolute paths, and `--`. Bare missing-name hints do not alter existing-path,
   inaccessible-path, symlink, or explicit-path errors. Options on either side
   of paths remain accepted.
3. Invalid flags/modes never scan, spawn a worker, create files, or write data;
   exit remains 1. Test typo distance, transposition, ambiguity, missing value,
   `--flag=value`, shorts, excessively long names, invalid UTF-8 and controls.
4. Planner invalid selector/scope errors preserve errors.Is in CLI, exact
   sentinels in pkg/planning. Module/question lists come from the registry;
   project comma handling and existing source revalidation tests still pass.
5. Capture help for every command; catalog commands and applicable flags match.
   Run --cli with a changed flag value and ensure catalog defaults still reflect
   DefValue. Reject conflicting selector flags, empty/unknown schema names and
   scan-only flags consistently; JSON stderr remains empty on success.
6. For every exported schema, compile offline and compare acceptance/rejection
   with its original file graph. Exercise profile's external module resources
   and their local definitions with populated focus, availability, formats,
   declarations, environments and hotspots examples; empty-only tests are
   insufficient. Check NaN legacy output; reject a misspelled/extra schema
   property, wrong type, missing required field and invalid nested enum.
7. New views are byte-stable across repeat runs and flag ordering; no clock,
   host path or username leaks. Guide text and JSON retain the same examples.
8. Saved-report errors never echo malformed report snippets or untrusted root.
   Plan text escapes ESC/LF/CR/Cf input and prints an inert JSON argv list, not a
   shell command. Every new output writer propagates a short/erroring writer.
9. Context cancellation is checked before nontrivial introspection/schema work
   where appropriate; no new prompts in non-TTY and no ANSI under baseline,
   NO_COLOR, CI or TERM=dumb.
10. Per-recommendation audit regressions run against genuine released baseline
    and candidate; expected new behavior must fail on baseline. Existing
    strengths do not get fictitious uplift. Genuine remaining gaps are deferred
    explicitly, not hidden by synthetic scores or new feature quotas.

Implementation checkpoint: authorized CLI and schema changes now pass go test ./internal/cli ./schema. This document retains the proposal details; application status and final evidence are recorded separately.
