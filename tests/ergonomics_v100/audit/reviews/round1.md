# Round 1 independent CLI/schema review — NOT CLEAN

Reviewed the v1.0 preparation changes under `internal/cli`, `schema`, and the related documentation against baseline `121027f44f014ac8d0a003a451a04b080a5ffe88`. Runtime probes used the frozen candidate at `.cache/v100/dircue-ergo-candidate`, SHA-256 `a94125486bb43ba1e80ac18e2661e5571aa8b285afddda504fee88c8ba1b1db6`. Scanner/cache optimization and third-party changes were excluded as assigned.

## [P2] Initialize Cobra's version flag before catalog traversal

`describeCLI` initializes the default help command and each command's help flag, but it never calls `InitDefaultVersionFlag` before collecting root flags (`internal/cli/capability_cli.go:74-105`). Cobra creates the root version flag lazily, so the new machine-readable catalog omits a supported, advertised option: `dircue --version` exits 0 and root `--help` lists `-v, --version`, while the root record from `capabilities --cli --json` has no `version` flag. This violates the catalog's core promise to describe the actual CLI grammar and prevents an agent relying on the catalog from discovering the installed binary's version surface.

Reproduction:

```sh
.cache/v100/dircue-ergo-candidate --version
# exit 0; stdout: dircue 0.8.0

.cache/v100/dircue-ergo-candidate capabilities --cli --json |
  jq -e '.commands[] | select(.path == ["dircue"]) |
         any(.flags[]; .name == "version")'
# prints false; jq exits 1
```

Call `root.InitDefaultVersionFlag()` before traversal, then make the catalog/help alignment assertion bidirectional so every flag shown in each command's flag sections must also appear in the catalog. The current test only checks that catalog flags appear in help, which cannot detect omissions.

## Other reviewed contracts

No additional actionable issue was found in the assigned scope. I checked selector conflicts including explicit boolean false, rejected inherited scan flags, unchanged default `capabilities --json` bytes, writer and short-writer failures, planning prevalidation parity with `pkg/planning`, path/command disambiguation, terminal-safe reflected values, output-contract/schema-resource references, offline compound-schema identities and reference closure, exact string-only `"NaN"` acceptance, and the guide/help/documentation claims. The coordinator's already identified generic pflag fallback secret reflection and C1/Cf plain-plan rendering defects are intentionally not duplicated here.
