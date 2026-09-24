# Atlas Run: claude/rc-atlas — 38-repo parity atlas (#107)

## Run metadata

- **Branch**: claude/rc-atlas
- **Upstream merged**: origin/codex/1.0.0-map-candidate @ 84dfc27
- **dircue binary**: bin/dircue (dev build)
- **scc oracle**: 4.1.0
- **Linguist oracle**: 9.7.0 (docker image dircue-linguist:9.7.0)
- **Corpus**: 38 repos (corpus.json)

## Summary

### Linguist 9.7.0

- **355 language breakdowns matched, 0 mismatched** across all 38 repos (100% agreement)
- Command: `python3 tests/atlas/run.py --candidate bin/dircue --scc <scc> --cache <cache> --output <output> --all`
- Results: `tests/atlas/run.py` with `run_linguist` using `dircue-linguist:9.7.0` image

### scc 4.1.0 per-file parity

| Metric | Value |
|--------|-------|
| Counters identical | 261081 / 262575 files |
| Counter differences (grammar_selection_differs) | 1494 |
| dircue-bug | 0 |
| Unexplained counter mismatches | 0 |
| Unexplained one-sided files | 0 |

#### Counter-mismatch category

All 1494 counter mismatches are `grammar_selection_differs`: dircue selected a different scc grammar
than scc did for the same file. dircue picks the grammar via its Linguist language name (from enry
context-sensitive detection); scc picks by filename/extension registry. The grammar difference fully
explains all counter differences. No same-grammar counting bugs were found.

Grammar-pair breakdown:

| dircue grammar | scc grammar | Count |
|----------------|-------------|-------|
| C | C Header | 668 |
| TypeScript | JavaScript | 355 |
| Jinja | HTML | 240 |
| Perl | Raku | 199 |
| HCL | Terraform | 11 |
| TypeScript | Qt Translation Source | 5 |
| Ruby | Gemfile | 5 |
| JavaScript | JSX | 4 |
| Go Template | Smarty Template | 3 |
| Shell | Raku | 2 |
| PHP | Blade template | 1 |
| Shell | TemplateToolkit | 1 |

#### One-sided file categories (evidenced)

| Category | Count |
|----------|-------|
| dircue_only/scc_no_language | 374 |
| dircue_only/scc_skips_dotfiles | 3 |
| dircue_only/scc_skips_file | 23 |
| scc_only/dircue_binary | 10 |
| scc_only/dircue_file_too_large | 1 |
| scc_only/dircue_out_of_scope | 102427 |
| scc_only/dircue_unsupported_encoding | 6 |
| scc_only/dircue_unsupported_language | 1693 |

## Per-repo results

| Repo | Linguist | scc matched | scc mismatched | category |
|------|----------|-------------|----------------|----------|
| cobra | 2 langs matched | 26 | 0 | — |
| flask | 4 langs matched | 73 | 0 | — |
| ripgrep | 5 langs matched | 108 | 0 | — |
| helm-examples | 1 langs matched | 0 | 1 | grammar_selection_differs (HCL/Terraform) |
| linux | 25 langs matched | 69522 | 669 | grammar_selection_differs (C/C-Header) |
| express | 3 langs matched | 101 | 0 | — |
| apache-maven | 7 langs matched | 3203 | 0 | — |
| uv | 11 langs matched | 808 | 0 | — |
| typescript | 3 langs matched | 35069 | 355 | grammar_selection_differs (TypeScript/JavaScript) |
| roslyn | 15 langs matched | 22365 | 0 | — |
| aspnetcore | 19 langs matched | 12568 | 1 | grammar_selection_differs (C/C-Header) |
| spring-framework | 13 langs matched | 9321 | 2 | grammar_selection_differs (Go Template/Smarty Template) |
| rails | 7 langs matched | 3392 | 2 | grammar_selection_differs (Ruby/Gemfile) |
| laravel | 6 langs matched | 2386 | 1 | grammar_selection_differs (PHP/Blade template) |
| jq | 10 langs matched | 79 | 0 | — |
| oras | 5 langs matched | 295 | 0 | — |
| terraform-aws-vpc | 1 langs matched | 20 | 5 | grammar_selection_differs (HCL/Terraform) |
| serverless-examples | 13 langs matched | 411 | 0 | — |
| kotlin-koans | 2 langs matched | 108 | 0 | — |
| dotnet-samples | 19 langs matched | 3598 | 0 | — |
| functions-framework-python | 3 langs matched | 91 | 0 | — |
| aws-sam-java-rest | 2 langs matched | 32 | 0 | — |
| microservices-demo | 10 langs matched | 95 | 2 | grammar_selection_differs (Go Template/Smarty Template) |
| eshop | 6 langs matched | 646 | 0 | — |
| spring-petclinic | 5 langs matched | 64 | 0 | — |
| terraform-alias | 1 langs matched | 3 | 0 | — |
| kubernetes | 10 langs matched | 9803 | 0 | — |
| react | 7 langs matched | 3388 | 0 | — |
| django | 7 langs matched | 2507 | 240 | grammar_selection_differs (Jinja/HTML) |
| jellyfin | 4 langs matched | 2172 | 0 | — |
| bioperl | 4 langs matched | 501 | 199 | grammar_selection_differs (Perl/Raku) |
| nixpkgs | 67 langs matched | 46566 | 4 | grammar_selection_differs (Ruby/Gemfile, Shell/Raku) |
| terraform | 5 langs matched | 2010 | 4 | grammar_selection_differs (HCL/Terraform) |
| actions-toolkit | 6 langs matched | 139 | 0 | — |
| pnpm | 11 langs matched | 4604 | 0 | — |
| next | 15 langs matched | 23154 | 9 | grammar_selection_differs (TypeScript/Qt-Translation-Source, JavaScript/JSX) |
| prometheus | 12 langs matched | 1011 | 0 | — |
| traefik | 9 langs matched | 842 | 0 | — |

## Classifier design changes

### grammar_selection_differs (new category)

Per-file grammar attribution is now performed for every scc counter mismatch:
- dircue reports the scc grammar it used via the `grammar` field in its metrics JSON
- scc reports the grammar it used via the `Language` field in `--by-file --format json` output
- If the grammars differ, the mismatch is classified `grammar_selection_differs` (not a counting bug)
- If the grammars are the same and counts still differ, it becomes `dircue-bug`

This replaces the previous wildcard file-extension entries in `known-differences.json`. Those entries
were rejected because they could hide genuine same-grammar counting bugs.

### Shared materialized checkout (harness fix)

Previously, dircue read git objects (`--source git`) and scc read a materialized checkout. For repos
with `.gitattributes text=auto` (e.g. aspnetcore), this caused byte-count differences from CRLF
conversion that were unrelated to dircue's counting logic.

Fix: `run.py` now materializes HEAD once via `git checkout-index` and runs both dircue
(`--source directory`) and scc against the same directory. Both tools now see identical bytes.

### known-differences.json

Reduced from 33 entries to 2: only the specific per-file scc 4.1.0 raw-string literal
comment-overcounting entries for `src/RawStringLimitation.java` and `src/RawStringLimitation.cs`.
All extension-based entries are now handled by the `grammar_selection_differs` classifier.
