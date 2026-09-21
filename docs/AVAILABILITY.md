# Source-availability evidence

Added in 0.7.0.

```sh
dircue analyze availability --json /checkout
dircue analyze availability --source directory --json /checkout
dircue analyze all --availability --declarations --json /checkout
```

Availability profiling looks for acquisition boundaries that can make source appear absent: Git LFS pointers, Gitlinks, submodule declarations, and supported local sparse-checkout metadata. It does not fetch repositories, initialize submodules, hydrate LFS objects, run filters, or execute checkout code.

This is explicit bounded content inspection. Ordinary language runs and metadata-only discovery do not enable it automatically.

## Different evidence, different conclusions

- A valid LFS pointer is a small metadata file referring to an object. Its recorded object size is not the size of content inspected by dircue.
- Pointer-like text that fails supported validation stays distinct from a valid pointer. Noncanonical representations are labeled separately from canonical ones.
- An LFS attribute records a configured filter. Nonpointer content with that attribute does not prove successful hydration or correspondence to a remote object.
- A Gitlink records a path and commit in the selected tree. It does not contain the submodule's files.
- `.gitmodules` records declarations. A declared submodule path need not have a matching Gitlink. URLs and arbitrary section names are withheld.
- Sparse-checkout metadata is evidence about the local checkout. It does not make a committed Git tree sparse.

With Git source, the report observes the chosen tree and does not use checkout sparsity to explain that tree's contents. With directory source, supported `.git` metadata is inspected within the directory boundary. External Git-directory files, unsupported index formats, and incomplete metadata are qualified rather than followed or interpreted optimistically. Git index v2/v3 sparse indications are supported; arbitrary sparse patterns are not evaluated into an inferred missing-file list.

## Missing references

When a combined run also requests project or declaration profiling, availability can qualify supported missing targets against directly observed boundaries. A target beneath a Gitlink, for example, may point into separately acquired content.

This correlation is evidence about a boundary, not proof that fetching it would resolve the reference or make a build succeed. Unsupported metadata, capped pointer inspection, or incomplete inventories prevent broad absence claims. Standalone availability does not implicitly parse every project manifest.

## Costs and bounds

Each pointer inspection reads at most 1,024 bytes. The default aggregate pointer budget is 16 MiB, allocated deterministically by path. Evidence retention, correlation work, and output have separate caps; full source-file sizes do not become content-read totals. Supported `.gitmodules` parsing and local checkout metadata use separately bounded reads.

`--max-file-bytes` omits larger source-file contents while preserving applicable inventory evidence. Other module limits and the usual tree-size limit still apply. These are analysis bounds, not a hard process-memory limit.

Inspect `coverage.selected_inventory_complete`, checkout-metadata coverage, counts, diagnostics, and omission reasons together. A report can preserve useful boundary observations while remaining partial. Reported paths remain relative to the original source root.

Availability uses aggregate schema 1.6.0. Existing language-statistics exclusions and legacy LFS handling remain unchanged.
