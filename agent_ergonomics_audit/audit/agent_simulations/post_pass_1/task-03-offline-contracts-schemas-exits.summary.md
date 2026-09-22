# Task 03: Discover offline output contracts, schemas, and exit meanings from the binary only

**Status.** COMPLETE

**CLI calls.** 3 intended-operation calls; no task-local help call was needed because global help named all three routes exactly.

**First intended operation success?** YES (step 1).

**What worked.** `capabilities --cli --json` exposed the command grammar, restrictions, output contracts, schema-resource export argv, source-selection behavior, and the exit dictionary: 0 is successful execution (including qualified or differing results), while 1 is a handled CLI/input/analysis/cancellation/output error with diagnostics on stderr. `capabilities --guide` explained workflow semantics, and `capabilities --schema profile --json` returned an offline bundled Draft 2020-12 schema.

**What was confusing.** The CLI capability document and profile schema are very large. They are machine-readable but awkward for direct human inspection; transcript stdout is capped per the simulator's 4 KiB evidence limit.

**Round-trips to completion.** 3.
