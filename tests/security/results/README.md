# Results

`govulncheck-2026-09-07.txt` is the unedited output from these commands at the
repository root:

```sh
go version
go run golang.org/x/vuln/cmd/govulncheck@latest -version
go run golang.org/x/vuln/cmd/govulncheck@latest -show verbose ./...
```

The scan used Go 1.26.6, govulncheck v1.7.0, and the vulnerability database
updated 2026-09-02 19:12:04 UTC. It found zero symbol-reachable
vulnerabilities. It also reported four unreachable dependency advisories from
`golang.org/x/crypto` v0.53.0:

- GO-2026-6355 and GO-2026-6354, fixed in v0.56.0;
- GO-2026-6303, fixed in v0.55.0;
- GO-2026-5932 for the unmaintained `openpgp` package, with no fixed version.

Auragaze's scanned paths do not call the vulnerable symbols according to
govulncheck's static call graph. The complete module inventory and scanner
wording are retained in the transcript.
