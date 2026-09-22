# File format evidence

`dircue analyze formats` inspects small prefixes of selected regular files. It can describe mixed directories containing documents, structured data, archives and executable artifacts, including files excluded from language statistics.

```sh
dircue analyze formats --json /path/to/content
dircue analyze formats --source directory --json /path/to/content
dircue analyze all --formats --discovery --json /path/to/content
```

The standalone command does not run language classification or structural workers. The `--formats` option adds the same module to `analyze all`. Existing commands do not enable it automatically. Reports containing this module use aggregate schema version `1.5.0`.

The default source policy is unchanged: a detected Git repository supplies the selected committed tree; an ordinary directory supplies its live files. Use `--source directory` to inspect working files, including untracked changes. Symlinks and special files are omitted. The module does not expand archives, decompress data, execute content or access external XML entities.

## Read the evidence, not just the extension

Each retained file has an `evidence` array. Evidence entries can overlap; their counts are not mutually exclusive file categories.

| `basis` | What it establishes |
| --- | --- |
| `extension_hint` | The filename has a recognized extension. Its contents may contradict the hint. |
| `signature_match` | Bytes at a supported header location match a known signature. Container integrity and the rest of the file remain unchecked. |
| `parsed_prefix` | The inspected prefix met the named validation profile, or a structured parser processed a supported prefix before its bounded end. It does not validate the remaining bytes. |
| `complete_validation` | The entire file was read and passed the specific profile in `validation`. This is syntax or encoding evidence, not schema, application or semantic validation. |

A file named `records.xml` containing ZIP header bytes can have both an XML extension hint and a ZIP signature match. It is not reported as a validated XML document. XML evidence alone does not establish that a file contains logs, configuration, source code or any other particular kind of data.

`read_scope` independently says whether the read was `complete` or `prefix`. Reading an entire ZIP file still produces only a signature match: no archive validation is performed. An empty `evidence` array means none of the supported detectors established evidence for that file; it does not establish that the file is binary.

## Supported checks

| Format | Check |
| --- | --- |
| JSON | UTF-8 JSON syntax, including scalar values. Invalid UTF-8 and unpaired UTF-16 escapes are rejected. Duplicate object keys are allowed. Numbers are checked syntactically without conversion to a machine numeric type. |
| XML | UTF-8 token syntax with one root, matching element tokens and no non-whitespace content outside that root. DTDs and directives are unsupported. XML declarations must use the supported XML 1.0 / UTF-8 profile. |
| Text | Valid UTF-8 without NUL, DEL or disallowed C0 controls. Tab, line feed, carriage return and form feed are allowed. This is a text-encoding check, not a source/prose classification. |
| Empty | Zero bytes in a complete read. |
| ZIP, gzip, 7z, ELF, PDF, PNG, JPEG, SQLite | Supported header magic only. |
| DOS executable | An initial `MZ` signature only. |
| PE | An `MZ` signature plus a referenced `PE` signature within the inspected prefix. |

XML uses Go's token parser with additional document-boundary checks. It does **not** validate schemas, DTDs, namespace bindings or the complete XML specification. Its profile is named `utf8_single_root_token_syntax_no_dtd` to make that limit explicit. Non-UTF-8 XML is unsupported.

JSON is attempted for recognized JSON extensions or prefixes beginning with `{` or `[`. XML is attempted for recognized XML extensions or prefixes beginning with `<`, allowing a UTF-8 BOM at the beginning. A JSON scalar in an extensionless file can therefore receive text evidence without JSON evidence. Incomplete quoted JSON strings and incomplete Unicode sequences receive conservative diagnostics instead of repaired text being accepted as JSON.

Filename hints include XML-related extensions such as `.svg` and `.csproj`; JSON and `.ipynb`; ZIP-based names such as `.jar`, `.whl`, `.nupkg` and `.docx`; and common text, image, PDF, executable and database extensions. These hints do not imply that a specialized document or package parser ran.

