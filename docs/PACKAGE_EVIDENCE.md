# Importing package evidence

Available on the development branch after 0.3.0.

Dircue can import an existing native [Syft](https://github.com/anchore/syft)
JSON report and associate its locations with the static project inventory.
Import is explicit. Dircue does not look for an installed Syft executable,
invoke it, contact registries, or restore dependencies.

```sh
dircue analyze packages --syft-report /reports/syft.json --json /checkout
dircue analyze packages --syft-report /reports/syft.json --syft-root / --json /checkout
dircue analyze all --discovery --graph --syft-report /reports/syft.json --syft-root / --json /checkout
```

`analyze packages` requires `--syft-report`. On `analyze all`, that flag opts
into the import. Both include the project inventory needed for attribution.
The aggregate report uses schema `1.3.0`, with `projects` and
`package_evidence` fields. Existing invocations retain their earlier schemas.

## Report coordinates and source identity

Syft locations are coordinates in the cataloged source, not instructions to
open paths on this machine. Dircue never follows those paths. Without an
explicit mapping, it retains location digests and reports unmapped evidence.
It does not infer a mapping from Syft's host filesystem path.

`--syft-root /` explicitly maps the report's `/` coordinate root to the
selected dircue inventory. This is suitable for ordinary Syft directory
reports whose source is the same directory or tree being profiled. Other
coordinate roots can be supplied when the caller knows that relationship.
Paths escaping the mapped root are not assigned to projects. Nested-archive
locations remain separate evidence rather than becoming ordinary source files.

A coordinate mapping does **not** prove that the report describes this
checkout. Without a trusted source binding, `source.match` stays `unknown`
and candidate project associations are marked `source-unverified` or
`ambiguous-source-unverified`. A Syft source ID alone is not a content hash.

For a selected Git tree, a caller that has independently recorded the exact
report-to-tree relationship can supply both:

```sh
dircue analyze packages --source git --rev HEAD --json \
  --syft-report /reports/syft.json --syft-root / \
  --syft-report-sha256 "$REPORT_SHA256" --syft-source-tree "$CATALOGED_TREE" \
  /checkout
```

The report digest is SHA-256 over the exact imported bytes; the tree value is
a 40-digit Git tree ID, not a commit ID. Dircue compares both values to its
inputs. A match records a **caller-supplied attestation**, not independent
verification of what Syft scanned. A mismatch produces partial evidence and
withholds project associations. Directory scans cannot use this Git binding.
Do not derive an attestation merely by hashing an unrelated report and the
current checkout.

Project attribution also requires a complete project inventory. When that
inventory is partial or skipped, the report withholds project IDs: an omitted
nested project must not make a package appear to belong to its parent.
Multiple projects sharing a root remain ambiguous.

## Coverage and limits

The initial importer accepts native Syft JSON schema **16.1.10**, exercised
against an actual Syft **1.52.0** report. Other schema versions are rejected
until their interpretation is supported and tested. This is not a general
SPDX or CycloneDX importer.

| Boundary | Default |
| --- | --- |
| Report bytes | 16 MiB; `--syft-report-max-bytes` can raise this to 128 MiB |
| Packages | 20,000 |
| Relationships | 50,000 |
| Files | 50,000 |
| Locations | 100,000 |
| JSON nesting depth | 64 |
| JSON values, including containers | 2,000,000 |

The input must be a regular file. Malformed, unsupported, duplicate-key, or
over-limit reports fail without a partial JSON result. The byte limit bounds
input size, not total process memory. Library callers can configure collection
limits within the importer's hard caps.

`coverage.import` describes the supported import work. `coverage.provider_scan`
stays `unknown`: successfully reading a Syft report does not establish that
Syft found every package or cataloged every file. Source correspondence and
metadata coverage are reported separately. Unknown relationship kinds or
endpoints remain qualified evidence and can make the import partial.
Relationships such as archive containment are not recast as dependency edges.

The output preserves provider and schema versions, report digest, selected
configuration details, catalogers, package locations, relationship kinds, and
diagnostics. Arbitrary metadata and host paths are represented by digests;
recognized credential forms and PURLs with qualifiers or fragments are omitted.
This is a normalized subset,
not a lossless replacement for the original Syft report. Retain that report if
you need fields outside the supported subset.

See the [fixture provenance](../tests/packageevidence/README.md),
[project guide](PROJECTS.md), and [staged-analysis guide](STAGED_ANALYSIS.md).
