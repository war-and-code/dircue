# Independent result binary provenance

The candidate binary recorded in `independent_results.json` is reproducible from
source commit `bac6eb1e7e356ed5f62dfd245a3b54b6715d24e6` (the pre-fix source).
Using Go `go1.26.6 darwin/arm64`, export that commit's source and run:

```sh
env CGO_ENABLED=1 go build -trimpath -buildvcs=false -o /tmp/dircue-candidate .
```

The resulting binary has SHA-256
`59faf4a4a70309eb944658a717936cca0c800712f1548c4dde73d70cbacc0894`, matching
`independent_results.json`. This provenance note does not alter the original
first-run receipt.
