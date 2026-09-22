# Post-pass tiebreak rationale

This assessment was made blind from `post-tiebreak-input.jsonl`, rubric 1.0.0, and current source/help/test evidence. No other score or baseline files were consulted, and the frozen pre-fix binary was not used.

The five shorthand flags are genuine registered aliases. `-j` scores 750 because the intent corpus tests `--jsno` and `--jason` and requires the exact `did you mean --json?` correction. `-b`, `-r`, `-s`, and `-t` score 500: each works as an alias, but none has the surface-specific misspelling/response evidence required for 750.

`SystemRoot`, `SIGINT`, and `SIGTERM` score 0 for regression resistance. Their source behavior is clear, but no test pins the Windows taskkill lookup/fallback or sends either signal to a process. Generic capability-schema and ordinary process tests would not catch removal of these individual behaviors.
