# Direction after 0.3.0

Dircue profiles source repositories and other directories of computer content.
The next step is to make its observations easier to reuse: first discover what
is present, then explicitly request the detail needed for a particular task.
That can support navigation, testing, architecture review, build preparation,
and many kinds of downstream analysis.

This document records the direction agreed on September 18, 2026. The work
sequence describes proposed capabilities, not promises for a particular
release. The development status below distinguishes implemented work from
remaining plans. Linked issues hold the acceptance criteria and decisions;
the [capability matrix](CAPABILITIES.md) distinguishes released support from
development additions.

## Development status after 0.3.0

These additions are on the development branch, not in the downloadable 0.3.0
binaries. They remain subject to candidate validation and review.

| Area | Implemented development work | Remaining work |
| --- | --- | --- |
| Discovery and module contracts | Explicit metadata inventory with candidate manifests, scoped counts, and omissions; documented module ownership | More file-role adapters and any future module registry |
| Project relationships | Static .NET graph components, cycles, and degrees from supported declarations | Broader workspace adapters and relationship types |
| Package evidence | Bounded import of existing native Syft JSON with explicit mapping and source binding | Optional execution of an installed Syft binary |
| Caller rules | Explicit bounded JSON rules over filenames, paths, and complete-file literals | Named profiles and additional rule operations |
| Function evidence | Optional bounded BCA function-space metrics with spans, hashes, and coverage | Comparable distributions, explainable hotspot selection, and report comparison |
| Semantic analysis | Retained Bifrost feasibility and isolation findings | A validated production provider contract and broader cost measurements |
| Distribution | Manual workflow for verified draft-release assembly and local packaged function checks | Full cross-platform workflow rehearsal and any public/PyPI publication |

No repository scheduler, atlas, or cross-repository aggregator is planned as
part of these changes. A consumer may reuse dircue reports for that purpose.

## Starting point

Version 0.3.0 preserves the Linguist-compatible language interface and Git-free
directory analysis. Explicit modules add scc counting, static project
declarations, and a BCA/Tree-sitter worker covering 20 source languages. Java
and C# have additional syntax declaration counts. There is no cross-file call
graph, compiler name resolution, runtime route discovery, or vulnerability
analysis in the current structural report.

The [staged-analysis guide](STAGED_ANALYSIS.md) shows the released first pass and
follow-up composition. The development branch's [discovery command](DISCOVERY.md)
adds a metadata-only option; reusable scan results and semantic relationships
remain future work. Existing language JSON, exit behavior, and ordinary
invocation costs remain compatibility gates.

## Work sequence

