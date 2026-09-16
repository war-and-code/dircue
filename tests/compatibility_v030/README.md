# Compatibility with dircue 0.2.0

`run.py` compares stdout, stderr, and exit codes byte-for-byte for existing CLI invocations. It creates isolated directory and Git fixtures, including committed, staged, unstaged, untracked, and revision-specific contents. No network access is needed.

```sh
python3 tests/compatibility_v030/run.py \
  --baseline bin/dircue \
  --candidate .cache/v030/dircue \
  --output tests/compatibility_v030/results.json
```

The default baseline check requires the verified macOS arm64 v0.2.0 release binary (SHA-256 `62c884a82eacb8153cc57c6c3cc980ec1fba35391b02ab1690e5e850a145d059`). The baseline and candidate run against the same fixture paths. Reports preserve raw outputs for every comparison.

The matrix covers language analysis, ordinary `analyze all`, scc metrics and optional per-file results, byte limits, text/source scopes, `.gitattributes`, explicit directory mode, committed Git trees, `--rev`, empty inputs, unborn repositories, and invalid arguments. Version/help output and the list of available analyses are recorded separately because the new opt-in commands intentionally extend them; they are not silently normalized in ordinary comparisons.

The report records executable hashes, the repository commit, worktree status, and a digest of current Go build inputs. The source digest is a snapshot taken at test time; a release build receipt must independently bind final source inputs to the executable. The corpus does not establish universal behavioral equivalence.
