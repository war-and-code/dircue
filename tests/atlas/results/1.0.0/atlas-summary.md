# dircue Parity Atlas Summary

Repos evaluated: 38

## Linguist 9.7.0 parity

| Metric | Value |
|--------|-------|
| Repos | 38 |
| Languages matched | 355 |
| Languages mismatched | 0 |
| Agreement rate | 100.0% |

## scc 4.1.0 per-file parity

Counter identity: 261081/262575 (1494 documented grammar differences, 0 unexplained counter mismatches)

| Metric | Value |
|--------|-------|
| Repos | 38 |
| Counters identical (both tools) | 261081/262575 |
| Counter differences (documented) | 1494 |
| dircue-only (scc did not output) | 400 |
| scc-only (dircue outside scope) | 104137 |
| Union size | 367112 |
| Unexplained one-sided files | **0** |

### Counter-mismatch categories

- `grammar_selection_differs`: 1494

### One-sided file reasons (evidenced)

- `dircue_only/scc_no_language`: 374
- `dircue_only/scc_skips_dotfiles`: 3
- `dircue_only/scc_skips_file`: 23
- `scc_only/dircue_binary`: 10
- `scc_only/dircue_file_too_large`: 1
- `scc_only/dircue_out_of_scope`: 102427
- `scc_only/dircue_unsupported_encoding`: 6
- `scc_only/dircue_unsupported_language`: 1693

## Timings (wall seconds)

| Repo | Tool | dircue | oracle |
|------|------|--------|--------|
| actions-toolkit | linguist | 0.07s | 1.16s |
| actions-toolkit | scc | 0.05s | 0.02s |
| apache-maven | linguist | 0.60s | 11.27s |
| apache-maven | scc | 3.01s | 0.14s |
| aspnetcore | linguist | 0.88s | 19.81s |
| aspnetcore | scc | 1.56s | 0.22s |
| aws-sam-java-rest | linguist | 0.03s | 0.78s |
| aws-sam-java-rest | scc | 0.03s | 0.01s |
| bioperl | linguist | 0.20s | 3.62s |
| bioperl | scc | 0.14s | 0.03s |
| cobra | linguist | 0.04s | 1.16s |
| cobra | scc | 0.03s | 0.01s |
| django | linguist | 0.45s | 6.76s |
| django | scc | 0.87s | 0.08s |
| dotnet-samples | linguist | 0.46s | 10.21s |
| dotnet-samples | scc | 1.14s | 0.10s |
| eshop | linguist | 0.12s | 1.89s |
| eshop | scc | 0.11s | 0.02s |
| express | linguist | 0.05s | 7.05s |
| express | scc | 0.04s | 0.02s |
| flask | linguist | 0.06s | 0.88s |
| flask | scc | 0.04s | 0.02s |
| functions-framework-python | linguist | 0.04s | 1.08s |
| functions-framework-python | scc | 0.04s | 0.02s |
| helm-examples | linguist | 0.02s | 1.02s |
| helm-examples | scc | 0.02s | 0.01s |
| jellyfin | linguist | 0.19s | 4.09s |
| jellyfin | scc | 0.19s | 0.04s |
| jq | linguist | 0.06s | 1.16s |
| jq | scc | 0.04s | 0.02s |
| kotlin-koans | linguist | 0.03s | 0.82s |
| kotlin-koans | scc | 0.03s | 0.02s |
| kubernetes | linguist | 1.46s | 23.70s |
| kubernetes | scc | 2.44s | 0.40s |
| laravel | linguist | 0.20s | 4.07s |
| laravel | scc | 0.24s | 0.04s |
| linux | linguist | 8.93s | 171.30s |
| linux | scc | 9.63s | 3.50s |
| microservices-demo | linguist | 0.09s | 1.32s |
| microservices-demo | scc | 0.05s | 0.02s |
| next | linguist | 1.45s | 21.49s |
| next | scc | 4.58s | 0.44s |
| nixpkgs | linguist | 2.39s | 43.92s |
| nixpkgs | scc | 7.63s | 1.21s |
| oras | linguist | 0.07s | 1.40s |
| oras | scc | 0.06s | 0.02s |
| pnpm | linguist | 0.45s | 11.19s |
| pnpm | scc | 0.83s | 0.12s |
| prometheus | linguist | 0.17s | 2.98s |
| prometheus | scc | 0.15s | 0.04s |
| rails | linguist | 0.26s | 5.56s |
| rails | scc | 0.41s | 0.07s |
| react | linguist | 0.36s | 6.54s |
| react | scc | 0.57s | 0.09s |
| ripgrep | linguist | 0.08s | 1.29s |
| ripgrep | scc | 0.04s | 0.02s |
| roslyn | linguist | 2.35s | 52.36s |
| roslyn | scc | 3.57s | 0.44s |
| serverless-examples | linguist | 0.11s | 1.60s |
| serverless-examples | scc | 0.10s | 0.02s |
| spring-framework | linguist | 0.64s | 19.10s |
| spring-framework | scc | 1.69s | 0.16s |
| spring-petclinic | linguist | 0.06s | 1.05s |
| spring-petclinic | scc | 0.04s | 0.02s |
| terraform | linguist | 0.28s | 5.29s |
| terraform | scc | 0.57s | 0.08s |
| terraform-alias | linguist | 0.02s | 0.71s |
| terraform-alias | scc | 0.02s | 0.01s |
| terraform-aws-vpc | linguist | 0.04s | 0.82s |
| terraform-aws-vpc | scc | 0.03s | 0.02s |
| traefik | linguist | 0.22s | 3.08s |
| traefik | scc | 0.21s | 0.04s |
| typescript | linguist | 3.19s | 47.15s |
| typescript | scc | 4.08s | 0.54s |
| uv | linguist | 0.26s | 3.29s |
| uv | scc | 0.16s | 0.04s |