A text prefix can end partway through a UTF-8 character. The text check ignores only an incomplete terminal character; invalid bytes elsewhere do not qualify as valid UTF-8. Text evidence for a prefix makes no statement about later bytes or another possible encoding.

## Scope and bounds

The module selects the lexically first eligible paths independently of scanner worker order. Vendor directories and data files remain eligible even when Linguist attributes exclude them from language totals.

| Bound | Value |
| --- | --- |
| Selected candidates retained for inspection | 4,096 files |
| Representable relative path | 2,048 UTF-8 bytes |
| Inspected prefix per file | 64 KiB |
| Read lookahead | At most one additional byte per file |
| Total bytes read by this module | 32 MiB, including lookahead |
| Structured parser depth | 64 |
| Structured parser tokens | 65,536 per file |
| Serialized module output | 16 MiB |

The scanner's existing `--tree-size` limit still applies. Its `--max-file-bytes` option skips source files larger than that limit; the default `0` adds no source-size cutoff. This is separate from the module's 64 KiB prefix limit: by default, a multi-gigabyte XML file is eligible for bounded prefix inspection.

The final file read may receive less than 64 KiB when it reaches the cumulative budget. Prefixes are not a random or statistically representative sample. Lexical selection can favor one directory in a large tree, so inspect coverage before drawing conclusions about a whole repository.

`inspected_bytes` counts bytes returned by this module's selected-source readers, including lookahead. It does not measure total process I/O, Git object decompression, attribute reads or reads by other selected modules. Complete reading and format validation are distinct: malformed syntax can be observed in a complete read.

## Status, coverage and comparison

| Field | Meaning |
| --- | --- |
| `source` | Selected source mode, Git tree when applicable, and consistency description. Directory reads are live, not an atomic snapshot. |
| `scope` | Regular-file population, deterministic selection policy and disabled archive expansion. |
| `limits` | The module bounds and selected source-file size limit. |
| `coverage.selected_files`, `selected_bytes` | Regular-file inventory before the module's own read/selection limits. |
| `coverage.inspected_files`, `inspected_bytes` | Files read and bytes returned to the module. |
| `coverage.complete_reads`, `prefix_reads` | Extent of those reads, independent of format-validation success. |
| `coverage.omitted_files` | Selected regular files not read because of module limits or an unrepresentable path. |
| `coverage.retained_observations` | File observations retained after the output bound. |
| `observations` | Relative paths, sizes, read extents, fixed evidence labels and diagnostics. |
| `omissions` | Counted selection/output exclusions, including non-regular entries and bounds reached. |

`complete` means the bounded inspection completed without selection or output omissions. It does not mean every file was fully read or valid. `partial` indicates omissions or an observed changing/incomplete source. `skipped` means the selected inventory was not inspected, for example after the scanner's tree-size limit. A source read failure fails the command by default; it is not silently converted into a successful report. Under `--on-error continue` an unreadable candidate is recorded as a `file_read_error` omission in the module's partial report and the remaining candidates still contribute observations.

A changed file size is reported as `source_changed_or_incomplete_read`, and any complete-validation evidence is downgraded to prefix evidence. Changes that preserve size during a live directory read cannot always be detected. Git tree selection provides stronger source consistency.

Per-file diagnostics identify unsupported or malformed syntax and parser limits. They do not by themselves make the module status partial: the requested inspection may have completed and found an invalid input. No raw parser messages, XML names, JSON values or payload excerpts are included. Relative filenames can still contain sensitive information.

Save aggregate JSON reports for offline comparison:

```sh
dircue analyze formats --json ./before > before.json
dircue analyze formats --json ./after > after.json
dircue compare before.json after.json --json
```

Comparison checks source policy, provider version, scope and limits. Missing observations under incomplete coverage cannot prove removal. Files can change without changing their format observations; this is not a content diff. The comparison does not establish whether a package is usable, an archive is intact or a file is safe to execute.
