# Packfile descriptor-cache adversarial review

Review date: 2026-09-21
Baseline commit: `121027f44f014ac8d0a003a451a04b080a5ffe88`
Candidate branch: `codex/1.0.0-preparation`

## Verdict

No reproducible correctness finding remains in the bounded packfile descriptor-cache change. The implementation is suitable for a draft PR after the candidate performance run is attached. This review does not establish a performance win: the frozen baseline has 20 measured processes per lane, while the candidate A/B run has not started.

## Invariants checked

- `pkg/scanner/git.go:227` configures `MaxOpenDescriptors: 8` only on the scan-owned primary filesystem storage. The upstream alternate lookup at `third_party/go-git/storage/filesystem/object.go:369-371` still constructs an option-free `ObjectStorage`, preserving uncached alternates.
- `pkg/scanner/git.go:412-420` retains the per-snapshot object mutex around packed-object lookup and the complete lazy-reader lifetime. Tree walking and size lookup finish before jobs are sent at `pkg/scanner/git.go:449-469`, so the earlier unguarded size phase does not overlap worker reads.
- `pkg/scanner/scanner.go:394-530` keeps the producer and every worker in the same wait group and closes `results` only after they exit. `Scan` therefore cannot run `snapshot.close()` at `pkg/scanner/scanner.go:262-267` while a worker still owns a lazy reader, including cancellation and fail-fast paths.
- `pkg/scanner/git.go:216-223` closes the bounded storage on every failed snapshot initialization. Successful `Scan` and `Inspect` calls transfer ownership to `gitSnapshot` and close it once; `gitSnapshot.close` clears the stored closer before invoking it, making repeated close calls idempotent.
- `third_party/go-git/storage/filesystem/object.go:260-291` bounds the primary cache with an eight-slot ring and closes an evicted pack before replacement. If eviction close fails, `object.go:238-243` closes the newly opened, unowned pack and returns the original eviction error. The cache cannot exceed capacity on that path.
- Large packed objects remain independent of cached pack lifetime: the cached pack returns an `FSObject`, whose reader reopens its own pack descriptor. The greater-than-capacity regression retains the first lazy blob, evicts its originating cached pack, and then reads identical bytes.
- Linked-worktree scans use the repository filesystem wrapper retained from `PlainOpenWithOptions`, while snapshot cleanup owns the reopened bounded storage. The fixture compares reports for both the main and linked worktrees.
- `go.mod:55` replaces `github.com/go-git/go-git/v5` with `./third_party/go-git`. The maintained patch, fork tree, and `PROVENANCE.json` agree under the updater's full verification.

## Verification

Passed from the candidate worktree:

```text
go test ./pkg/scanner -count=1
go test -race ./pkg/scanner -run 'TestGitPackCacheEvictionRetainsLazyReaderAndReport|TestPackedDeltaRepositoryConcurrentWorkers' -count=1
(cd third_party/go-git && go test ./storage/filesystem ./plumbing/format/packfile)
python3 third_party/update_go_git.py --check
git diff --check
```

The focused lifecycle fixture exercises success, discovery-only, explanation, availability, inspect success, inspect size failure, failed snapshot initialization, and cancellation repeatedly while checking process descriptor counts. The maintained-fork regression injects an eviction close error and proves that both the evicted descriptor and failed replacement descriptor are closed exactly once.

## Draft-PR boundary

Do not describe the hotspot as improved until the interleaved same-host candidate measurements are complete. Compare the candidate against the retained 20-sample baseline and follow the baseline's stated threshold: investigate changes above 10% with repeated interleaved runs. This is a performance-evidence requirement, not a correctness defect in the implementation.

## Subsequent measurement evidence

The review above was completed before timing. The subsequent six-lane interleaved A/B, exact 22-lane equivalence, allocation reprofile and acceptance rationale are now attached in [OPTIMIZATION.md](OPTIMIZATION.md). Runtime activation is commit `3b9c1c5`; the independent maintained-fork cleanup prerequisite is `df1b8cb`. This addendum does not retroactively change the review's original scope.
