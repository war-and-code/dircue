# Round 8 independent CLI/schema review

## Verdict

Clean. I found no additional correctness, compatibility, safety, or offline-usability defect in the final 1.0.0 CLI/schema preparation. The reviewed state is suitable for the planned external draft-PR review.

## Reviewed state

- Comparison base: `121027f`.
- Functional/schema source freeze: `932fb3e`.
- Final checkout reviewed: `5ef618f`, consisting of the frozen implementation, compatibility receipts, and the final two-file help-catalog disclosure correction.
- Final candidate binary: `.cache/v100/ready/dircue-review`, version `1.0.0-dev`, SHA-256 `97cabacd5babea8ae9fcf4c944162c6ff7108780b77f7606a4dd157a42565324`.
- Comparison binary: `.cache/v100/ready/dircue`, version `0.8.0`, SHA-256 `a33099ebc799a61867f662b51018223997f07d419c8e467faf42bf4314998547`.

I inspected `internal/cli`, `schema`, the associated tests, and the README/documentation changes. I did not inspect earlier review reports. Scanner-cache work was excluded as separately reviewed.

## Evidence

- The default `capabilities --json` output is byte-equivalent between 0.8.0 and the candidate after removing only `provider_version`, preserving the existing planner-capability contract.
- `capabilities --cli --json` enumerates 24 command paths, their command-scoped flags, rejected inherited flags, restrictions, output contracts, and 15 allowlisted schema resources from the completed Cobra tree. Repeated invocations were byte-identical.
- `capabilities --guide --json` produced 28 nonempty sections and was byte-identical across repeated invocations.
- The exported `profile` schema is a standalone Draft 2020-12 compound document with an absolute root identity and all seven referenced component resources embedded: availability, declarations, environments, explanation, focus, formats, and hotspots.
- Selector conflicts and invalid schema names fail without stdout. Typo handling remains refusal-only and provides the exact correction; for example, `--structural-wroker` exits 1 and suggests `--structural-worker` without executing the supplied value.
- The final `5ef618f` correction changes catalog metadata and its regression test only. A bounded final-binary probe confirmed that `help --json plan` still emits plain text while the machine-readable catalog now explicitly says that help's inherited analysis flags and `--json` are accepted only for parser compatibility and ignored, and points callers to the structured guide/catalog views.
- Static review found schema names, component-versus-whole-output qualifications, language percentage bounds (including the existing zero-byte `"NaN"` string), focused-metrics partial markers, saved-report restrictions, help examples, and README/schema documentation aligned with the emitted contracts.

## Limits

This was a bounded read-only review. I did not build the source, run heavy suites, execute code from inspected fixtures, rescan compatibility corpora, or evaluate scanner-cache changes. I relied on the separately reported post-freeze full race suite, `go vet`, and 319 compatibility cases, all passing after the final schema and help-catalog edits. Runtime checks were limited to public, offline, nonmutating binary surfaces. I did not independently exercise the compound schemas in a second JSON Schema implementation; the repository's offline schema compilation and parity tests cover that behavior.
