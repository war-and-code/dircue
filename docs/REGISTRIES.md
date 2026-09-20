# Package-source declarations

Added in 0.4.0.

Read selected NuGet and npm configuration explicitly:

```sh
dircue analyze registries --discovery --json /checkout
dircue analyze all --projects --registries --json /checkout
```

The first command combines file metadata with package-source declarations. It
does not classify languages or parse project manifests. The second adds these
declarations to the ordinary aggregate report and project inventory. Neither
command executes package managers, expands variables, contacts registries, or
searches for configuration outside the selected source, such as user or system
configuration directories.

## What the evidence means

The module selects case-insensitive `NuGet.Config` basenames and exact `.npmrc`
basenames from the selected regular files, including vendor and data paths.
Selection identifies candidates; it does not establish that a package manager
would load each file. Git mode reads the selected committed tree. Directory
mode observes live files and does not establish a snapshot.

NuGet observations cover supported package-source additions, clear operations,
disabled-source declarations, and package-source mappings. Source order and
the difference between an absent disabled value and `false` are retained.
Remove-shaped XML is recorded with unsupported semantics, rather than treated
as an effective removal. Nested NuGet configurations may identify ancestor
configuration candidates; the module does not evaluate their merged result.

npm observations cover ordinary top-level `registry` and `@scope:registry`
assignments. Supported comments and string escapes are handled explicitly.
Arrays, sectioned registry declarations, quoted keys, and ambiguous syntax
are qualified as unsupported. Nested `.npmrc` files remain separate project
configuration candidates; no parent-directory inheritance is inferred.

These are declarations, not proof that a build used or contacted a source.
User/system configuration, environment values, command-line options, project
context, and package-manager versions can change the effective configuration.
The module does not decide whether a registry is approved, private, public,
commercial, or necessary for a successful build.

## Output disclosure

For supported HTTP(S) URLs, output contains only a validated origin:
`scheme://host[:port]`. Userinfo, the entire URL path, query, and fragment are
discarded. Local and UNC feed paths are classified without retaining their
values. Uncertain authorities and unresolved variable references produce
statuses without copying the original value or variable name.

Authentication, proxy, certificate, and arbitrary configuration values are not
reported. Output contains no raw configuration excerpts, configuration-content
hashes, or parser error messages. Malformed XML discards that configuration's
declarations; diagnostics use fixed codes.

Qualified feed names, package scopes, package patterns, origins, and selected
file paths can still reveal internal infrastructure or organizational names.
Syntax checks do not establish that an arbitrary identifier is nonsensitive.
Treat the report accordingly before sharing it outside its intended audience.

## Coverage and bounds

The scanner reuses its existing inventory. Only admitted configuration
candidates retain readers, and complete bounded files are read sequentially.
An unrequested module creates no registry collector or reads. Regex matching
for supported values is initialized only when configuration is inspected.

| Bound | Maximum |
| --- | ---: |
| Admitted configuration files | 64 per ecosystem |
| Complete file bytes | 256 KiB |
| Retained declarations per file | 256 |
| Retained declarations per ecosystem | 1,024 |
| XML depth | 32 |
| XML tokens | 32,768 |
| npm line bytes | 8 KiB |

A positive `--max-file-bytes` can lower the complete-file limit. Candidate
retention uses lexical paths independently of worker order; NuGet and npm have
separate budgets. Declaration order follows the configuration, with explicit
counts for omitted entries. XML DTDs, custom entities, unsupported namespaces,
duplicate attributes, malformed encodings, and exceeded parser bounds cannot
produce unqualified successful declarations.

`scope.supported_configurations` identifies the NuGet and npm selection rules
even when no candidates were found. Other package managers are outside this
module's current scope. Coverage distinguishes candidate, admitted, read, and
parsed files. Check
`declaration_count_complete` before using a configuration's observed count as
a total. A parser limit may prevent counting the unseen remainder. A tree
limit makes enumeration incomplete; an empty successful inventory is different
from skipped enumeration. Read failures and cancellation fail the command
without returning a success-shaped partial report.

The optional `registries` object uses aggregate schema `1.3.0`, records its
rule version and source consistency, and reports `complete`, `partial`, or
`skipped` coverage. Complete refers to the supported declaration inspection,
not a complete or effective feed set. Existing invocations retain their prior
output schemas and the Linguist-compatible language interface.
