# Independent CLI investigation before design review

Binary: `.cache/release080/installed/dircue` (0.8.0), target release commit supplied by parent: 121027f. Rubric version: 1.0.0. No implementation, README, other-agent findings, or audit drafts were read before the eight tasks. Initial shared root/analyze help is counted under task 1, not repeated across subsequent tasks. Full stdout/stderr, argv, cwd, timing, and status are in per-task JSONL; Formal copies use pass 1; originals retain pass 0 to record collection before bootstrap. The original fixtures and transcripts were retained under ignored `.cache/v100/independent`; these formal copies preserve their observations. Only the fixture has an isolated local Git commit needed to compare HEAD with current files; no production Git changes or builds.

| Task | Status | Calls | First intended operation succeeded? | Finding |
|---|---|---:|---|---|
| Light directory profile | Complete | 4 | Yes | Required root, analyze, and all help before the actual operation; default all is language+filename hints only. |
| Current files vs HEAD | Complete | 2 | Yes | Root help is explicit. HEAD shows deleted Python; directory source shows current Go/JavaScript. |
| Offline outputs, schemas, exit meanings | Partial | 1 | No | Capabilities describes eight planner modules; no complete CLI/schema/exit contract is advertised. |
| Metrics without worker | Complete | 2 | Yes | metrics help plus invocation works. Capabilities confirms external_process:false. |
| Mistyped --jsno recovery | Complete | 2 | No | Error is only `Error: unknown flag: --jsno`; recovery depends on remembered root help. |
| Planning saved report | Complete | 2 | Yes | plan help plus module-selected invocation works; output is an inert argv template. Base light aggregate lacks source provenance, retained explicitly as unavailable. |
| Capabilities schema validation/help | Partial | 1 | No | Help has no schema or validation route and advertises irrelevant inherited scan flags. |
| Paths named analyze/compare | Complete | 2 | Yes | `-- analyze` and `./compare` both classify the intended directory. No route is documented in root examples because there are none. |

6/8 complete, 2 partial/stuck, median 2 calls, 5/8 first intended operations successful. 16 total CLI calls in the canonical pass. Every command took 16–63 ms on tiny fixtures; these are command-response observations, not a benchmark.

