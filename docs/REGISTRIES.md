# Package-source declarations

Added in 0.4.0.

Read selected NuGet, npm, Maven and Cargo configuration explicitly:

```sh
dircue analyze registries --discovery --json /checkout
dircue analyze all --projects --registries --json /checkout
```

The first command combines file metadata with package-source declarations. It does not classify languages or parse project manifests. The second adds these declarations to the ordinary aggregate report and project inventory. Neither command executes package managers, expands variables, contacts registries, or searches for configuration outside the selected source, such as user or system configuration directories.

## What the evidence means

The module selects case-insensitive `NuGet.Config` basenames and exact `.npmrc`, `settings.xml`, `.cargo/config`, and `.cargo/config.toml` paths from selected regular files, including vendor and data paths. Selection identifies candidates; it does not establish that a package manager would load each file. Git mode reads the selected committed tree. Directory mode observes live files and does not establish a snapshot. The Maven and Cargo adapters never search outside the selected repository for global, user, or system settings.

NuGet observations cover supported package-source additions, clear operations, disabled-source declarations, and package-source mappings. Source order and the difference between an absent disabled value and `false` are retained. Remove-shaped XML is recorded with unsupported semantics, rather than treated as an effective removal. Nested NuGet configurations may identify ancestor configuration candidates; the module does not evaluate their merged result.

npm observations cover ordinary top-level `registry` and `@scope:registry` assignments. Supported comments and string escapes are handled explicitly. Arrays, sectioned registry declarations, quoted keys, and ambiguous syntax are qualified as unsupported. Nested `.npmrc` files remain separate project configuration candidates; no parent-directory inheritance is inferred.

Maven `settings.xml` observations cover the `id`, `mirrorOf`, and `url` fields of mirror declarations; profile IDs; repository and plugin-repository IDs and URLs under profiles; and active-profile IDs. Only empty-namespace settings and the Maven settings namespace versions 1.0.0, 1.1.0, and 1.2.0 are accepted. A profile's repositories retain its qualified profile ID as a scope when it passes the identifier allowlist. Profile activation, active-profile matching, mirrors, settings precedence, and effective repository sets are not evaluated; every Maven declaration reports `applicability=unresolved`. Credentials, proxies, local repository paths, properties, and other settings are ignored and counted as omissions. XML directives, custom namespaces, malformed documents and exceeded XML bounds discard that file's facts. Repeated recognized fields add an omission even when a repeated value is nested or too large to retain; duplicate profile IDs remain unresolved rather than assigning their repositories to the first ID.

Cargo `.cargo/config` and `.cargo/config.toml` observations cover the `index` field in `[registries.<name>]`, the `default` field in `[registry]`, and `[source.<name>]` declarations. Source fields `registry`, `git`, `directory`, `local-registry`, and `replace-with` are retained only as qualified names, sanitized origins, or status-only path references. A `sparse+https://` or `sparse+http://` index is reduced to its validated HTTP origin. A bare path in a URL field is invalid; only path-valued fields such as `directory` and `local-registry` produce local-path classifications. Local paths and unresolved values never retain their input. `replace-with` records the declared source name but does not follow or validate replacement chains. Config precedence, credentials, registry access, and effective source selection are not evaluated; each Cargo declaration reports `applicability=unresolved`. Unsupported Cargo fields are omitted with fixed reason counts. A nonempty or malformed top-level `include` declaration makes coverage partial with `unsupported_cargo_include`; included files are never opened, and their paths are not retained. An empty include list does not omit any configuration.

These are declarations, not proof that a build used or contacted a source. User/system configuration, environment values, command-line options, project context, and package-manager versions can change the effective configuration. The module does not decide whether a registry is approved, private, public, commercial, or necessary for a successful build.

## Output disclosure

For supported HTTP(S) URLs, output contains only a validated origin: `scheme://host[:port]`. Userinfo, the entire URL path, query, and fragment are discarded. Local and UNC feed paths are classified without retaining their values. Uncertain authorities and unresolved variable references produce statuses without copying the original value or variable name.

Authentication, proxy, certificate, and arbitrary configuration values are not reported. Output contains no raw configuration excerpts, configuration-content hashes, or parser error messages. Malformed XML discards that configuration's declarations; diagnostics use fixed codes.

Qualified feed names, package scopes, package patterns, origins, and selected file paths can still reveal internal infrastructure or organizational names. Syntax checks do not establish that an arbitrary identifier is nonsensitive. Treat the report accordingly before sharing it outside its intended audience.

## Coverage and bounds

The scanner reuses its existing inventory. Only admitted configuration candidates retain readers, and complete bounded files are read sequentially. An unrequested module creates no registry collector or reads. Regex matching for supported values is initialized only when configuration is inspected.

| Bound | Maximum |
| --- | ---: |
| Admitted configuration files | 64 per ecosystem |
| Complete file bytes | 256 KiB |
| Retained declarations per file | 256 |
| Retained declarations per ecosystem | 1,024 |
| XML depth | 32 |
| XML tokens | 32,768 |
| TOML depth | 32 |
| TOML tokens | 32,768 |
| npm line bytes | 8 KiB |
| Captured Maven field bytes | 8 KiB |

A positive `--max-file-bytes` can lower the complete-file limit. Candidate retention uses lexical paths independently of worker order; each ecosystem has its own configuration and declaration budgets. Declaration order follows each configuration, with explicit counts for omitted entries. XML DTDs, custom entities, unsupported namespaces, duplicate attributes, malformed encodings, and exceeded parser bounds cannot produce unqualified successful declarations. Cargo TOML is parsed with a bounded generic decoder after a bounded structural preflight; duplicate keys and malformed TOML discard that file's facts.

`scope.supported_configurations` identifies the NuGet, npm, Maven and Cargo selection rules even when no candidates were found. Other package managers are outside this module's current scope. Coverage distinguishes candidate, admitted, read, and parsed files. Check `declaration_count_complete` before using a configuration's observed count as a total. A parser limit may prevent counting the unseen remainder. A tree limit makes enumeration incomplete; an empty successful inventory is different from skipped enumeration. Cancellation fails the command without returning a success-shaped partial report. Read failures also fail by default; `--on-error continue` instead records the unreadable configuration with a per-file `file_read_error` omission and marks the report partial, keeping the remaining configurations. The caller's underlying I/O error text is never disclosed through this report in either mode.

The optional `registries` object records its rule version and source consistency, and reports `complete`, `partial`, or `skipped` coverage. Complete refers to the supported declaration inspection, not a complete or effective feed set. Adding adapters advances the registry rule version and adds supported-configuration labels; existing NuGet and npm declaration facts keep their meanings. Existing invocations retain their prior output schemas and the Linguist-compatible language interface.

## Uncovered configuration

The parser covers NuGet.Config, .npmrc, Maven settings.xml, and Cargo config files. It does not cover pip.conf, Yarn configuration, Maven POM repositories, environment or command-line overrides, or other package sources. An empty result does not rule out registry declarations in those inputs. `enumeration_complete` describes the selected inventory under this supported scope; it does not establish registry coverage for every ecosystem.

The supported XML and TOML subsets follow the [Maven Settings Reference](https://maven.apache.org/settings.html), [Cargo Configuration Reference](https://doc.rust-lang.org/cargo/reference/config.html), [Cargo Registries Reference](https://doc.rust-lang.org/cargo/reference/registries.html), and [Cargo Source Replacement Reference](https://doc.rust-lang.org/cargo/reference/source-replacement.html). These references describe package-manager behavior; dircue reports selected declarations only and does not implement that behavior.
