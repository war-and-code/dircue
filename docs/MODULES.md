# Profiling module boundaries

This document describes the current scanner and the constraints for extending
it. The discovery, graph, and package-import rows refer to the development
branch after 0.3.0. There is no runtime plugin loader or declarative module
registry.

## Inputs and selection

The scanner owns the selected inventory: either a committed Git tree or a
confined directory. Each worker receives a relative path, regular-file size,
resolved attributes, and a reader for that source. Optional modules must use
those selected bytes. Opening a path in the checkout directly would mix dirty
working files into a Git-tree analysis.

Directory mode is a live view, not a filesystem snapshot. Bounded read checks
catch some input changes, including inconsistent rereads, but do not establish
that every file existed simultaneously in its observed state. A consumer that
needs stronger correspondence should supply an immutable checkout or use the
selected Git tree. Git-dependent evidence can be unavailable without preventing
ordinary directory profiling.

| Module | Population and inputs | Additional work |
| --- | --- | --- |
| Languages | Selected regular files, attributes, bounded content; Linguist inclusion rules apply | Classifies a prefix of at most 128 KiB; full selected file sizes contribute to totals |
| Detector hooks | Eligible file views, including recognized manifests and CI configuration; vendor exclusions still apply | Stateless, concurrent observation hooks |
| Discovery | Regular-file metadata, including paths excluded from language totals | Filename/extension candidates and overlapping path/attribute roles; no source-payload reads |
| Projects | Selected inventory and recognized manifests, independently of XML language inclusion | Bounded manifest parsing, declarations, configuration candidates, composition, attribution |
| Metrics | Explicit source or text scope | Complete-file scc counting, with size bounds and omission reasons |
| Structure | Selected supported source files | Complete bounded source sent to an explicit native worker; per-file syntax and metrics |
| Graph | Finalized project report | Pure aggregation over defined declaration edges; no new filesystem reads |
| Package evidence | Explicit imported report and project context | Bounded parsing and conservative association; no Syft execution or evidence-path reads |

An inventory exclusion, a language-statistics exclusion, and an unsupported
module input are different facts. For example, XML can be excluded from
language totals while an XML project manifest supplies build declarations.
A vendored archive can matter to discovery without being a source-language
input. Each module must report its own coverage; top-level language counters
are not a substitute.

## Delivery, ownership, and limits

One bounded job queue distributes files among scanner workers. Within a job,
the scanner calls each applicable consumer. Giving several independent modules
the same receive channel would distribute files between modules and lose
observations; it would not broadcast each file to all subscribers.

Workers send typed results to scan-scoped collectors. Collectors combine those
results and sort public output deterministically. Graph calculations and
package associations happen after project finalization. Findings, relationships,
counts, and paths can outlive a file job; complete source contents should not.

`profile.File.Content` is a borrowed, bounded byte slice. Detector hooks must
not modify or retain it. Hooks are invoked concurrently and must be
concurrency-safe. Hook results use root-relative evidence paths validated by
the scanner. The Go interface is for trusted compiled code; it cannot prevent
a malicious implementation from accessing the process environment or network.

Project and structural consumers can reuse a per-file read cache. That cache
belongs to one job, is never shared between workers, and is released when the
job completes. A larger read can require fetching bytes again; this is not a
persistent cross-run cache or a promise that every source byte is read once.
Structural worker invocation is separately gated so file-worker concurrency
does not multiply native parser processes without bound.

Cancellation propagates through enumeration, workers, bounded readers, and
external-worker calls. A fatal input error cancels the scan and returns an
error instead of a success-shaped partial result. Supported omission cases,
such as a configured tree or file-size limit, can produce successful processes
with skipped or partial modules. Consumers must check those statuses. Detector
errors become warnings and can retain valid accompanying observations, as in
the existing detector contract.

Each module has its own read, collection, and output bounds. These bounds are
not a process RSS or CPU guarantee. The pending resource work in
[#1](https://github.com/war-and-code/dircue/issues/1) and
[#2](https://github.com/war-and-code/dircue/issues/2) must also account for Go
runtime overhead, Git storage, result accumulation, and native children.

## Adding a module

Before adding a scanner option or command, specify:

1. The question answered, supported evidence, and what cannot be inferred.
2. The selected population, metadata/content needs, and explicit limits.
3. Ownership and concurrency rules for observations and aggregation.
4. A typed result with provider/rule identity, source correspondence, evidence,
   effective scope, and unavailable/partial/complete behavior.
5. Compatibility checks for existing commands and separate measurements of
   incremental time, memory, reads, and output size.

Prefer pure aggregation over existing results when no additional input is
needed. Deeper analysis remains explicit, and repository-owned hints cannot
silently enable execution or disable caller-required work. Absence of an
observation must not become a universal recommendation to skip other analysis.

The current implementation still wires module-specific options and collectors
directly. A future registry should replace that wiring only when its scope and
ownership contract improves on these concrete integrations. Provider versions,
source identities, and omissions are not yet represented uniformly across all
legacy result types; changing those contracts requires explicit schema and
compatibility work. See [#4](https://github.com/war-and-code/dircue/issues/4),
the [capability matrix](CAPABILITIES.md), and the module-specific guides.
