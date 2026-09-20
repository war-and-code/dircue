# Caller-supplied observation rules

Added in 0.4.0.

Rules add factual observations about selected files. Supply a JSON ruleset explicitly:

```sh
dircue analyze rules --rules-file /pipeline/policy/rules.json --json /checkout
dircue analyze all --rules-file /pipeline/policy/rules.json --json /checkout
```

The first command runs rule matching without language classification, project parsing, metrics, or a structural worker. The second adds rules to the usual `all` report. Neither command discovers a ruleset from repository files. A caller that requires trusted policy must choose a trusted path; dircue cannot establish who authored a supplied file.

Add `--discovery` to `analyze rules` to collect regular-file counts and candidate manifests in the same traversal. This combination keeps language classification and the other content profilers disabled; only explicitly requested content rules read file payloads:

```sh
dircue analyze rules --discovery --rules-file /pipeline/policy/rules.json --json /checkout
```

Rules cannot disable language analysis, change `.gitattributes`, execute commands, or launch other tools. They do not assign severity, quality grades, or scanner recommendations.

## Ruleset format

```json
{
  "schema_version": "1.0.0",
  "rules": [
    {
      "id": "talend-job-candidate",
      "match": {
        "path_prefixes": ["process/"],
        "extensions": [".item"]
      }
    },
    {
      "id": "custom-build-convention",
      "match": {
        "filenames": ["build.gradle.kts"]
      },
      "content": {
        "contains_utf8": "com.example.build-conventions"
      }
    }
  ]
}
```

Each rule needs a unique ID and at least one nonempty metadata selector. IDs contain 1–128 ASCII letters, digits, dots, underscores, or hyphens, starting with a letter or digit.

Selectors are case-sensitive. Values within one selector are alternatives; different selectors must all match. `filenames` matches the exact basename. `extensions` matches a literal suffix, including compound suffixes such as `.d.ts`. `path_prefixes` uses clean relative directory paths ending in `/`, such as `process/`. Paths use `/` on all platforms. There are no glob or regular-expression operators: a filename containing `*` means a literal asterisk.

A rule with `content.contains_utf8` first matches metadata, then checks for the exact UTF-8 byte sequence in the complete file. A prefix of a large file cannot establish a negative result. Matched text and rule literals are not copied into reports. Content matches include the SHA-256 digest of the complete bytes inspected; metadata-only matches have no source-content digest.

Invalid rulesets fail before scanning. Unknown fields, duplicate JSON keys or rule IDs, malformed Unicode, and exceeded limits are errors. There are no includes, environment substitutions, or named profiles.

## Scope and limits

The scanner delivers selected regular files independently of language-statistics exclusions. This includes vendor, generated, and data paths. In Git mode, matching uses the selected Git tree, including committed content rather than dirty working files. Directory mode reads live files and does not promise a snapshot. Symlinks and other special files are not followed.

Metadata matching uses the existing traversal. Content candidates are selected in lexical path order, independent of worker completion order. Matching retains at most 256 eligible candidate records and reads their contents sequentially after traversal. It does not retain source payloads from other modules. `.gitattributes` processing and Git storage access can still perform their normal bounded reads; metadata-only rules do not make the whole scan free of I/O.

| Limit | Value |
| --- | ---: |
| Ruleset bytes | 256 KiB |
| Rules | 256 |
| Values per selector | 64 |
| Total selector values | 4,096 |
| Literal bytes | 4,096 |
| Relative path bytes | 4,096 |
| Complete content bytes per eligible file | 64 KiB |
| Content files per scan | 256 |
| Retained observations | 1,024 |

A positive `--max-file-bytes` below 64 KiB lowers the content limit. Larger files can still match metadata rules. Size, invalid UTF-8, incomplete content, content-budget, and inventory omissions are counted explicitly. Read failures fail the scan; they are not converted into a successful report. Cancellation also returns an error.

Observed size changes are reported as incomplete content. An equal-length concurrent edit in a live directory cannot always be detected; the digest identifies the bytes actually matched. Use a selected Git tree when a stable source identity is required.

## Report contract

Requesting rules selects structured schema `1.3.0`. Without `--rules-file`, existing invocations keep their prior schemas and results. Legacy `dircue --json` has no rules option and preserves its Linguist-compatible format.

The optional `rules` object records:

- Exact ruleset-byte SHA-256, ruleset schema, and evaluator version.
- Source kind, selected Git tree when applicable, and source consistency.
- Effective limits and selected-file, candidate, evaluation, match, and omission counters.
- Per-rule candidate, evaluated, matched, and content-omission counts.
- Observations ordered by `(path, rule_id)`, retaining the first 1,024 while counting all matches.

`complete` means matching covered the selected population within the declared scope. `partial` identifies omitted inputs, content, or retained evidence. A tree-size limit that prevents inventory produces `skipped`. An empty successfully inventoried directory can be complete with zero matches. These states describe coverage, not whether a directory is safe or well built.

The top-level language summary keeps its existing meaning. Use `rules.inventory_files` for this module's population; excluded language files can contribute observations.

The Go `pkg/rules` package provides an immutable compiled `Program` and a single-owner `Collector`. Embedding callers must deliver metadata once per selected path and report omissions. Compilation and evaluation perform no filesystem access. `pkg/scanner` supplies traversal, deterministic admission, bounded reads, and source identity.