| Work | Intended result | Tracking |
| --- | --- | --- |
| Refine module contracts and cheap discovery | Bounded observations over a selected source, with scope and omissions explicit | [#4](https://github.com/war-and-code/dircue/issues/4), [#6](https://github.com/war-and-code/dircue/issues/6), [#22](https://github.com/war-and-code/dircue/issues/22) |
| Extend project and entry-point mapping | Evidence for project relationships, handlers, interfaces, and operations; start with Spring and ASP.NET Core | [#3](https://github.com/war-and-code/dircue/issues/3), [#5](https://github.com/war-and-code/dircue/issues/5), [#23](https://github.com/war-and-code/dircue/issues/23) |
| Evaluate optional semantic analysis | Decide whether Bifrost provides useful references, calls, and bounded flow at an acceptable cost | [#24](https://github.com/war-and-code/dircue/issues/24) |
| Add trusted rules and portable context | Reusable observations and declared prerequisites for caller-selected tools | [#9](https://github.com/war-and-code/dircue/issues/9), [#25](https://github.com/war-and-code/dircue/issues/25) |
| Explain hotspots and compare reports | Like-for-like changes in metrics and relationships, with reasons and missing evidence visible | [#27](https://github.com/war-and-code/dircue/issues/27), [#10](https://github.com/war-and-code/dircue/issues/10) |
| Optimize repeated work | Reuse compatible results; validate incremental output against fresh full analysis | [#10](https://github.com/war-and-code/dircue/issues/10) |

These are dependencies and priorities, not a requirement to ship everything
together. A Bifrost evaluation can run alongside contract work. A negative
evaluation is a useful outcome; entry-point mapping does not require adopting
Bifrost. Broader language and framework coverage should arrive as separately
tested adapters, without making Java and .NET the only useful inputs.

## Evidence that consumers can use

Observations should identify their source snapshot, project, path and span,
provider and rule version, effective scope, and coverage. A declared route, a
syntactic call, a resolved target, and a possible target convey different facts.
Unknown, unsupported, partial, and failed analysis must remain distinguishable
from a successful search with no matches.

Application entry points include HTTP handlers, commands, queue consumers,
scheduled jobs, and GraphQL operations. Operation observations can cover storage,
network, filesystem, process, and serialization use. Their presence alone does
not establish runtime exposure or a complete execution path. Dynamic routing,
reflection, generated code, and missing dependencies need explicit limitations.

Repository content supplies evidence. Trusted caller policy decides which work
to request and how to interpret it. Repository-owned configuration must not
silently disable required work or grant permission to run commands. Installing
an external analyzer must not change the default behavior.

The inventory must also describe non-code content. A small project beside
gigabytes of XML must remain discoverable; XML is not necessarily a log, and
an empty language report is not proof that no useful follow-up exists. Default
code metrics continue to exclude data such as XML logs, subject to the selected
scope and attribute overrides.

## Providers and result reuse

The proposed Bifrost evaluation starts with an explicitly selected CLI and
structured output. It must establish source identity, supported versions,
cache ownership, offline operation, cancellation, and process-tree resource
cost. Returned graph edges need their original certainty and completeness
qualifications. Shared use of Tree-sitter does not make Bifrost's index
interchangeable with BCA's trees or metrics.

Choose providers for requested capabilities. Avoid running both merely because
both are installed. Preserve existing BCA metric definitions; measure any
duplicate parsing and decide whether reusable results justify their storage
and invalidation costs. No production dependency change follows from creating
the evaluation issue.

Package evidence is a separate extension: import an existing Syft report in
[#20](https://github.com/war-and-code/dircue/issues/20), then optionally invoke
an installed executable in [#21](https://github.com/war-and-code/dircue/issues/21).
Package dependencies and source-project relationships remain separate types.
Imported reports must disclose unknown or mismatched source identity.

## Useful metrics without a universal grade

Dircue already exposes scc's lexical complexity and BCA's per-file metric
groups. A review view should reuse those results before adding another engine.
Function hotspots require function-level measurements and source locations;
they cannot be inferred from a file total alone.

Show distributions, outliers, and comparable changes with their definitions
and measured populations. Keep lexical, cyclomatic, cognitive, and graph
metrics distinct. A large file with many simple functions differs from one
complex function. Moving branches into helpers or dispatch tables can change
a score without reducing the behavior a reader must understand.

Dependency cycles, fan-in/fan-out, optional churn, and imported execution
coverage could add context. They must retain their graph type, source identity,
and missing data. Test-file counts are not execution coverage; a missing
measurement is not zero. Thresholds belong to caller policy. A higher branch
count may reflect a useful validation check, and a straight-line operation can
still contain a defect. None of these metrics alone establishes safety,
maintainability, or a reason to suppress further analysis.

## Optional downstream experiments

Portable context can help users prepare build tools, review code, select tests,
or configure their own analysis pipeline. Tool-specific recipes should remain
separate from the general profiler. Export evidence and declared prerequisites;
do not imply that static observations establish a successful build. Build,
restore, network, and remote-service requirements must be explicit.

[#26](https://github.com/war-and-code/dircue/issues/26) records a separate
OpenTaint-to-ZAP experiment on an authorized test application. It would retain
original static findings and record dynamic outcomes separately: reproduced,
attempted but not reproduced, and not tested. Non-reproduction is not evidence
that a finding was false. Such a recipe needs deployed-source identity,
authentication, endpoint mapping, and broader scans as well as targeted runs.
Creating the issue does not initiate builds or active testing.

Review assistants are another consumer of bounded context. Source text must
remain untrusted input, and sending it to a remote service must be deliberate.
Repeated useful discoveries can become reviewed and tested rules. Measure
the whole workflow: preparation, profiling, indexing, downstream work, model
cost where used, and human review. An extra profiling step is worthwhile only
when its useful results justify its cost.

## Continuing work

- Resource budgets: [memory #1](https://github.com/war-and-code/dircue/issues/1)
  and [CPU #2](https://github.com/war-and-code/dircue/issues/2), including child
  processes and explicit enforcement limits.
- Further observations: [registry configuration #7](https://github.com/war-and-code/dircue/issues/7),
  [manifest consistency #8](https://github.com/war-and-code/dircue/issues/8),
  and [workflow/infrastructure relationships #11](https://github.com/war-and-code/dircue/issues/11).
- Broader scc reuse and structural coverage: [#12](https://github.com/war-and-code/dircue/issues/12)
  and [#17](https://github.com/war-and-code/dircue/issues/17). Their initial
  integrations shipped; remaining work should be assessed incrementally.
- Distribution: [release automation #13](https://github.com/war-and-code/dircue/issues/13)
  and [PyPI #14](https://github.com/war-and-code/dircue/issues/14). GitHub Releases
  already carry binaries and wheels; PyPI publication remains a separate decision.
- Upstream maintenance: continue the [Enry and dependency update process](../third_party/README.md)
  and [scc update checks](SCC_UPSTREAM.md), and retire local go-git fixes when
  equivalent released upstream fixes are verified ([#15](https://github.com/war-and-code/dircue/issues/15)).

## Reading behind the proposals

- [Bifrost](https://github.com/BrokkAi/bifrost) and its
  [capability boundaries](https://bifrost.brokk.ai/capabilities/): candidate
  semantic provider. The [DataFlowBench comparison](https://dataflowbench.brokk.ai/)
  is useful for test cases, but answered-case accuracy must be read alongside
  unanswered cases, scope, and provider-authored benchmark design.
- [OpenTaint](https://github.com/seqra/opentaint) and the
  [guided ZAP experiment](https://www.zaproxy.org/blog/2026-03-27-guided-zap-scans-faster-cicd-feedback-using-sast/):
  an example of passing evidence between specialized downstream tools, not a
  general performance guarantee.
- [Synthesia's review workflow](https://www.synthesia.io/post/automating-code-security-reviews-with-claude-mythos-level-capabilities):
  orientation, bounded review, deduplication, and independent validation are
  useful design ideas; the headline is not a model-equivalence guarantee.
- [The HN complexity discussion](https://news.ycombinator.com/item?id=49731413),
  [NDepend's C# article](https://blog.ndepend.com/understanding-cyclomatic-complexity/),
  and [Python Code Audit's complexity check](https://nocomplexity.com/documents/codeaudit/complexitycheck.html#complexity-check):
  motivation for explainable hotspots and clear metric definitions, not a
  validated universal risk score.
