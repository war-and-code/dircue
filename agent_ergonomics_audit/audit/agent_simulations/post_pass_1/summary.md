# Pass 1 Binary-Only Simulation Summary

All eight tasks completed against the immutable `1.0.0-dev` binary. The simulation used only executable help/output and the supplied fixture/report. It did not read source, README, prior audit material, or baseline transcripts; it used no network, did not execute examined code or a planned command, and left the fixture unchanged.

| Task | Status | CLI calls | Help/discovery | First intended operation? | Notes |
|---|---:|---:|---:|---:|---|
| 01 Lightweight directory profile | COMPLETE | 2 | 1 | YES | Current files returned Go/JavaScript legacy JSON |
| 02 Committed vs current comparison | COMPLETE, qualified | 8 | 2 | NO | `/dev/fd` rejected; regular saved reports worked; language claims remained observed-only |
| 03 Offline contracts/schemas/exits | COMPLETE | 3 | 0 | YES | CLI contract, guide, and profile schema were all in-tool |
| 04 Code metrics, no worker | COMPLETE | 1 | 0 | YES | Built-in scc metrics; no structural worker |
| 05 `--jsno` recovery | COMPLETE | 2 | 0 | NO | Empty stdout, exit 1, exact `--json` hint, then success |
| 06 Saved-report plan | COMPLETE | 1 | 0 | YES | Plan was inert, `executable:false`, with revalidation requirements |
| 07 Capability descriptor/schema | COMPLETE, limit | 3 | 1 | YES | Offline schema present; no built-in validator command exposed |
| 08 Literal `analyze`/`compare` directories | COMPLETE | 2 | 0 | YES | Documented `./name` disambiguation worked |

- **Total CLI calls:** 22
- **Help/discovery calls:** 4
- **Median round-trips:** 2
- **Tasks completed:** 8/8
- **First intended operation succeeded:** 6/8
- **Tasks stuck:** 0/8

The strongest surfaces were self-documentation and recovery. Global help gave exact routes for CLI metadata, the offline guide, and schemas; the typo handler named both the corrected flag and applicable help; planning explicitly prevented accidental execution. The main friction was `compare` requiring regular files even when readable file descriptors were supplied. Its error taught the requirement, but the extra regeneration/retry inflated Task 2 to eight calls. The comparison itself was appropriately cautious about missing provenance.

Capture limits: transcript stdout/stderr is capped at 4 KiB per invocation. Full raw bytes for oversized capability/schema/comparison outputs were not retained after the original console captures, so the transcript uses explicit truncation markers and does not reconstruct missing bytes. Five repetitive JSON stdout bodies were whitespace-compacted during manual packaging while preserving their data. All argv vectors, working directories, environment overlays, timestamps, elapsed times, exit codes, and diagnostics are retained.

This exercise is not a controlled speed comparison. Task 2 adds an actual saved-report `compare` operation, a failed `/dev/fd` attempt, and recovery, whereas the referenced baseline count of 16 only included two source-profile calls. The 22-versus-16 totals therefore show different work, not a round-trip regression or improvement; no productivity speedup is claimed.

The exercised binary is bound by SHA-256 in `summary.json` to source `2bcd134`.
It predates the final schema constraints and framework-help catalog disclosure;
final-source regression checks and compatibility receipts cover those changes.
