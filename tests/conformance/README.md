# Linguist differential conformance

This suite compares Dircue with the `github-linguist` 9.7.0 CLI installed from the official RubyGems release. It checks byte totals, two-decimal percentage strings, file inclusion, attributes, Git state, exit status, and CLI text formatting.

```sh
docker build -t dircue-linguist:9.7.0 -f tests/conformance/Dockerfile tests/conformance
python3 tests/conformance/run.py
```

The runner builds the current `dircue` checkout as a native binary, generates deterministic local repositories, and executes the reference in an isolated container with read-only fixture mounts and networking disabled. Fixture content is never executed. Python 3, Git, Go, and Docker are required on the host; there are no Python package dependencies.

Use `--binary /absolute/path/dircue` to test an existing binary. `--case attrs-nested` selects a fixture (repeatable); `--mode json-breakdown` limits the mode for fast diagnosis (also repeatable). `--output /path/report.json` selects report location. `--keep-fixtures /path/new-directory` retains generated inputs for investigation; use an empty directory.

The JSON report retains both commands' stdout, stderr, and exit code for each case, plus generator/binary hashes, Git revision, fixture hashes, image identity, and installed gem versions. The neighboring Markdown file contains the coverage matrix. Unexpected mismatches cause a nonzero exit. Intentional subdirectory and quoted-pattern flat extensions are XFAIL against Ruby's direct response. They must also match separate Ruby invocations that test their required behavior; otherwise they FAIL.

JSON object key order and file-array order are normalized. Fields, types, language sizes, and percentage strings must match exactly; text is compared byte-for-byte. For invocations rejected by both tools, error wording is excluded from the contract.

Flat-directory cases compare `dircue` on files with `.git` removed against Linguist's output on the same committed content. They validate the explicitly requested extension; they do not assert that Linguist itself accepts non-Git directories.

The commands above use the runner's default paths under `results/` and can overwrite existing reports. Use `--output` with a fresh path when preserving recorded evidence. To update the reference, change the pinned image and gem versions/checksum, generate a separate report, and resolve every new mismatch before changing the claimed compatibility scope. See [PROVENANCE.md](PROVENANCE.md), [COVERAGE.md](COVERAGE.md), and [DISCREPANCIES.md](DISCREPANCIES.md).

## Upstream classifier samples

```sh
python3 tests/conformance/samples.py --require-match
```

This test downloads the hash-pinned official Linguist 9.7.0 source archive and reads all regular files under `samples/`. An optional `--archive` reuses a downloaded archive after verifying its hash. It compares Ruby `Linguist::FileBlob#language`, official unmodified `enry.GetLanguage` in a separate temporary module, and `scanner.DetectLanguage` on identical complete file content. Corpus code is never executed. The helper under `classifier/` is test infrastructure and is not part of the shipped CLI. Use `--output` with a fresh path to preserve the existing sample report.

The report includes every sample, reference label/type, both Go labels, version/hash provenance, and an exact-match count. It checks every ordered token sequence against Ruby using SHA-256 of NUL-separated tokens and the token count. The unmodified baseline must have no module replacement and must match the expected Go module checksum. The Markdown report lists every mismatch.

With `--require-match`, any candidate label or ordered-token mismatch exits nonzero; use this flag for CI acceptance. Without it, a completed comparison exits zero even if classifications differ. Reference execution errors always exit nonzero. Ruby's output determines the expected label; sample folder names identify the corpus layout. This comparison covers the pinned samples, excluding repository inclusion policy, prefix limits, and runtime performance.
