# Ordinary command performance

These macOS ARM64 measurements compare released dircue 0.3.0 with the frozen development candidate that adds discovery, project graphs, package evidence, declarative rules and optional function evidence. None of those optional modules is enabled here. This checks the existing language and project commands separately from the new capabilities.

The first window retained 100 measured invocations and 20 warmups across five cases. Every stdout byte matched the released binary, stderr was empty and exit status was zero. The existing 209-case compatibility suite covers behavior more broadly; these measurements do not replace it.

One result needed investigation: Roslyn directory project analysis was 6.7% slower in the first window, across all five pairs. A separate, narrower window did not reproduce that difference. Both windows remain in the record. There is no demonstrated persistent default-path regression here, but this small diagnostic does not establish identical performance or a universal speed guarantee.

## First window

Times are medians of five subprocess measurements in seconds. Each round alternates binary order and language/project mode order. Positive changes mean the candidate took longer or used more memory. RSS changes compare medians; the receipt also retains every sample and maximum.

| Corpus and source | Command | Released time | Candidate time | Time change | RSS change |
| --- | --- | ---: | ---: | ---: | ---: |
| Cobra, Git | Languages | 0.0338 | 0.0335 | −1.0% | −1.5% |
| Cobra, Git | All with projects | 0.0336 | 0.0340 | +1.3% | +4.1% |
| Spring, Git | Languages | 1.5891 | 1.5760 | −0.8% | +3.1% |
| Spring, Git | All with projects | 1.6231 | 1.5860 | −2.3% | +1.8% |
| Roslyn, directory | Languages | 3.0881 | 3.0791 | −0.3% | −4.2% |
| Roslyn, directory | All with projects | 3.0998 | 3.3079 | +6.7% | +1.0% |
| Roslyn, Git | Languages | 6.3176 | 6.2567 | −1.0% | +0.4% |
| Roslyn, Git | All with projects | 9.2639 | 9.1939 | −0.8% | −1.3% |
| XML plus .NET, directory | Languages | 0.0293 | 0.0303 | +3.1% | −11.0% |
| XML plus .NET, directory | All with projects | 0.0555 | 0.0562 | +1.1% | +4.7% |

The XML fixture contains 128 real XML files of 16 MiB each, exactly 2 GiB, plus a small C# source and project file. These XML data files are excluded from the default language statistics; this is not a full-content XML parsing benchmark. One XML language pair was 10.1% slower, approximately milliseconds at this scale. That sample was retained. The lower XML language RSS is an observation, not a demonstrated memory optimization.

The source populations are Cobra 66 files / 700,442 bytes, Spring 11,347 / 60,747,942, Roslyn 35,220 / 455,563,738 and mixed XML/.NET 130 / 2,147,483,835. Public corpus commits are pinned in `../corpus.json`. Directory content hashes and Git file/blob inventories were checked before and after; no duplicate large corpus was generated.

## Roslyn follow-up

The first window's project command used 3.08–3.12 seconds for the released binary and 3.14–3.37 seconds for the candidate. Total user/system CPU work was similar: about 6.5 / 14.6 CPU seconds for the release and 6.6 / 14.5 for the candidate. RSS medians differed by about 0.94 MiB. No page faults were reported.

The follow-up ran only the identical Roslyn directory project command. It rotated four lanes over five rounds: released binary, the earlier function-capable development binary, and two lanes using exactly the same current candidate executable and arguments.

| Lane | Median seconds | Change against release in this window |
| --- | ---: | ---: |
| Released 0.3.0 | 3.0952 | — |
| Earlier development candidate (`7c950ff5`) | 3.1753 | +2.6% |
| Current candidate A (`20ea257d`) | 3.1031 | +0.3% |
| Identical current candidate B (`20ea257d`) | 3.1108 | +0.5% |

The released binary itself had a 3.3320-second sample in this window; candidate B had a 3.2532-second sample. CPU totals stayed similar. Recorded context-switch counts do not establish a cause for the elapsed-time difference. Scheduling or other host variation is plausible, but unproven. The identical-binary lanes are a limited control, not proof that all earlier differences were noise. We made no runtime change or third measurement attempt to improve the result.

## Reproduce and verify

The measurement scripts require macOS `/usr/bin/time -l`, the existing corpus/fixtures, and the exact locally retained binaries. They do not download dependencies, execute repository build scripts or change machine tuning. Build/test/benchmark work by the other project agents was paused for each timing window; light reading and documentation work could continue, and unrelated host background activity was uncontrolled. Filesystem caches were warm. No CPU or RAM limit was enforced.

The release binary SHA256 is `ff0d723411657a61dc4385c84fc011c1392ce7696066fc13f1c663b1c3967ade`; the candidate is `20ea257d856d2805fadc8b0ce7488ff15768b3ac23f164f0d101cff332f24d8e`. The candidate deliberately embeds version `0.3.0` for byte-for-byte output comparison; it is a development build, not the published release. Its complete historical build-input manifest is embedded in the receipt, along with the identity of the separately verified build provenance. That does not imply the current worktree still contains those exact inputs.

From the repository root, choose fresh output paths:

```sh
python3 tests/performance/default_paths/benchmark.py prepare \
  --baseline .cache/release-v030/archive-audit/extracted/darwin_arm64/dircue \
  --candidate .cache/next-sprint/dircue-rules-optimized \
  --build-inputs tests/compatibility_next/results/rules-optimized-build-inputs.json.gz \
  --fixtures .cache/staged-analysis/fixtures --corpus-root .cache/corpus \
  --output .cache/default-paths-prepared.json.gz

python3 tests/performance/default_paths/benchmark.py measure \
  --prepared .cache/default-paths-prepared.json.gz \
  --output .cache/default-paths-results/macos-arm64.json.gz \
  --environment-note 'Describe concurrent activity and measurement conditions here.'

python3 tests/performance/default_paths/isolate.py \
  --receipt .cache/default-paths-results/macos-arm64.json.gz \
  --intermediate .cache/next-sprint/dircue-functions \
  --output .cache/default-paths-results/roslyn-isolation-macos-arm64.json.gz \
  --environment-note 'Describe the separate follow-up conditions here.'
```

Both measured modes use `--json --source MODE --workers 8 --tree-size 1000000 PATH`; project analysis prefixes `analyze all --projects`. The receipt retains the exact commands, execution order, every output digest, full macOS resource diagnostics and compressed complete JSON output for each case/mode. Wall time includes the subprocess and time wrapper, excluding output parsing and corpus verification. RSS is the CLI process maximum, excluding the Python harness.

Verify the retained samples without running a binary:

```sh
python3 tests/performance/default_paths/verify.py tests/performance/default_paths/results/macos-arm64.json.gz
python3 tests/performance/default_paths/verify.py tests/performance/default_paths/results/roslyn-isolation-macos-arm64.json.gz
```

The verifier checks all 120 measured outputs against the captured payloads, empty stderr, successful exit codes, exact command scope, calculated medians and paired deltas. Every sample is retained; neither window filters outliers.
