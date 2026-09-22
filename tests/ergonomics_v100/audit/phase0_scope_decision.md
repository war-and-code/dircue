# Pass 1 scope

Target: dircue, starting at released commit `121027f44f014ac8d0a003a451a04b080a5ffe88`. Full audit, implementation, re-score, regression testing, and fresh-context simulation. Existing `main` is used in the existing release worktree; no branch or sibling workspace is created. The original checkout and its unrelated commits remain untouched.

## Contracts

Preserve successful existing CLI invocations, legacy Linguist JSON and exit 0/1 semantics, source selection, analysis populations, evidence and omission distinctions. Improvements to failure diagnostics and help are deliberate surface changes, not byte-equivalence claims. Do not automatically correct and execute guesses, enable heavier modules, execute inspected code, contact services, or add telemetry. Keep ordinary directory use and general-purpose positioning. Existing planner capabilities JSON remains unchanged; deeper self-documentation requires explicit options. No release or repository visibility change is part of this pass.

## Method

Use GPT-6 Astra with high reasoning for inventory, independent simulation/review, profiling, and implementation. These are independent agents of one model family; do not claim cross-model triangulation. The existing conversation and fresh invocations supply task evidence; skip cross-project session mining to avoid collecting unrelated material. Reserve write ownership through the root coordinator and serialize shared-source edits. Deferred work belongs in GitHub Issues, with a concise evidence handoff here rather than a new roadmap.

The installed skill preflight found GNU timeout and flock absent. Existing Python subprocess timeout and fcntl file locks provide the audit equivalents; no machine-wide packages are installed or OS settings changed. Helper adaptation is a routine implementation choice within the requested autonomous work, not presumed user approval from silence. The original checks for git, jq, node, awk, find and sed passed. Native-helper and released-binary help checks passed; see preflight.json. Audit runners use argv arrays, bounded subprocess execution, and serialized writes.

The upstream bootstrap scripts ran in order: scaffold, discover, skill inventory. Discovery inferred the worktree directory basename as a Go binary name; corrected to dircue based on main.go and the released executable. No helper skills are missing; UBS binary is absent, so native tests/vet and independent review replace its optional pass.

The user subsequently requested installation of the missing utilities. After the
baseline profiling window ended, Homebrew installed flock 0.4.0 and coreutils
9.12. The original preflight then passed; its complete result is retained in
`preflight-after-install.json`. No shell startup files or system tuning changed.
The portable Python audit helpers remain usable; the earlier adaptation record
describes what happened before installation, not a continuing missing dependency.

The user later requested a draft pull request for 1.0.0 and a pause for external
adversarial review. That instruction supersedes the initial no-new-branch scope:
the existing worktree now uses `codex/1.0.0-preparation`. The draft must not be
merged or published as a release during this pass. Existing Astra agents finish
their assignments; subsequent substantial subagent assignments use GPT-5.6 Sol
at the user's request, with root retaining final review.

Profiling happens before optimization in a separate measurement lane. No concurrent builds/tests during timing windows. Retain raw samples and limitations. Every accepted optimization needs a scored measured hotspot, a one-lever change, golden equivalence, and matched before/after evidence. Ergonomics and optimization changes receive separate commits.
