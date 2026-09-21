# Bundled JSON schemas

`Names()` lists the exact canonical names accepted by `Export(name)`. Names do
not include `.schema.json`; paths, URLs, fragments, and case variants are not
accepted. Export returns deterministic Draft 2020-12 JSON and never reads local
files or retrieves remote resources. It does not validate a caller's report.

Each exported document has an absolute `$id` under
`https://dircue.invalid/schema/`. A compound document includes the transitive
resources it references; each embedded resource retains its own absolute `$id`
and local `$defs` scope. These URIs are resource identities, not download
locations. A consumer can register the one exported document with an offline
validator under any retrieval location. The validator must support Draft
2020-12 compound schema documents and should disable external resource loading.

The `profile` schema describes aggregate reports. `availability`, `declarations`,
`environments`, `explanation`, `focus`, `formats`, and `hotspots` describe
components within those reports, not necessarily the complete output of a
similarly named command. `findings` describes the standalone framework/ecosystem
array; `languages` describes Linguist-compatible **directory** language maps.
The distinct legacy single-file response currently has no bundled schema.
`capabilities`, `planning`, and `comparison` describe their dedicated outputs.
`cli-capabilities` describes the explicit CLI catalog, and `guide` describes the
offline guide's section and argument-array format.

Directory language percentages are strings with two decimal places, or the
existing string `"NaN"` for attributed zero-byte populations. This is not a
nonstandard numeric JSON NaN value. Aggregate profile percentages remain numeric
and retain their existing constraints.

`ValidateProfile` continues to use its existing lazy, offline validator. The
export API does not initialize validation or change validation scope.
