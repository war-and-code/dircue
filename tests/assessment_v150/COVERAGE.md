# Assessment 1.5.0 acceptance coverage

This is an acceptance subset for the behaviors below, not a complete language,
manifest, build-system, or MSBuild conformance claim. Every fixture is synthetic
and its assertions are written from its declarations before running the
candidate.

| Requirement in this suite | Hand-labeled expectation | Coverage |
|---|---|---|
| Populations distinguish one substantial project from many small projects | Exact selected file/byte count; parsed project totals are distinct from files and candidates | large Go module; .NET app/library/test solution |
| Explicit workspace groups retain membership or qualified static uncertainty | Group kind/ecosystem/member totals reflect literal declarations; dynamic Gradle include remains qualified | Maven, Gradle, npm, uv, Cargo, and Go workspaces |
| Explicitly empty supported workspaces remain visible | Zero-member group records preserve declared boundaries | npm, Gradle, Cargo, and Go |
| Mixed repositories retain independent evidence | Groups and project populations remain partitioned by ecosystem | Combined multi-ecosystem tree |
| Local reference graph preserves definite vs qualified evidence | Only literal, resolved unconditional project references appear as definite edges; a conditional duplicate of a definite reference, a missing target, and a two-project cycle remain visible | .NET reference graph |
| Entry-point evidence can differ when project graph does not | Same project graph with distinct declared launch scripts yields the corresponding entry-point observations | npm app fixtures |
| Separate npm binary and script declarations remain distinct | `bin` and `scripts.start` for one package both survive as entry observations | npm package fixture |
| Deployment entry taxonomy is provider-specific | CloudFormation functions/tasks and Helm application charts are entries; generic CloudFormation resources and Helm libraries are excluded and counted | CloudFormation plus Helm fixture |
| Declaration and association totals are independent | Three unassociated deployment declarations remain three declarations, zero project associations; one Skaffold declaration matching two same-root projects remains one declaration with two qualified association rows | Deployment and Skaffold fixtures |
| Skaffold associations use declared context and expose ambiguity | Exact context-to-root match is associated; a shared root with two projects keeps two qualified rows | Skaffold context fixtures |
| Source entry-point discovery stays explicitly uninspected | Entry-point coverage is partial and names `source_entry_points_not_inspected`, including association-layer failure | Skaffold and association-failure fixtures |
| Explicit self-reference is not silently discarded | A literal `.NET ProjectReference` to its own project remains a definite or qualified observation | Single-project .NET fixture |
| Coordinate-only Maven sibling is not promoted to a local edge | Without a declared reactor, coordinate matching remains qualified evidence | Two independent Maven POMs |
| Vendor/generated/data inflation does not invent projects or groups | Inventory totals change by exact added files/bytes; project and relation facts stay fixed | Paired baseline/inflated trees |
| Evidence is bounded without falsifying population totals | Evidence/member arrays obey documented bounds; aggregate counts and omitted counts remain explicit | 70-member npm workspace and 270 manifest candidates |
| Solution group samples preserve totals above their cap | 270 explicit solution groups produce an exact total plus retained/omitted samples | Synthetic .NET solution set |
| NuGet candidate presence is separate from ownership | Empty shared props, CPM version-only declarations, literal custom lock path, condition/import/collision uncertainty are reported distinctly | Static synthetic .NET files |
| NuGet custom path ownership scales across unique and colliding projects | 18 distinct paths remain determinate; two of the 20 projects sharing one path remain indeterminate | Static synthetic .NET files |
| NuGet custom/default path collisions and foreign shared props remain uncertain | Both colliding owners stay indeterminate; a foreign-namespace shared props file cannot create determinate ownership | Static synthetic .NET files |
| Structural output is ecosystem-neutral | Dart local path dependency is a definite edge; unsupported Python association remains descriptive; flat data files create no projects | Dart and Python fixtures plus text/XML/CSV-only tree |
| Parser errors are ecosystem-scoped | Malformed Maven XML does not lower valid npm workspace membership coverage | Cross-ecosystem poison fixture |
| Saved report contracts are enforced | CLI-exported offline profile and assessment schemas and native compare loader accept every new report; population, group, graph, row/association, and lock corruptions are rejected | Every saved synthetic report and eleven corruption probes |
| MSBuild import path anchoring is checked against SDK item results | `MSBuildThisFileDirectory`, `./`, and backslash paths select `build/common.props`, not a decoy `build/build/common.props` | Isolated SDK 10.0.401 on hand-authored fixtures |
| Shared NuGet inputs follow only supported implicit SDK imports | The base, Web, Razor, and Worker SDKs import props/targets; a bare project imports neither; disabled/dynamic target controls remain qualified in candidate evidence | Controlled SDK oracle and paired candidate fixtures |
| Limits and invalid options are explicit | Invalid source/workers/byte limit fail; byte-capped inputs qualify omitted evidence | Runtime CLI checks |
| Source/worker/relocation stability | Directory and committed Git facts match after source identity is removed; 1/8 workers and relocated copies are deterministic | Repeated synthetic tree runs |
| Legacy output remains stable | Default language command matches explicit languages; opting into assessment preserves language output | CLI compatibility checks |
| Report stays descriptive | No policy verdict or independence/sufficiency claim appears in the report | Recursive report-key/value scan |
| Entry-point zero remains qualified | A zero observed entry-point count cannot be labeled complete while source entry points are not inspected | Every saved report fixture |

The suite does not cover all manifest grammar, MSBuild evaluation, property
functions, SDK import behavior, all conditional expressions, package resolution,
restore/build success, source-control providers, or every filesystem race and
permission failure. The optional MSBuild oracle covers only assignment/import
precedence, empty/dynamic shadowing, and literal property/item queries for the
generated fixtures. It does not infer NuGet restore path normalization or claim
complete MSBuild conformance.
