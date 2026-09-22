# Pass 1 recommendation playbook

These are ranked proposals, not claimed fixes. Priorities use explicit estimates; uplift values are hypotheses for re-scoring, not measured results. Preserve the strong baseline contracts and avoid adding another mega-command: analyze all and inert plan already serve that purpose.

## R-002: Suggest unique nearby flags without executing guesses

Attach bounded command-local edit/transposition hints in SetFlagErrorFunc; escape reflected names; preserve exit1 and empty stdout.

Files: internal/cli/diagnostics.go, internal/cli/cli.go. Regression: Focused CLI/schema tests plus audit/regression_tests/R-002__contract.test.py; new-behavior checks must fail on released baseline and pass on candidate.

## R-005: Identify invalid planner selections and missing worker prerequisite

Validate CLI selector vocabulary from existing capability registry; wrap library sentinels only in CLI; name --structural-worker explicitly.

Files: internal/cli/planning.go, internal/cli/diagnostics.go, internal/cli/cli.go. Regression: Focused CLI/schema tests plus audit/regression_tests/R-005__contract.test.py; new-behavior checks must fail on released baseline and pass on candidate.

## R-004: Make saved-report arguments and help actionable

Render scoped help without mutating shared flags; replace count-only args errors and privacy-safe read errors with input requirements/examples.

Files: internal/cli/planning.go, internal/cli/compare.go, internal/cli/help.go. Regression: Focused CLI/schema tests plus audit/regression_tests/R-004__contract.test.py; new-behavior checks must fail on released baseline and pass on candidate.

## R-001: Teach automation workflows through help

Add root/analyze/explain examples and accurate source/output/exit/partial semantics; cross-link explicit capabilities views.

Files: internal/cli/cli.go, internal/cli/explain.go. Regression: Focused CLI/schema tests plus audit/regression_tests/R-001__contract.test.py; new-behavior checks must fail on released baseline and pass on candidate.

## R-006: Expose actual CLI grammar through opt-in capabilities view

Add --cli view derived from live Cobra commands and flags, with source/output/exit/env contracts and explicit restrictions. Preserve old planner descriptor.

Files: internal/cli/capability_cli.go, internal/cli/planning.go. Regression: Focused CLI/schema tests plus audit/regression_tests/R-006__contract.test.py; new-behavior checks must fail on released baseline and pass on candidate.

## R-003: Explain analyzer and bare command-name mistakes safely

Use exact analysis mode guidance and optional missing-bare-path hints only after failure; existing explicit paths remain untouched.

Files: internal/cli/diagnostics.go, internal/cli/cli.go. Regression: Focused CLI/schema tests plus audit/regression_tests/R-003__contract.test.py; new-behavior checks must fail on released baseline and pass on candidate.

## R-007: Export embedded schemas with offline resource resolution

Add --schema NAME view using embedded sources lazily; compound schema assigns correct per-resource IDs and preserves nested local refs.

Files: schema/embed.go, internal/cli/planning.go. Regression: Focused CLI/schema tests plus audit/regression_tests/R-007__contract.test.py; new-behavior checks must fail on released baseline and pass on candidate.

## R-008: Offer a static in-tool guide in text and JSON

Add --guide view with safe staged workflows, explicit module costs/worker trust, legacy versus aggregate output and inert planning interpretation.

Files: internal/cli/capability_guide.go, internal/cli/planning.go. Regression: Focused CLI/schema tests plus audit/regression_tests/R-008__contract.test.py; new-behavior checks must fail on released baseline and pass on candidate.

## R-009: Show usable inert follow-up steps in plain plans

Render bounded plan steps with JSON argv arrays, unresolved inputs, cost qualifications and revalidation; JSON output unchanged.

Files: internal/cli/planning.go. Regression: Focused CLI/schema tests plus audit/regression_tests/R-009__contract.test.py; new-behavior checks must fail on released baseline and pass on candidate.

## R-010: Correct legacy language schema for documented NaN output

Permit exactly the documented NaN percentage alongside decimal string percentages; validate actual attributed-empty legacy output without changing CLI output.

Files: schema/languages.schema.json, schema/schema_test.go. Regression: Focused CLI/schema tests plus audit/regression_tests/R-010__contract.test.py; new-behavior checks must fail on released baseline and pass on candidate.

## Ownership and completion

Root coordinates serialized production ownership and owns manifest/scope. Inventory/scorer A/CLI ergonomics owner may implement R-001–005 and R-009 after baseline preservation. A later explicit owner handles R-006–008 and R-010. Do not claim cross-model review: reviewers use one model family independently. Implementation design documents exact boundaries, schema reference handling, and privacy requirements.

## R-011: Render final CLI errors safely

Added during independent review after the original prioritization. Escape terminal control and format characters at the final CLI error boundary while preserving ordinary messages and underlying error causes. Public regression R-011 exercises missing source paths containing control characters; Go tests cover cause identity and revision errors. This follow-up receives no retroactive score uplift.
