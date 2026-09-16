# Prototype dependencies

The Go driver uses only the standard library. The worker uses the following
parser stack, resolved with the checksums in [Cargo.lock](worker/Cargo.lock):

| Component | Version | Enabled scope | License |
| --- | --- | --- | --- |
| [big-code-analysis](https://github.com/dekobon/big-code-analysis/tree/v2.2.0) | 2.2.0 | `java`, `csharp`; default features disabled | MPL-2.0 |
| [Tree-sitter](https://github.com/tree-sitter/tree-sitter) | 0.26.12 | Native parser runtime | MIT |
| [tree-sitter-java](https://github.com/tree-sitter/tree-sitter-java) | 0.23.5 | Java grammar | MIT |
| [tree-sitter-c-sharp](https://github.com/tree-sitter/tree-sitter-c-sharp) | 0.23.5 | C# grammar | MIT |

BCA's annotated `v2.2.0` tag resolves to commit
`09fb2301528e12916b9b4514c01db814de8f0a74`. The worker consumes the unmodified
crates.io release. It does not copy BCA source into dircue's Go module or change
BCA's license. The prototype's own driver and worker code use dircue's MIT license.

The local Docker image includes dependency license/notice files, the Cargo lockfile,
and the complete unmodified BCA crate archive under
`/usr/share/dircue-structural/notices`. Any eventual distributed package must
retain applicable notices and satisfy BCA's MPL source-availability requirements;
that work belongs in the release packaging review. The existing production
release's dependency notices remain unchanged because this prototype is separate.

Tree-sitter grammars and their generated node identifiers must stay paired with
BCA's metric implementations. Upgrade BCA and its grammar pins together, rerun
syntax/metric fixtures, and compare corpus outputs before accepting an update.
A grammar with the same language name is not necessarily interchangeable.

The broader `xberg-io/tree-sitter-language-pack` is not included. This experiment
answers whether BCA's own parse can serve both consumers before introducing
another native runtime, grammar set, or parser-download mechanism.
