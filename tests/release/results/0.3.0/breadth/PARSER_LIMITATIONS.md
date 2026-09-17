# Investigated parser limitations

The [diagnostic receipt](parser-limitations.json) examines representative
partial results against the pinned worker. In jq, macro-based loops such as
`jv_object_keys_foreach(...) { ... }` are not understood as loop syntax by the
unexpanded C grammar. In TypeScript's compiler source, a named tuple label
`symbol:` causes recovery in the pinned TypeScript grammar. Another sampled
TypeScript test file embeds JSX test cases inside an outer `.ts` file.

Minimal diagnostic inputs reproduce the first two cases:

```c
void f() { each(x, y) { } }
```

```typescript
type T = [symbol: Symbol];
```

Replacing the macro call with `for (;;)` or the tuple label with `value:` removes
recovery in the diagnostic probes. Those replacements isolate parser behavior;
they are not equivalent programs or transformations that dircue performs.
Partial results do not establish that the original source is invalid. These
findings explain selected cases, not every recovery in the corpus.
