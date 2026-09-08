# Generated conformance matrix

Reference: github-linguist 9.7.0. Synthetic requirements are scoped below; this is not a claim of universal language compatibility.

| Category | MUST tested | Passing | Divergent (verified extension) | Failing | Exact-match score |
|---|---:|---:|---:|---:|---:|
| attributes | 70 | 70 | 0 | 0 | 100.0% |
| classification | 15 | 15 | 0 | 0 | 100.0% |
| cli-options | 10 | 10 | 0 | 0 | 100.0% |
| dotnet | 5 | 5 | 0 | 0 | 100.0% |
| encoding | 5 | 5 | 0 | 0 | 100.0% |
| files | 5 | 5 | 0 | 0 | 100.0% |
| flat-extension | 34 | 33 | 1 | 0 | 97.1% |
| flat-single-files | 66 | 57 | 9 | 0 | 86.4% |
| git | 35 | 30 | 5 | 0 | 85.7% |
| java | 5 | 5 | 0 | 0 | 100.0% |
| paths | 5 | 5 | 0 | 0 | 100.0% |
| revision | 3 | 3 | 0 | 0 | 100.0% |
| selection | 50 | 50 | 0 | 0 | 100.0% |
| single-file-magic | 41 | 41 | 0 | 0 | 100.0% |
| single-file-read-scope | 35 | 35 | 0 | 0 | 100.0% |
| single-files | 36 | 35 | 1 | 0 | 97.2% |

## Failures


## Verified intentional extensions

- subdirectory/json-breakdown: DISC-003: reference rejects subdirectories; candidate matches the committed subtree oracle
- subdirectory/json: DISC-003: reference rejects subdirectories; candidate matches the committed subtree oracle
- subdirectory/text: DISC-003: reference rejects subdirectories; candidate matches the committed subtree oracle
- subdirectory/text-breakdown: DISC-003: reference rejects subdirectories; candidate matches the committed subtree oracle
- subdirectory/short-flags: DISC-003: reference rejects subdirectories; candidate matches the committed subtree oracle
- symlink/file-json: DISC-006: explicit single-file symlink is refused with no output; normal file counterpart is covered
- single-file-limits-flat/over-limit-generated.js/file-json: DISC-007: Git-free single files >1 MiB use bounded 128 KiB content; candidate must match actual Git LazyBlob oracle for identical file bytes
- single-file-limits-flat/over-limit-generated.js/file-text: DISC-007: Git-free single files >1 MiB use bounded 128 KiB content; candidate must match actual Git LazyBlob oracle for identical file bytes
- single-file-limits-flat/over-limit-generated.js/file-strategies: DISC-007: Git-free single files >1 MiB use bounded 128 KiB content; candidate must match actual Git LazyBlob oracle for identical file bytes
- single-file-limits-flat/over-limit-late-nul.cs/file-json: DISC-007: Git-free single files >1 MiB use bounded 128 KiB content; candidate must match actual Git LazyBlob oracle for identical file bytes
- single-file-limits-flat/over-limit-late-nul.cs/file-text: DISC-007: Git-free single files >1 MiB use bounded 128 KiB content; candidate must match actual Git LazyBlob oracle for identical file bytes
- single-file-limits-flat/over-limit-late-nul.cs/file-strategies: DISC-007: Git-free single files >1 MiB use bounded 128 KiB content; candidate must match actual Git LazyBlob oracle for identical file bytes
- single-file-limits-flat/over-limit-nul-edge.cs/file-json: DISC-007: Git-free single files >1 MiB use bounded 128 KiB content; candidate must match actual Git LazyBlob oracle for identical file bytes
- single-file-limits-flat/over-limit-nul-edge.cs/file-text: DISC-007: Git-free single files >1 MiB use bounded 128 KiB content; candidate must match actual Git LazyBlob oracle for identical file bytes
- single-file-limits-flat/over-limit-nul-edge.cs/file-strategies: DISC-007: Git-free single files >1 MiB use bounded 128 KiB content; candidate must match actual Git LazyBlob oracle for identical file bytes
- attrs-quoted/flat-extension: DISC-004: flat mode applies Git-standard quoted patterns; equivalent portable-pattern reference verified
