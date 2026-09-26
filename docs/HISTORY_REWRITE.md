# History rewrite, 2026-09-26

On 2026-09-26, before the repository went public and before 1.0.0, the maintainer rewrote the repository's history to remove bulky evidence files (#139).

## What changed

- 177 archived evidence files were removed from every commit. They came from `tests/enry-performance/results`, `tests/stress/results`, `tests/profiling/results` and `tests/release/results`, and totalled 156 MiB unpacked, mostly tarballs and audit dumps. No test or CI job reads them.
- The files are assets of the [`evidence-archive-1`](https://github.com/war-and-code/dircue/releases/tag/evidence-archive-1) prerelease. `tests/receipts/evidence-archive.json` records each file's path, SHA-256 and size, and `make fetch-receipts` restores them.
- Every branch and every `v0.x` tag was rewritten with `git filter-repo --invert-paths`. No commits were dropped: every commit keeps its message, author, date and remaining files. Commits that only touched removed files are now empty.
- The packed repository shrank from 94.8 MiB to about 14 MiB.

## Commit IDs

Every commit ID changed. `docs/history-rewrite-commit-map.txt` maps each old ID to its new one (`old new`, one per line). Commit messages that quoted abbreviated IDs were updated by `git filter-repo`.

Receipts and review notes committed before the rewrite still quote the old IDs; they record what was true then. To translate an old ID, look it up in the map, for example:

```sh
grep '^c976461' docs/history-rewrite-commit-map.txt
```

Scripts that resolve a commit in this repository, and the evidence documents that tell readers which commit to inspect, use the new IDs. That covers the label freeze commits in `tests/map_corpus` and the build-provenance source commit.

GitHub releases stayed attached to their tags. Tag commits:

| Tag | Old commit | New commit |
| --- | --- | --- |
| `v0.1.0` | `b932a2b04f30` | `5e3ec766b6ed` |
| `v0.2.0` | `a77cc5a3a172` | `4424db323d25` |
| `v0.3.0` | `2e8cc42fbd28` | `a18a160c3f8c` |
| `v0.4.0` | `a27301535bad` | `cab5d5ba0ebd` |
| `v0.4.0-rc.1` | `a27301535bad` | `cab5d5ba0ebd` |
| `v0.5.0` | `d75bc1ad2094` | `1b9139585b65` |
| `v0.6.0` | `bf211aeb0ba2` | `aa0492135186` |
| `v0.6.1` | `c9ca1778e8da` | `4b370c570478` |
| `v0.7.0` | `0d1f80bd410c` | `e66dc227ccba` |
| `v0.8.0` | `121027f44f01` | `29b57bd3c126` |
| `v0.9.0` | `59e435b1d85e` | `84ca7eb932ec` |

## If you had a clone

Clones made before the rewrite hold the old history.
- **Re-clone** rather than pulling.
- **Never push** from an old clone. That would bring the removed files and old commits back.
- **Uncommitted work:** export it as patches, or copy the files, into a fresh clone.

Pull request pages on GitHub still show their original commits. Those references are read-only and are not fetched by clones. The maintainer keeps a complete bundle of the pre-rewrite history.
