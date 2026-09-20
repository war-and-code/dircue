# 0.4 candidate: existing commands and optional registry inventory

The frozen 0.4 candidate matched released 0.3.0 output in all 209 compatibility cases. Five paired measurements per ordinary command showed median time changes from −1.5% to +3.6% on this host. These measurements did not reproduce a material default-path regression. They do not establish identical performance on every repository or platform.

The comparison binary was built from source commit `3e8e8ad52a78caf047f8901e0986ecb01adff5f7`, with its reported version deliberately overridden to `0.3.0`. Its SHA256 is `0a0587135b7f906db7c2add58623dd4a3908e6d236b8b95cee79fd5941f99214`. The [build receipt](../../compatibility_next/results/v040-final-build-inputs.json.gz) records the command and all 280 local Go, module, and embedded inputs. The actual release-version binary is a separate artifact.

The [compatibility receipt](../../compatibility_next/results/v040-final-macos-arm64.json.gz) compares stdout, stderr, and exit status exactly, including 30 native structural cases across 20 languages. The [current-worker receipt](../../compatibility_next/results/v040-final-native.json.gz) adds 67 default protocol comparisons, 21 opt-in requests, 11 counterexamples, and an actual function-evidence CLI run. Worker protocol comparison excludes the documented timing field. The worker binary hash identifies the executable tested; observed local source hashes alone do not prove its build provenance.

## Ordinary commands

These measurements enable no new optional modules. Languages uses `--json`; projects uses `analyze all --projects --json`. Both use eight workers, an explicit Git or directory source, and a tree limit of 1,000,000. Each mode has one warmup followed by five pairs, alternating binary order. Output bytes matched in every sample.

| Corpus / source | Language median change | Projects median change |
| --- | ---: | ---: |
| Cobra / Git | +2.29% | −0.28% |
| Spring Framework / Git | −0.52% | −1.49% |
| Roslyn / directory | +0.38% | −0.59% |
| Roslyn / Git | +0.34% | −0.63% |
| XML and .NET / directory | +3.60% | +2.66% |

Median peak RSS changes ranged from −2.9% to +4.4%. The earlier Roslyn directory projects increase in [the preceding campaign](../default_paths/README.md) did not recur here; that earlier evidence remains unchanged.

The XML fixture contains 128 real 16 MiB XML files, exactly 2 GiB, beside a small C# project. XML is excluded from default language statistics. This measures handling of a large directory containing excluded data, not parsing all 2 GiB of XML. The other pinned inventories contain 66 files for Cobra, 11,347 for Spring, and 35,220 for Roslyn. Receipts retain commit/tree identities, inventory digests, full output captures, every sample, and resource diagnostics.

## Optional registry inventory

Three commands ran on the same candidate: projects, projects with `--registries`, and `analyze registries`. Each had one warmup and five rounds with rotating lane order. The combined report preserved all existing fields; its schema version changed from 1.2.0 to 1.3.0 and it added the registry module. Combined and standalone registry results matched exactly.

| Directory | Config bytes read | Projects median | With registries | Registries alone |
| --- | ---: | ---: | ---: | ---: |
| Cobra | 0 | 23.80 ms | 23.80 ms | 20.16 ms |
| Roslyn | 3,514 | 3.278 s | 3.304 s | 1.085 s |
| XML and .NET | 0 | 55.68 ms | 56.44 ms | 19.63 ms |
| Small synthetic project | 363 | 26.32 ms | 26.42 ms | 19.63 ms |

Adding registry inventory increased median time by 0.02%–1.36% in these samples. Individual pairs varied, particularly for short commands. Standalone registry inventory performs less work and does not replace the language or project report. Read counts refer only to configuration content, not filesystem enumeration, attributes, or other modules.

The XML combined-mode samples used **8 MiB more median peak RSS**, from 65.83 to 73.83 MiB (+12.2%), with all five paired increases. This remains an observed optional-mode cost even though there were no matching configuration reads. Source review found no registry reader or configuration-content retention for those XML filenames; the measurements do not identify the cause. No claim of zero allocation overhead follows from zero configuration reads.

## Reproducing and checking the evidence

Run from the repository root on macOS. Prepare the pinned corpora and existing XML fixture described in [the preceding campaign](../default_paths/README.md); preparation verifies their expected identities. Use a new output directory for a fresh run rather than overwriting these historical results.

```sh
python3 tests/discovery/build_candidate.py \
  --output .cache/v040-check/dircue \
  --inputs .cache/v040-check/build-inputs.json
python3 - <<'PYTHON'
import gzip, hashlib, json
from pathlib import Path
root = Path('.cache/v040-check')
record = json.loads((root / 'build-inputs.json').read_text())
record['candidate_sha256'] = hashlib.sha256((root / 'dircue').read_bytes()).hexdigest()
(root / 'build-inputs.json.gz').write_bytes(
    gzip.compress((json.dumps(record, indent=2) + '\n').encode(), mtime=0))
PYTHON
python3 tests/performance/v040_candidate/benchmark.py prepare \
  --baseline .cache/release-v030/archive-audit/extracted/darwin_arm64/dircue \
  --candidate .cache/v040-check/dircue \
  --build-inputs .cache/v040-check/build-inputs.json.gz \
  --fixtures .cache/staged-analysis/fixtures \
  --corpus-root .cache/corpus --output .cache/v040-check/default-prepared.json.gz
python3 tests/performance/v040_candidate/registries.py prepare \
  --default-prepared .cache/v040-check/default-prepared.json.gz \
  --fixture .cache/v040-check/registry-fixture \
  --output .cache/v040-check/registry-prepared.json.gz
python3 tests/performance/v040_candidate/benchmark.py measure \
  --prepared .cache/v040-check/default-prepared.json.gz \
  --output .cache/v040-check/results/default-macos-arm64.json.gz \
  --environment-note 'Record actual host activity and resource constraints here.'
python3 tests/performance/v040_candidate/registries.py measure \
  --prepared .cache/v040-check/registry-prepared.json.gz \
  --output .cache/v040-check/results/registries-macos-arm64.json.gz \
  --environment-note 'Record actual host activity and resource constraints here.'
```

To verify retained receipts without running the binaries:

```sh
python3 tests/performance/v040_candidate/verify.py \
  --default tests/performance/v040_candidate/results/default-macos-arm64.json.gz \
  --registries tests/performance/v040_candidate/results/registries-macos-arm64.json.gz
python3 tests/compatibility_next/verify.py \
  tests/compatibility_next/results/v040-final-macos-arm64.json.gz
```

Verification covers 100 ordinary and 60 registry timing samples plus 32 warmups, checking hashes, outputs, resource values, medians, and registry scope. Measurements ran on an Apple M1 Max with 32 GiB RAM, warm caches, and no enforced resource limits. Other project agents paused tests and builds; light documentation work and unrelated host activity were not controlled. Five pairs on one host are diagnostic evidence, not a universal speed guarantee. The added Rust research example does not enter the measured Go input set or alter the tested worker binary.
