# Task 07: Discover capability descriptor schema, validation route, and applicable help

**Status.** COMPLETE WITH LIMIT

**CLI calls.** 3 total: 1 discovery/help call and 2 intended-operation calls.

**First intended operation success?** YES (step 2).

Applicable help documented the mutually exclusive `--cli`, `--guide`, and `--schema` views. The default JSON descriptor exposed planner modules and versions, while `--schema capabilities --json` exported its offline Draft 2020-12 schema. No built-in `validate` command is exposed; validation therefore means passing the descriptor and exported schema to a caller-selected Draft 2020-12 validator.

**Round-trips to completion.** 3.
