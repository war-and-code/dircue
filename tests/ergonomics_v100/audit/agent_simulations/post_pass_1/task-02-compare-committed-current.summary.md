# Task 02: Compare committed contents versus current files

**Status.** COMPLETE WITH QUALIFICATIONS

**CLI calls.** 8 total: 2 discovery/help calls, 4 profile-generation calls, and 2 comparison attempts.

**First intended operation success?** NO — the first comparison at step 5 used `/dev/fd` inputs and failed; the error taught the regular-file requirement, and step 8 succeeded.

**What worked.** Help clearly identified committed HEAD as the Git-root default and `--source directory` as the current-file override. The committed report contained one Python file (19 bytes), while the current-directory report contained three Go/JavaScript files (56 bytes). After regular report files were supplied, `compare` returned a complete comparison with a changed summary.

**What was confusing.** `compare` requires readable regular files, so an attempt to avoid intermediates with pipe-backed `/dev/fd` paths failed even though both descriptors were readable. The error was actionable. The result deliberately qualifies language deltas as `observed_only`/`unavailable` because the minimal aggregate profiles lack provider and selection provenance; it does not overclaim source attribution.

**Round-trips to completion.** 8, including discovery and the failed comparison.
