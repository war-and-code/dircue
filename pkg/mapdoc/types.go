// Package mapdoc defines dircue's portable, deterministic map document.
package mapdoc

const SchemaVersion = "1.0.0"

const QuestionSourceBinding = "source_binding"

type NodeKind string

const (
	NodeContent    NodeKind = "content"
	NodeComponent  NodeKind = "component"
	NodeDeployable NodeKind = "deployable"
	NodeInterface  NodeKind = "interface"
	NodeCapability NodeKind = "capability"
	NodePackage    NodeKind = "package"
	NodeToolRun    NodeKind = "tool_run"
)

type EdgeType string

const (
	EdgeContains       EdgeType = "contains"
	EdgeMemberOf       EdgeType = "member_of"
	EdgeDependsOnLocal EdgeType = "depends_on_local"
	EdgeBuilds         EdgeType = "builds"
	EdgeRuns           EdgeType = "runs"
	EdgeExposes        EdgeType = "exposes"
	EdgeDeclares       EdgeType = "declares"
	EdgeUsesCapability EdgeType = "uses_capability"
	EdgePackagedIn     EdgeType = "packaged_in"
	EdgeAnalyzedBy     EdgeType = "analyzed_by"
	EdgeConflictsWith  EdgeType = "conflicts_with"
)

type CoverageStatus string

const (
	CoverageComplete CoverageStatus = "complete"
	CoveragePartial  CoverageStatus = "partial"
	CoverageUnknown  CoverageStatus = "unknown"
)

type EvidenceBasis string

const (
	BasisDeclaredConfig    EvidenceBasis = "declared_config"
	BasisCodeSyntax        EvidenceBasis = "code_syntax"
	BasisResolvedReference EvidenceBasis = "resolved_reference"
	BasisProviderReported  EvidenceBasis = "provider_reported"
	BasisRuleInferred      EvidenceBasis = "rule_inferred"
	BasisFilenameHint      EvidenceBasis = "filename_hint"
)

type EvidenceSource string

const (
	SourceFile          EvidenceSource = "file"
	SourceDirectory     EvidenceSource = "directory"
	SourceConfiguration EvidenceSource = "configuration"
	SourceCode          EvidenceSource = "code"
	SourceComment       EvidenceSource = "comment"
	SourceDocstring     EvidenceSource = "docstring"
	SourceDocumentation EvidenceSource = "documentation"
)

type Document struct {
	SchemaVersion  string                `json:"schema_version"`
	Kind           string                `json:"kind"`
	Status         CoverageStatus        `json:"status"`
	Source         Source                `json:"source"`
	Coverage       []QuestionCoverage    `json:"coverage"`
	CoverageLedger []CoverageLedgerEntry `json:"coverage_ledger"`
	Nodes          []Node                `json:"nodes"`
	Edges          []Edge                `json:"edges"`
}

// CoverageLedgerEntry records only coverage a supplied provider report actually
// disclosed. An empty CoveredFiles list is unknown coverage, not a clean run.
type CoverageLedgerEntry struct {
	Tool         string   `json:"tool"`
	ReportKind   string   `json:"report_kind"`
	Scope        string   `json:"scope"`
	Binding      string   `json:"binding"`
	Ran          bool     `json:"ran"`
	CoveredFiles []string `json:"covered_files"`
	State        string   `json:"state"`
	Reason       string   `json:"reason"`
}

type Source struct {
	Mode     string  `json:"mode"`
	Revision string  `json:"revision,omitempty"`
	Tree     string  `json:"tree,omitempty"`
	Digest   *Digest `json:"digest,omitempty"`
}

// Digest binds a directory snapshot only when the producer actually read the
// declared scope. A cheap metadata-only map should leave Digest nil and report
// source_binding as unknown or partial in question coverage.
type Digest struct {
	Algorithm string `json:"algorithm"`
	Scope     string `json:"scope"`
	Value     string `json:"value"`
}

type Coverage struct {
	Status  CoverageStatus `json:"status"`
	Reasons []string       `json:"reasons"`
}

type QuestionCoverage struct {
	Question string `json:"question"`
	Scope    string `json:"scope"`
	Coverage
}

type Node struct {
	ID            string            `json:"id"`
	Kind          NodeKind          `json:"kind"`
	Name          string            `json:"name,omitempty"`
	Paths         []string          `json:"paths"`
	Discriminator string            `json:"discriminator,omitempty"`
	Coverage      Coverage          `json:"coverage"`
	Evidence      []Evidence        `json:"evidence"`
	Properties    map[string]string `json:"properties,omitempty"`
	Facts         []Fact            `json:"facts,omitempty"`
}

// Fact retains an attributable observation about a node when it is not itself
// a node or relationship. It can represent manifest requirements, unresolved
// references, declared interfaces, metrics, and provider annotations.
type Fact struct {
	Kind       string            `json:"kind"`
	Name       string            `json:"name,omitempty"`
	Value      string            `json:"value,omitempty"`
	State      string            `json:"state,omitempty"`
	Condition  string            `json:"condition,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
	Coverage   Coverage          `json:"coverage"`
	Evidence   []Evidence        `json:"evidence"`
}

type Edge struct {
	ID            string            `json:"id"`
	Type          EdgeType          `json:"type"`
	From          string            `json:"from"`
	To            string            `json:"to"`
	Discriminator string            `json:"discriminator,omitempty"`
	Coverage      Coverage          `json:"coverage"`
	Evidence      []Evidence        `json:"evidence"`
	Properties    map[string]string `json:"properties,omitempty"`
}

type Evidence struct {
	Basis      EvidenceBasis  `json:"basis"`
	Path       string         `json:"path"`
	Span       *Span          `json:"span,omitempty"`
	SourceKind EvidenceSource `json:"source_kind"`
	Rule       *Producer      `json:"rule,omitempty"`
	Provider   *Producer      `json:"provider,omitempty"`
}

type Span struct {
	StartLine   int `json:"start_line"`
	StartColumn int `json:"start_column,omitempty"`
	EndLine     int `json:"end_line"`
	EndColumn   int `json:"end_column,omitempty"`
}

type Producer struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}
