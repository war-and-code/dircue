# Project mapping validation

Parser tests in `pkg/projects` cover .NET and JVM declarations, conditions, dynamic references, malformed manifests, bounded XML, and omitted credentials. Schema tests validate actual CLI reports and reject invalid version/field combinations.

```sh
go test ./pkg/projects ./schema
```

Set `DIRCUE_STRUCTURAL_WORKER` to a built native worker to also validate combined project/metrics/structural CLI reports against schema `1.2.0`.

`conformance.py` checks deterministic output, stable project IDs, exclusive file/byte attribution, and reference target state/path invariants on local checkouts. It does not execute build tools. The receipt contains counts and hashes, with no copied source files or absolute checkout paths.

```sh
go build -o .cache/dircue-project-check .
python3 tests/projects/conformance.py \
  --binary .cache/dircue-project-check \
  --output .cache/project-corpus.json \
  roslyn=/path/to/roslyn \
  aspnetcore=/path/to/aspnetcore \
  spring-framework=/path/to/spring-framework \
  apache-maven=/path/to/apache-maven
```

The scan uses `--source directory`, so recorded checkout commits identify provenance rather than asserting a clean working tree. The manifest-inventory hash covers manifest paths and their content hashes; the normalized-report hash covers output with its machine-local root removed. Binary hashes identify the precise executable used.

`results/corpus.json` is a measured implementation checkpoint. It proves the listed inventory invariants on those checkouts; it is not ground truth for compiler input membership, a successful build, or release performance. Malformed fixtures in Roslyn and Apache Maven produce diagnostics. Maven fixtures using unsupported model namespaces or XML encodings remain explicit omissions from declaration parsing. Refresh the receipt when changes alter the report contract or attribution.
