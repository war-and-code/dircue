# Fixture and reference provenance

- Oracle: [github-linguist 9.7.0](https://rubygems.org/gems/github-linguist/versions/9.7.0), released August 26, 2026; latest stable verified September 7, 2026.
- Official source: [v9.7.0 CLI](https://github.com/github-linguist/linguist/blob/v9.7.0/bin/github-linguist).
- Gem SHA-256: `39a5e077e946029ef0ddbdc1344a53802455f4920f8c758d2a6b114d963d7734`, verified against the [RubyGems release API](https://rubygems.org/api/v2/rubygems/github-linguist/versions/9.7.0.json) and checked during image build.
- Ruby: `3.3.9-bookworm`, multi-platform image digest `sha256:9bf63765d224b035214b4e72170ba89dc6ca4cfbbbf151bf12e1c50e4ecb0e25`.
- Non-default runtime gems are version-pinned in the Dockerfile; bundled gems come from the pinned base. Each report records the complete installed gem inventory. Debian build tooling uses the image's configured package repositories; image identity is recorded, since OS package resolutions are not bit-for-bit frozen.
- Synthetic fixture generator: `run.py` in this directory, versioned with the project. The report records its SHA-256, every generated fixture's content/path hash, and each repository's HEAD.
- Git fixture author, email, initial branch, timestamps, and commit messages are fixed. Global/system Git configuration is disabled during generation.
- Reference invocation: `github-linguist [flags] /fixtures/<case>/<target>` inside the image, using a read-only mount and `--network=none`.
- Candidate invocation: the exact compiled binary whose hash is in the report, with equivalent flags and fixture content on the host.

Use the commands in [README.md](README.md) to regenerate reports, selecting fresh output paths to preserve existing evidence. Review the differences; reference outputs must never be edited to hide a mismatch. For performance measurements, use the separate performance harness, which runs both executables in the same environment.

## Broad sample corpus

`samples.py` uses the official Linguist `v9.7.0` tag at Git object `e0c78d62c42abae6122235d8e68a7aa43eef89da`. Archive URL: `https://codeload.github.com/github-linguist/linguist/tar.gz/refs/tags/v9.7.0`; SHA-256 `e7b85d06f5e61a810303b8d2e03fc199760525c079fef4dd6f8b7c86342234d9`. Only regular files below `samples/` are extracted, with path traversal rejected. The archive hash is required even for user-supplied cached downloads. Each sample result records its relative path and byte size; the archive hash identifies the exact input corpus. Source sample licenses remain upstream; the corpus is not vendored into Dircue.
