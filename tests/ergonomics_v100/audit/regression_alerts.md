# Dimension decrease review

No overall surface decrease exceeds the 50-point hard-stop threshold. The following 22 dimension changes were investigated; none establishes a new behavior regression. We retain the scores rather than editing them upward.

- Static schema surfaces previously received 1000 for non-applicable intent inference. They now have an executable exact-name export interface, so the post reviewers judged that interface instead. Invalid names receive the canonical choices; the original schema resources are still available. This is an applicability change, not lost functionality.
- Help and group commands were previously assigned non-applicable parseability by one cohort. Their text remains text; structured alternatives now exist through the explicit catalog/guide. Strategies also retains its legacy text contract.
- SIGINT/SIGTERM handling is unchanged. The lower safety score and low signal regression-resistance scores reflect conservative source-only assessment and missing direct process-signal coverage, not a changed cancellation path.
- Focus-selection refusal is unchanged; the later cohort gave its existing diagnostic less credit. The 319-case preservation run and intent replay show no new failure.

| Surface | Dimension | Before | After | Difference |
|---|---|---:|---:|---:|
| error__focus-selection | error_pedagogy | 750 | 650 | -100 |
| flag__h | output_parseability | 625 | 500 | -125 |
| flag__help | output_parseability | 625 | 500 | -125 |
| flag__s | output_parseability | 675 | 575 | -100 |
| flag__strategies | output_parseability | 675 | 575 | -100 |
| schema__availability | intent_inference | 1000 | 500 | -500 |
| schema__capabilities | intent_inference | 1000 | 500 | -500 |
| schema__comparison | intent_inference | 1000 | 500 | -500 |
| schema__declarations | intent_inference | 1000 | 500 | -500 |
| schema__environments | intent_inference | 1000 | 500 | -500 |
| schema__explanation | intent_inference | 1000 | 500 | -500 |
| schema__findings | intent_inference | 1000 | 500 | -500 |
| schema__focus | intent_inference | 1000 | 500 | -500 |
| schema__formats | intent_inference | 1000 | 500 | -500 |
| schema__hotspots | intent_inference | 1000 | 500 | -500 |
| schema__languages | intent_inference | 1000 | 500 | -500 |
| schema__planning | intent_inference | 1000 | 500 | -500 |
| schema__profile | intent_inference | 1000 | 500 | -500 |
| signal__SIGINT | safety_with_recovery | 1000 | 850 | -150 |
| signal__SIGTERM | safety_with_recovery | 1000 | 850 | -150 |
| verb__analyze | output_parseability | 1000 | 575 | -425 |
| verb__help | output_parseability | 1000 | 475 | -525 |
