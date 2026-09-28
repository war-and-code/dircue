# Static project-reference graphs

Added in 0.4.0.

```sh
dircue analyze graph --source directory --json /path/to/checkout
dircue analyze all --graph --discovery --json /path/to/repo
```

Graph analysis reuses the static project inventory. It adds a `graph` field alongside `projects` under profile schema `1.3.0`. Existing project commands and output stay unchanged unless graph analysis is requested.

The first graph kind is `dotnet-project-reference`: direct `ProjectReference` declarations in parsed .NET project manifests. Project IDs identify manifests, so two projects in the same directory remain separate nodes. Maven aggregation, solution membership, imports, package dependencies, and runtime calls are different relationships and are not mixed into this graph.

## Which edges count

Graph calculations include an edge only when its declaration is unconditional and its target is a present, parsed, unambiguous .NET project within the selected inventory. The report preserves conditional, unresolved, missing, external, and unrecognized targets as separate edge observations. They do not contribute to definite cycle or degree calculations.

Certainty and target resolution are separate fields. A conditional reference can also have a missing target. Conditions and original declaration values remain available for a consumer that can evaluate them in the correct build context. Dircue does not execute MSBuild or restore packages.

The resulting graph is a declaration graph, not evaluated build membership. Shared `.props` or `.targets` files can declare additional project references; assigning those to effective projects requires import evaluation. Such observations remain excluded and qualified rather than silently becoming definite edges.

## Results

- `nodes` include fan-in, fan-out, component identity, and cycle membership. Degrees count distinct included neighbors, not repeated declarations.
- `edges` retain source evidence, certainty, resolution, and `included`.
- `components` are weakly connected components, including isolated projects.
- `cycles` are strongly connected components containing multiple projects or a self-reference. They identify cyclic groups, not every possible cycle path.
- `coverage` records included, conditional, unresolved, and excluded observations on separate axes. Those counts must not be blindly summed.
- `diagnostics`, `input_status`, and source identity preserve the limitations of the underlying project inventory.

Results have deterministic ordering. Component and cyclic-group IDs use their lexically first project ID. Graph traversal is iterative so long chains do not require recursive traversal depth proportional to repository size.

`not_applicable` means a complete project inventory contained no supported .NET project nodes. `skipped` means the required inventory was unavailable or skipped. `partial` preserves missing or unresolved evidence. `complete` is limited to the stated declaration scope; it does not establish that a build succeeds or that all effective project references are known.

Cycles and high fan-in can help choose where to investigate architecture or build behavior. They are observations, not automatic defects or a universal quality grade. The [project guide](PROJECTS.md) describes parser limits, and the [roadmap](https://github.com/war-and-code/dircue/issues/41) records broader relationship and hotspot proposals.
