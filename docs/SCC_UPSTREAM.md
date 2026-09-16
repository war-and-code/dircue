# scc upstream integration

`dircue` uses the unmodified [`processor` package from scc v4.1.0](https://github.com/boyter/scc/tree/v4.1.0/processor) for optional code metrics. The Go module and checksum files pin that release. [Third-party notices](../THIRD_PARTY_NOTICES.md) include its MIT license and the licenses of linked dependencies.

## What runs

The adapter accepts a language already identified by dircue and a complete UTF-8 file. It calls `processor.CountStats` to count physical lines, code lines, comment lines, blank lines, and complexity tokens. It uses scc's default counting behavior, including the generic counter. Complexity is a lexical estimate; it is not a count of control-flow paths obtained from an AST.

Directory traversal, Git revision selection, exclusions, byte limits, and report aggregation remain in dircue. Git content is read through the [maintained go-git dependency](../third_party/README.md), which includes fixes for packed-object streaming and file-handle cleanup. The adapter does not invoke scc's walker, load repository configuration, guess another language, calculate cost estimates, or use Git history. This release integrates the counting engine, not every feature of the standalone scc command.

`Grammar` uses a fixed table of ungrouped Linguist names. An unknown name produces an explicit unsupported result, even when its filename extension looks familiar. This preserves the distinction between languages sharing extensions, such as Objective-C and MATLAB.

## Initialization and concurrency

The first supported file enables scc’s lazy loading mode and initializes its lookup maps once. Each grammar is then built once on first use, through the same upstream feature builder used by eager initialization. Calls read completed features concurrently; a scan encountering a new language builds only that additional grammar. The adapter does not set GOGC, call `ConfigureGc`, or invoke scc's worker loop. Other code in the process must not mutate scc's package globals after counting starts.

Importing the scc package still initializes its static language database and small lookup tables at process startup. Language-only performance comparisons must include that cost; lazy feature construction does not eliminate all startup work.

Counting keeps the caller's source bytes unchanged. Cancellation is checked before and after counting and at line boundaries. A single long line can delay cancellation until the counter reaches its end. The scanner supplies bounded file contents and reports files it cannot count; a per-file byte limit is not a process memory limit.

## Known counting limits

The adapter rejects invalid UTF-8 and files containing NUL bytes. It does not transcode UTF-16 or legacy encodings. A file can therefore have a recognized language without usable code metrics.

scc's lexical rules do not fully model every language construct. In v4.1.0, a Java text block or C# raw string containing a standalone double quote followed by a line beginning with `//` can cause that latter line to be counted as a comment. Agreement with scc confirms integration behavior, not language-parser accuracy. These limitations do not alter dircue's language statistics.

## Updating scc

1. Select a released scc version and read its changes to `processor.CountStats`, initialization, globals, binary handling, callbacks, and language definitions.
2. Update the module requirement with `go get github.com/boyter/scc/v4/processor@vX.Y.Z`, then run `go mod tidy`. Review transitive version changes as well.
3. Update `EngineVersion` and review `pkg/codemetrics/grammars.go`. Add mappings deliberately; do not substitute extension guessing for unsupported languages. Update the inventory below when coverage changes.
4. Run `go test -race ./pkg/codemetrics ./pkg/scanner ./internal/cli` and the metrics differential harness against the same standalone scc release. Retain Java and C# raw-string cases, unsupported encodings, cancellation, exclusions, and Git-versus-directory checks. Record known upstream differences rather than weakening checks to hide them.
5. Repeat the existing language-only comparison and metrics resource measurements. Report both the engine version and measured scope with results.
6. Run `python3 scripts/notices.py` and review its output before packaging binaries.

Changes to a counting grammar can change metrics between releases without changing the source files. Compare reports with their recorded engine versions in view.

## Supported language names

The adapter currently maps 204 ungrouped Linguist names. This inventory describes available counting grammars, not default file inclusion. Source scope still excludes generated, vendored, documentation, and data files according to dircue's language inclusion policy. Text scope expands that selection; it does not make every format countable.

| Linguist language | scc grammar |
|---|---|
| ABAP | ABAP |
| ABNF | ABNF |
| AL | AL |
| APL | APL |
| ASP.NET | ASP.NET |
| ATS | ATS |
| ActionScript | ActionScript |
| Ada | Ada |
| Agda | Agda |
| Alloy | Alloy |
| Apex | Apex |
| AppleScript | AppleScript |
| AsciiDoc | AsciiDoc |
| Assembly | Assembly |
| Astro | Astro |
| Bicep | Bicep |
| Blueprint | Blueprint |
| Boo | Boo |
| Brainfuck | Brainfuck |
| Bru | Bru |
| BuildStream | BuildStream |
| C | C |
| C# | C# |
| C++ | C++ |
| C3 | C3 |
| CMake | CMake |
| COBOL | COBOL |
| CSS | CSS |
| CSV | CSV |
| Cairo | Cairo |
| Cangjie | Cangjie |
| Cap'n Proto | Cap'n Proto |
| Ceylon | Ceylon |
| Chapel | Chapel |
| Circom | Circom |
| Clojure | Clojure |
| CodeQL | CodeQL |
| CoffeeScript | CoffeeScript |
| ColdFusion | ColdFusion |
| Common Lisp | Lisp |
| Creole | Creole |
| Crystal | Crystal |
| Cuda | Cuda |
| Cypher | Cypher |
| Cython | Cython |
| D | D |
| D2 | D2 |
| DM | DM |
| Dart | Dart |
| Dhall | Dhall |
| Dockerfile | Dockerfile |
| Elixir | Elixir |
| Elm | Elm |
| Emacs Lisp | Emacs Lisp |
| Erlang | Erlang |
| F# | F# |
| F* | F* |
| Factor | Factor |
| Fennel | Fennel |
| Forth | Forth |
| Fortran | FORTRAN Legacy |
| Fortran Free Form | Fortran Modern |
| Futhark | Futhark |
| GDScript | GDScript |
| GLSL | GLSL |
| GN | GN |
| Game Maker Language | Game Maker Language |
| Gherkin | Gherkin Specification |
| Gleam | Gleam |
| Go | Go |
| Go Template | Go Template |
| Gradle | Gradle |
| GraphQL | GraphQL |
| Groovy | Groovy |
| HCL | HCL |
| HTML | HTML |
| HTML+ERB | Ruby HTML |
| HTML+Razor | Razor |
| Haml | HAML |
| Handlebars | Handlebars |
| Hare | Hare |
| Haskell | Haskell |
| Haxe | Haxe |
| IDL | IDL |
| INI | INI |
| Idris | Idris |
| Isabelle | Isabelle |
| JCL | JCL |
| JSON | JSON |
| JSON5 | JSON5 |
| Janet | Janet |
| Java | Java |
| JavaScript | JavaScript |
| Jinja | Jinja |
| Jsonnet | Jsonnet |
| Julia | Julia |
| Just | Just |
| Kotlin | Kotlin |
| LOLCODE | LOLCODE |
| Lean | Lean |
| Less | LESS |
| LiveScript | LiveScript |
| Lua | Lua |
| Luau | Luau |
| MATLAB | MATLAB |
| MDX | MDX |
| MLIR | MLIR |
| MQL4 | MQL4 |
| MQL5 | MQL5 |
| Makefile | Makefile |
| Mako | Mako |
| Markdown | Markdown |
| Max | Max |
| Meson | Meson |
| Metal | Metal |
| Mojo | Mojo |
| Monkey C | Monkey C |
| Move | Move |
| Mustache | Mustache |
| Nim | Nim |
| Nix | Nix |
| Nushell | Nushell |
| OCaml | OCaml |
| Objective-C | Objective C |
| Objective-C++ | Objective C++ |
| Odin | Odin |
| OpenQASM | OpenQASM |
| Org | Org |
| Oz | Oz |
| PHP | PHP |
| Pascal | Pascal |
| Perl | Perl |
| Pkl | Pkl |
| Pony | Pony |
| PostScript | PostScript |
| PowerShell | Powershell |
| Processing | Processing |
| Prolog | Prolog |
| Protocol Buffer | Protocol Buffers |
| Puppet | Puppet |
| PureScript | PureScript |
| Python | Python |
| Q# | Q# |
| QML | QML |
| R | R |
| RAML | RAML |
| Racket | Racket |
| Raku | Raku |
| ReScript | ReScript |
| Rebol | Rebol |
| Redscript | Redscript |
| Rich Text Format | Rich Text Format |
| Ruby | Ruby |
| Rust | Rust |
| SAS | SAS |
| SQL | SQL |
| SRecode Template | SRecode Template |
| SVG | SVG |
| Sass | Sass |
| Scala | Scala |
| Scheme | Scheme |
| Shell | Shell |
| Sieve | Sieve |
| Slang | Slang |
| Slint | Slint |
| Smalltalk | Smalltalk |
| Smarty | Smarty Template |
| Snakemake | Snakemake |
| Solidity | Solidity |
| Stan | Stan |
| Standard ML | Standard ML (SML) |
| Stata | Stata |
| Stylus | Stylus |
| Svelte | Svelte |
| Swift | Swift |
| SystemVerilog | SystemVerilog |
| TOML | TOML |
| TSX | TypeScript |
| Tact | Tact |
| Tcl | TCL |
| TeX | TeX |
| Teal | Teal |
| Text | Plain Text |
| Textile | Textile |
| Thrift | Thrift |
| TypeScript | TypeScript |
| TypeSpec | TypeSpec |
| Typst | Typst |
| V | V |
| VHDL | VHDL |
| Vala | Vala |
| Verilog | Verilog |
| Visual Basic .NET | Visual Basic |
| Vue | Vue |
| Wren | Wren |
| XML | XML |
| XSLT | Extensible Stylesheet Language Transformations |
| Xtend | Xtend |
| YAML | YAML |
| Zig | Zig |
| hoon | hoon |
| jq | jq |
| reStructuredText | ReStructuredText |
| sed | sed |
