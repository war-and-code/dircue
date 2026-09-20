# Metadata discovery

Added in 0.4.0.

```sh
dircue analyze discovery --source directory --json /path/to/content
dircue analyze discovery --source git --rev HEAD --json /path/to/repo
dircue analyze all --discovery --projects --json /path/to/repo
```

Discovery inventories regular-file metadata and reports filename-based
candidates for manifests, shared configuration, source, data, and packaged
artifacts. It includes vendored and generated paths that language statistics
may exclude. It does not open source payloads, parse manifests, expand archives,
validate binary formats, or run another tool.

`analyze discovery` skips language classification and the other content
profilers. `analyze all --discovery` adds the same metadata observations to
the requested analysis. A `.csproj` filename is evidence of a candidate
project; use `analyze projects` when its declarations are needed.

## Reading the report

The aggregate profile uses schema `1.3.0` when discovery is requested. Its
`discovery` field includes:

| Field | Meaning |
| --- | --- |
| `source` | Directory or selected Git tree, with the consistency model stated. A live directory is not an immutable snapshot. |
| `scope` | Input population, metadata-only inspection, tree limit, and per-kind evidence limit. |
| `inventory` | Regular-file and byte totals observed within the selected inventory. |
| `categories` | Mutually exclusive filename-based categories; these counts sum to the observed inventory. |
| `roles` | Overlapping attribute or path hints, such as vendored, generated, documentation, or possible tests. Do not sum these as exclusive populations. |
| `candidate_counts` | Counts before evidence sampling, including candidates whose paths are omitted. |
| `candidates` | Deterministic path samples for manifests, shared configuration, and artifacts. |
| `omitted_candidates` | Evidence paths omitted by the per-kind cap. |
| `omissions` | Input exclusions or limits that affect the report. |
| `rule_version`, `linguist_data_commit` | Dircue metadata rules and the inherited Linguist data revision. |

Each candidate kind retains up to 256 paths in lexical order. Sampling is
independent by kind: thousands of artifacts cannot displace the only manifest.
Counts still cover all observed candidates. Exceeding a sample cap makes the
report partial because the path list is incomplete; it does not reduce those
counts. A tree limit can omit the inventory itself, so inspect status and
omissions before using any total.

`complete` means the supported metadata work finished with complete evidence
enumeration. It does not mean every file format or project was recognized.
`partial` exposes missing evidence; `skipped` means the inventory was not
performed, such as when the tree-size limit was reached. I/O failures remain
errors. A successful process can contain partial or skipped reports.

## Cost and interpretation

`classification_bytes_read` is zero for the discovery module. The scanner
still reads bounded `.gitattributes` files, directory metadata, and Git storage
when applicable. Git may need storage work to determine object sizes. On a
combined invocation, other requested modules can read source content; the
discovery field does not describe all process I/O.

The existing `--max-file-bytes` option limits content analysis. It does not
hide a large regular file from metadata discovery. `--tree-size` still limits
the inventory. Symbolic links and special files are not followed or inspected
as regular content.

Directory inventories exclude `.git` directories, as the existing scanner
does. The totals describe the selected content inventory, not all bytes used
by a checkout on disk. Top-level `summary` counters still describe language
analysis; a discovery-only invocation skips that analysis. Use
`discovery.inventory` and `discovery.status` for metadata coverage.

Filename matches are hints. An XML extension does not establish that a file is
a log or even valid XML. A DLL filename does not establish a managed assembly,
dependency provenance, or executable validity. Generated-code roles here use
explicit attributes; content-based generated-code heuristics are not run.
Test-path hints describe naming conventions, not execution coverage.

Use the evidence to choose explicit follow-ups by project or subtree, retaining
relevant shared configuration. Small manifests beside large datasets must not
disappear behind byte percentages. Unknown formats and empty source counts
are not a guarantee that further analysis is unnecessary. See the
[staged-analysis guide](STAGED_ANALYSIS.md) and [roadmap](https://github.com/war-and-code/dircue/issues/41).
