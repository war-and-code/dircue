# Registry declaration evidence

The module reads only bounded content supplied by the embedding scanner. It observes declarations in selected `NuGet.Config` and `.npmrc` files; it does not reproduce a package manager's effective configuration.

Tests in `pkg/registries/registries_test.go` use independent expected origins, declarations, counters and failure outcomes. Credential sentinels appear in URL userinfo, paths, query strings, fragments, authentication fields, unknown sections and malformed input. Reports and fixed errors must omit those sentinel values. Validated feed labels, npm scopes, package patterns, origins and configuration paths can nevertheless identify internal infrastructure or projects. Syntax validation is not secret detection: arbitrary sensitive text deliberately placed in a permitted identifier or hostname cannot be distinguished from an ordinary name.

Declared source order, clear operations, disabled Booleans and mapping patterns remain observations. `remove` is retained with unsupported semantics. A mapping source container has a `map` row with no pattern, followed by its package pattern rows; this preserves empty mapping declarations without inventing a pattern. Indices follow document order and are not executable priorities. NuGet ancestor candidates are selected configuration paths in ancestor directories; npm files do not receive ancestor inheritance claims.

Supported npm values are ordinary unquoted strings with npm/ini comment escapes, JSON double-quoted strings, and single-quoted values that npm/ini interprets as strings. Inner JSON numbers, arrays, objects, Booleans and null are omitted, while an inner JSON string can produce an origin. Quoted keys, arrays and sectioned registry settings are explicitly omitted. Quoted values with trailing comments and ambiguous quoting are not interpreted as registry URLs. CR, LF and CRLF line endings are accepted. Duplicate registry assignments remain separate declarations; no winner is selected.

NuGet uses UTF-8 XML with an optional BOM and a single leading XML 1.0 declaration. That declaration supports optional UTF-8 encoding and yes/no standalone attributes in specification order, with single or double quotes. Other declarations are rejected rather than relying on Go's permissive processing-instruction parsing. Required attribute spellings and supported section names are conservative and match the upstream lookup constraints described in `research.json`. DTDs, custom entities, namespaces, processing instructions other than the supported declaration, malformed XML and configured parser limits discard all declarations from that document. Unrelated settings and credential sections are ignored. The module does not support arbitrary custom NuGet configuration filenames selected by an external command.

Origins include only HTTP(S), a validated ASCII hostname or IP address, and an optional validated port. URL userinfo, paths, queries and fragments never appear. Local paths produce a `local_path` status without their value. Placeholders produce `unresolved` without names or expansion. Unsupported or uncertain origins produce a fixed status without their input. Empty or malformed URLs are not replaced with a default registry.

The collector selects at most 64 files per ecosystem by relative path, reads at most 256 KiB plus a one-byte completeness probe per admitted file, retains at most 256 declarations per file and 1,024 per ecosystem, and checks context around each supplied callback. `MaxFileBytes` can lower the read limit. Metadata-oversized files are not read. Selected readers must remain alive until `Finish` returns and must respect its maximum byte count. The scanner supplies each selected path once. No config contents or config-content digests are stored in reports or evidence caches.

`ObservedDeclarations` counts recognized declaration rows in fully parsed admitted documents. It is not a total across omitted files or unsupported syntax. `DeclarationCountComplete` is false when parsing was not attempted, failed or stopped at a parser bound; those files retain no provisional declaration counts. Retention caps preserve observed counts separately from retained entries. Coverage and omissions must be read together, especially when the selected tree or configuration budget is incomplete.

Run focused checks from the repository root:

```sh
go test ./pkg/registries -count=1
go test -race ./pkg/registries -count=1
go test ./pkg/registries -run '^$' -fuzz FuzzParseDeterminismAndBounds -fuzztime 30s -parallel 2
python3 tests/registries/npm_conformance.py --output .cache/npm-conformance-new.json
```

The npm conformance command downloads and runs the explicitly pinned npm/ini parser as test tooling, using Node.js and synthetic fixtures only. Its retained receipt contains sanitized observations, upstream tool source identity and case outcomes, not raw oracle values. No inspected repository code runs. `npm-conformance-reviewed.json` records 21 passing cases against npm/ini v6.0.0; the earlier 13- and 20-case receipts remain as historical subsets. The expanded cases cover independently found single-quoted JSON coercion and numeric-overflow bugs that were fixed before integration. The overflow case checks the oracle's numeric type because JSON serialization converts JavaScript Infinity to null. Synthetic fixture files contain recognizable test sentinels, not credentials from a checkout.

Scanner and CLI integration tests are maintained separately. This evidence does not claim full NuGet/npm parser compatibility or effective-feed discovery.

`validation.json` binds the focused validation to the package/test source hashes and retained artifacts. The reviewed fuzz run passed 572,217 executions in 31 seconds; its invariants cover deterministic output, declaration bounds and origin-only URL structure. Earlier fuzz logs remain historical evidence and did not discover every issue subsequently found by independent review.
