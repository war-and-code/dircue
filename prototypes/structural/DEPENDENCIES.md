# Structural worker dependencies

The standalone prototype Go driver uses only the standard library. The worker
also serves the 0.3.0 production adapter. It uses the parser stack below, resolved
with checksums in [Cargo.lock](worker/Cargo.lock).

| Component | Version | License |
| --- | --- | --- |
| [big-code-analysis](https://github.com/dekobon/big-code-analysis/tree/v2.2.0) | 2.2.0 | MPL-2.0 |
| [Tree-sitter](https://github.com/tree-sitter/tree-sitter) | 0.26.12 | MIT |
| bca-tree-sitter-ccomment | 2.2.0 | MIT |
| bca-tree-sitter-preproc | 2.2.0 | MIT |
| bca-tree-sitter-tcl | 2.2.0 | MIT |
| dekobon-tree-sitter-groovy | 0.2.2 | Apache-2.0 OR MIT |
| tree-sitter-bash | 0.25.1 | MIT |
| tree-sitter-c | 0.24.2 | MIT |
| tree-sitter-c-sharp | 0.23.5 | MIT |
| tree-sitter-cpp | 0.23.4 | MIT |
| tree-sitter-elixir | 0.3.5 | Apache-2.0 |
| tree-sitter-go | 0.25.0 | MIT |
| tree-sitter-irules | 0.1.1 | MIT |
| tree-sitter-java | 0.23.5 | MIT |
| tree-sitter-javascript | 0.25.0 | MIT |
| tree-sitter-kotlin-ng | 1.1.0 | MIT |
| tree-sitter-lua | 0.5.0 | MIT |
| tree-sitter-objc | 3.0.2 | MIT |
| tree-sitter-perl | 1.1.2 | MIT |
| tree-sitter-php | 0.24.2 | MIT |
| tree-sitter-python | 0.25.0 | MIT |
| tree-sitter-ruby | 0.23.1 | MIT |
| tree-sitter-rust | 0.24.2 | MIT |
| tree-sitter-typescript | 0.23.2 | MIT |

BCA's default features are disabled. The explicit worker features enable ordinary
language parsers, including F5 iRules; Firefox-specific variants are omitted.
The production adapter can select 20 languages through its Enry identities.
F5 iRules remains a standalone-worker capability because the pinned Enry catalog
has no matching language identity. C-family helper grammars are transitive
requirements; they are not extra user-selectable analysis languages.

BCA's annotated `v2.2.0` tag resolves to commit
`09fb2301528e12916b9b4514c01db814de8f0a74`. The worker consumes the unmodified
crates.io release. It does not copy BCA source into dircue's Go module or change
BCA's license. Dircue's own driver and worker code use its MIT license.

The worker release archives include complete checksum-verified crate sources for
every resolved dependency, their licenses/notices, worker source, Cargo manifest,
lockfile, and build provenance. In particular, the unmodified BCA crate source
accompanies the executable. The local prototype Docker image also includes crate
archives under `/usr/share/dircue-structural/notices`. Preserve these files when
redistributing the worker. See the [production redistribution guide](../../docs/STRUCTURE.md#dependencies-and-redistribution).

Tree-sitter grammars and their generated node identifiers must stay paired with
BCA's metric implementations. Upgrade BCA and its grammar pins together, rerun
syntax/metric fixtures, and compare corpus outputs before accepting an update.
A grammar with the same language name is not necessarily interchangeable.

The broader `xberg-io/tree-sitter-language-pack` is not included. BCA owns the parse
used by both consumers; no second parser runtime or grammar-download mechanism is
needed for the enabled languages.
