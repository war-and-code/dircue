# Top-K model attribution

Parts of `model.bend` and `PROOF.bend` adapt the order relation,
evidence-carrying comparison and insertion-sort proof from
[Bend's insertion-sort demo](https://github.com/bendlang/bend/tree/981899d6b2fb2c109ed83545cffed9d20910a025/demos/proof_insertion_sort).

Copyright 2026 HigherOrderCO. These modified files are licensed under the
Apache License, Version 2.0; the complete upstream license is retained in
[LICENSE-APACHE-2.0](LICENSE-APACHE-2.0).

The dircue modifications add descending metric values with ascending identity
ties, bounded insertion, histogram-independent top-K laws, and partition
merging. This code is an optional research model. It is not linked into or
distributed inside dircue's executable or native worker.
