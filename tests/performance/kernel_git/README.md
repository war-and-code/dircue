# Kernel Git read performance, 0.9.0

This directory records the source-bound measurements used to close the Git
read performance work for 0.9.0. The machine-readable samples and identities
are in [`receipt.json`](receipt.json). Large reports, profiles, binaries, and
the kernel fixture stay in the ignored `.cache/kernel-investigation` tree.

## Result

The final implementation is commit `0d9153fa26ca9b5c87227cc2c1391b3fc9d1167f`.
It uses at most three independent go-git storage lanes, selected from the
requested worker count. Each concurrent lane retains at most two pack readers.
Single-worker and inspection paths use one lane.

The comparison used Linux commit
`fe2ec83746e501645709761605c2464a44fd2929`, tree
`e1e29ac112aa72e141114477802e40ee085e9350`, on a case-sensitive APFS volume.
The Git and directory populations contained the same 96,039 paths. At 16
workers, three final-source repetitions produced these medians:

| Scan | Git | Directory | Ratio |
|---|---:|---:|---:|
| Metrics | 10.07 s | 6.99 s | 1.44x |
| Languages | 8.13 s | 6.87 s | 1.18x |

Metrics RSS ranged from 789,381,120 to 815,824,896 bytes. Language RSS ranged
from 862,420,992 to 932,397,056 bytes. These are observed ranges on this host,
not memory limits.

The final integrated binary measured 1-to-16-worker scaling of 4.05x, compared
with 4.69x for directory mode. Its medians were 44.22 seconds at one worker,
13.59 at four, 10.59 at eight, and 10.92 at sixteen. The full three-repetition,
order-balanced 1/4/8/16 samples are retained in the receipt. This scaling matrix
used the retained host bare repository and measured a 10.92-second median at 16
workers; the separate case-sensitive ratio series measured 10.07 seconds. A
separate descriptor sample observed 13 numeric descriptors and six pack descriptors at peak. The
six pack descriptors match the aggregate retained-reader bound of three lanes
times two readers. The descriptor sampler ran separately from timing medians.

Every final language report had SHA-256
`7eb5d08d74665a4b4fe82300682c9c7ada1a5092ebe1f7e796e5a2879999472a`.
Git and directory metrics content was equal after excluding source metadata;
its canonical content SHA-256 was
`84aa675513a7b0b5b1de906cf842291f959677f8e14eaab25947d6fd5b2ee55a`.

## Mechanism evidence

An instrumented replay control and seek candidate differed only in the delta
base seek block. On the full kernel, replay read 7,300,115,861,697 bytes from
delta base readers and took 217.20 seconds. Seek read 17,837,975,641 bytes and
took 19.37 seconds. Both produced the same normalized result hash. Inflated
bytes and physical pack bytes differed by less than 0.03%, isolating repeated
in-memory base replay as the removed work.

A deterministic 20-pair synthetic control produced the same output in all 40
runs. Replay read 2,147,516,416 base bytes per run; seek read 8,388,608 bytes.
The final three-lane counter snapshot ended with zero active delta readers and
the same normalized kernel result hash.

## Commands

The principal commands were:

```text
/usr/bin/time -l $DIRCUE analyze metrics --source git --rev fe2ec83746e501645709761605c2464a44fd2929 --workers $WORKERS --json $KERNEL_GIT
/usr/bin/time -l $DIRCUE --source git --rev fe2ec83746e501645709761605c2464a44fd2929 --workers $WORKERS --json $KERNEL_GIT
/usr/bin/time -l $DIRCUE analyze metrics --source directory --workers $WORKERS --json $KERNEL_TREE
```

Runs used order-balanced worker sequences and retained every JSON result, wall,
user and system time, maximum RSS, and SHA-256. No filesystem caches were
cleared and no operating-system tuning was applied.

## Filesystem qualifications

The ordinary host filesystem is case-insensitive. A direct `git archive`
materialization there contained 96,026 paths because 13 tracked Linux paths
collide by case. That diagnostic comparison is retained but is not the
population-equivalent acceptance result. The case-sensitive volume path list
and the Git tree path list both hash to
`3d1d44f4579fe51775f7da17cbf14dff10ba0b96532c1d2f52ac0391a3bc4d26`.

Docker Desktop remains a separate bottleneck. A read-only bind-mounted,
4,100-file pinned fixture took 10.35 seconds in the Linux container and 0.20
seconds natively; normalized outputs were identical. A full-kernel bind scan
was canceled after 147.12 seconds and produced no report, so no completion-time
claim is made for that run.
