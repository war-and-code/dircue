# HTTP declaration feasibility experiment

Question: can dircue's existing BCA-owned parse support useful HTTP declaration evidence for Java/Spring, C#/ASP.NET Core and Python/FastAPI without parsing the file again?

This experiment is separate from the worker protocol and dircue CLI. It is not a release feature. It does not enumerate runtime endpoints, determine public reachability, inspect authentication, or assign quality or risk scores.

The Rust example creates one `big_code_analysis::Ast`, borrows its tree-sitter tree for a bounded traversal, then calls BCA metrics with the same `Ast`. The Python harness attaches a SHA-256 computed from the exact source bytes supplied to that process. No new dependencies, grammar versions, dependency downloads, builds of inspected projects, or application execution are required.

## Evidence contract under evaluation

Each observation includes a framework candidate, versioned rule, source byte/line span, lexical qualification with supporting import/assignment spans, optional simple literal route and HTTP method, and `declared` or `unresolved` status. Bytes use zero-based half-open ranges; lines are one-based tree-sitter positions. Source identity belongs to the outer harness envelope. `declared` describes a supported syntactic declaration, never an effective runtime endpoint or verified installed framework.

Java type/method mapping annotations stay separate. C# controller prefixes, inherited attributes and registration receivers are not resolved. FastAPI router construction, inclusion and prefixes are not combined. A declaration such as `@GetMapping("/items")` can therefore retain `/items` while explicitly stating that no type-level prefix was applied.

The probe recognizes only the spellings enumerated in its source. `complete` means that this bounded supported-spelling traversal finished, not that every HTTP API or endpoint in the source was discovered. Unknown imports, foreign or shadowed symbols, namespace-only C# imports, minimal-API receiver types, computed strings, route configuration tokens, unsupported string forms and syntax recovery remain unresolved. Foreign same-named annotations never acquire a confirmed HTTP method.

All Python decorators remain unresolved in the final experiment, including those with a matching constructor/import chain. Their literal route and supporting source spans are retained; a confirmed HTTP method is withheld. This avoids treating incomplete binding analysis as proof.

The experiment accepts at most 256 KiB of UTF-8 source and a clean relative display path. It limits syntax depth to 256, nodes to 100,000, qualification-context names to 256, retained observations to 128, and route text to 2,048 bytes. It counts omitted candidates and marks capped/recovered traversal partial. Limit refusals emit no JSON. These are experiment limits, not proposed release defaults.

## Reproduce

Use the repository's pinned Rust 1.94.0 toolchain and its existing dependency lockfile. On hosts where another rustc is earlier in PATH, set `RUSTC` to the Rust 1.94.0 executable as the existing packager does.

```sh
rustup run 1.94.0 cargo build --locked --release \
  --manifest-path prototypes/structural/worker/Cargo.toml \
  --example http_declarations
python3 prototypes/entrypoints/run.py \
  --binary prototypes/structural/worker/target/release/examples/http_declarations \
  --corpus /path/to/pinned/corpus --output /tmp/entrypoint-results.json
```

The corpus checks read exact Git objects from Spring Framework and ASP.NET Core. They do not run repository scripts. Their commit IDs, paths and expected declarations are in `run.py`; the result records source SHA-256 and source-license provenance. The third ecosystem uses small handwritten FastAPI fixtures, with no manifest or package installation.

The harness checks manually authored source expectations, repeatability excluding timing fields, exact source spans, one-parse API use, syntax recovery, source/path/depth refusal, and observation caps. Incidental per-process timings are diagnostic samples rather than a performance benchmark.

## Qualification limits to decide before production

Single-file lexical imports cannot prove an installed dependency's identity. C# namespace lookup and extension-method resolution need more information than a syntax tree provides. Python's dynamic imports, module aliases, rebinding forms beyond ordinary assignments/function definitions, monkey-patching and runtime decorator behavior need either more explicit unknown states or a deliberately narrower support contract. Even fully qualified Java annotation spelling does not prove which implementation was loaded or whether a runtime route exists. Java custom composed annotations, inheritance and wildcard imports are outside this probe. No claim should be broadened by guessing from a type, method or variable name.

A production design should distinguish declared annotation/attribute evidence from unqualified registration-call candidates. It should retain source identity and rule versions, preserve separate prefix declarations, disclose unsupported coverage, and keep this traversal opt-in. The experiment's evidence is intended to support that decision, not to pre-approve implementation.

Primary references: [BCA source API](https://github.com/dekobon/big-code-analysis), [Spring request mappings](https://docs.spring.io/spring-framework/reference/web/webmvc/mvc-controller/ann-requestmapping.html), [ASP.NET Core minimal API route handlers](https://learn.microsoft.com/en-us/aspnet/core/fundamentals/minimal-apis/route-handlers?view=aspnetcore-10.0), and [FastAPI routers and larger applications](https://fastapi.tiangolo.com/tutorial/bigger-applications/).

## Result and recommendation

**The shared-parse approach is feasible for bounded syntactic evidence. This prototype is not ready for production endpoint discovery.** The final run passed 20 source cases and three explicit source/path/depth refusals. Eleven declaration spans across the main Java, C# and Python fixtures were checked against manually specified source tokens. Every successful case computed BCA metrics with the same retained tree, reported one parser call, and passed source-byte/line-span checks. The final Rust example also passed clippy with warnings denied.

The pinned Spring sample produced five mapping annotations: four literal declarations and one unresolved marker without a route. The pinned ASP.NET controller produced two attribute candidates; the minimal-API sample produced twelve registration-call candidates. All fourteen C# observations remained unresolved because namespace/receiver binding was not established. No combined `/hotels/bookings` or grouped minimal-API path was invented. The real source samples are framework test/sample code, not a measurement of application or endpoint coverage across those repositories.

An initial Python constructor/import heuristic failed three adversarial cases: deletion of the receiver, rebinding in a loop and replacement of its `get` attribute. Those cases incorrectly received `declared` status. The initial inputs and observations are preserved in [the qualification audit](results/python-binding-audit-initial.json). The final fixtures require unresolved results, and the final contract withholds resolved receiver identity for every Python decorator. This is a deliberate reduction in claims, not an assertion that those difficult cases have been semantically analyzed.

[The final receipt](results/feasibility.json) records exact probe, binary, harness, lockfile and fixture hashes, pinned upstream commit/path provenance, expected-versus-observed results, caps and omissions. Full raw observations remain reproducible with `run.py`; the tracked receipt summarizes them to avoid repeating all 128 retained cap-case entries.

Incidental final-run samples spent about 2.67 ms on bounds checks plus evidence extraction for the Spring file, 0.15 ms for the small controller and 5.13 ms for the minimal-API file. These include multiple traversals of the same AST and an unoptimized ancestor-depth check; they exclude process startup, JSON input/output and hashing in the wrapper. The depth check dominates the larger samples. These are single diagnostic observations outside a controlled benchmark, not speed claims or estimates for whole repositories. A production implementation should track cursor depth during an existing traversal, measure the complete optional path, and preserve the untouched default path.

The smallest defensible production contract would expose opt-in, versioned **syntactic declaration evidence**, not endpoint enumeration: source identity, source spans, import/attribute/constructor evidence, optional literal fields, separate prefixes, explicit unresolved reasons and bounded coverage. It should distinguish a qualified annotation spelling from actual symbol binding. Java annotations could be the narrow first supported extractor; C# and Python candidates should retain their current uncertainty until stronger binding rules have independent counterexample coverage. No runtime or protocol integration is proposed by this experiment.
