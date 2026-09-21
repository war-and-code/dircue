# Explain profiling decisions

Added in 0.7.0.

```sh
dircue analyze explain --file src/main.go --json /checkout
dircue analyze explain --file vendor/library.js --source directory /checkout
dircue analyze explain --project services/app/app.csproj --json /checkout
dircue analyze explain --file src/main.go --report saved-profile.json --json
```

Choose exactly one root-relative `--file` or `--project`. Fresh file explanations run the actual language-selection path for that file, including vendor and generated rules, detection strategy, language grouping, read limits, and supported attribute overrides. Attribute evidence includes the defining file and line when retained. No second classifier guesses why the first classifier made its decision.

Fresh inspection still traverses the selected-source metadata to preserve tree limits and attribute context. It classifies only the requested regular file. A Git run reads the selected committed tree; use `--source directory` for current files. A fresh project query reads supported declarations across the selected source, then presents the target's retained facts. It does not evaluate the build.

## Saved evidence

`--report` reads an explicit aggregate report without opening its root or evidence paths. It cannot be combined with a source directory or scan flags. The input is bounded and validated against bundled schemas, with the same strict JSON decoding used by saved-report comparison. New schema 1.6.0 is supported for explanations even though comparison does not yet support it.

A matching saved explanation preserves its recorded decision and becomes retained evidence. Other queries use only available file lists, declarations, focus boundaries, and availability observations. A strategy is absent when it was not retained. Missing file detail or an unrecorded exclusion reason produces an unavailable decision with an omission reason. Absence from an included-file list does not establish why a file was omitted or whether it was inspected.

`source.report_sha256` identifies the exact supplied report bytes. It does not authenticate the report or prove repository identity. Legacy reports without source provenance remain unknown; the directory name is not used to invent a Git identity. Conflicting recorded source identities are rejected.

`observation_id` identifies the provider/query/source/scope combination; it is not a digest of file contents or every reported fact. In particular, live directory observations are not globally unique across roots or changes. Preserve the enclosing source context when storing them.

## Reading the answer

The `explanation` object separates source, query, analysis scope, read extent, steps, facts, final decision, and omissions. A project ID retained by a parser is a reported candidate; it is not automatically proof of successful parsing or a working build. Conditions and unresolved target states remain visible.

Trace and fact collection are bounded. Explanations retain at most 32 steps, 16 overrides, 256 facts, and 16 omission categories, with bounded strings. Missing or capped evidence yields a partial explanation. Ordinary language scans do not retain these traces.

A successful explanation can describe unavailable evidence and still exit zero. Invalid options or report input, failed reads, and failed writes return errors. Structured output is an aggregate schema 1.6.0 report; `dircue --json` keeps its existing Linguist-compatible layout.
