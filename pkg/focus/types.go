// Package focus plans a bounded project-specific view of an already selected
// source inventory. It does not walk a filesystem, read manifests, or evaluate
// build-tool configuration.
package focus

import "dircue/pkg/declarations"

const (
	Provider        = "dircue"
	ProviderVersion = "1.0.0"

	DefaultMaxInventoryPaths = 200_000
	DefaultMaxProjectRecords = 4_096
	DefaultMaxRelations      = 65_536
	DefaultMaxWork           = 4_194_304
	DefaultMaxOutputBytes    = 16 << 20
	DefaultQueryLimit        = 1_024
	MaxQueryLimit            = 65_536
)

// File identifies a regular file in the original selected source. Path is
// always root-relative; Size is retained for population accounting only.
type File struct {
	Path string `json:"path"`
	Size int64  `json:"bytes"`
}

// ProjectRecord preserves per-manifest parser eligibility. Project.ID alone is
// intentionally insufficient: declaration reports can retain invalid manifest
// candidates so callers can explain why they were not eligible.
type ProjectRecord struct {
	Project declarations.Project
	Parsed  bool
}

type Input struct {
	Source            string
	Tree              string
	Inventory         []File
	InventoryComplete bool
	OmittedFiles      int64
	// OwnershipBarriers are selected nonregular project-shaped paths. They are
	// not measurable files, but their directories still block ancestor claims.
	OwnershipBarriers []string
	// DeclarationsComplete is independent of inventory coverage. A manifest
	// may be inventoried while its parser evidence is capped or omitted.
	DeclarationsComplete bool
	OmittedProjects      int64
	Projects             []ProjectRecord
}

// Request names one primary project and optional, explicitly requested related
// projects. Declared references never add related source populations by
// themselves.
type Request struct {
	Project    string
	Related    []string
	AffectedBy string
}

type Limits struct {
	InventoryPaths int `json:"inventory_paths"`
	ProjectRecords int `json:"project_records"`
	Relations      int `json:"relations"`
	Work           int `json:"work"`
	OutputBytes    int `json:"output_bytes"`
}

type Scope struct {
	// ID identifies the request, planner rules, and selected source/tree. For a
	// live directory it is not a hash of file contents.
	ID              string   `json:"id"`
	Algorithm       string   `json:"algorithm"`
	Rule            string   `json:"rule"`
	Role            string   `json:"role"`
	PrimaryProject  string   `json:"primary_project,omitempty"`
	RelatedProjects []string `json:"related_projects"`
	AffectedBy      string   `json:"affected_by,omitempty"`
}

type Coverage struct {
	InventoryFiles       int   `json:"inventory_files"`
	InventoryBytes       int64 `json:"inventory_bytes"`
	ProjectRecords       int   `json:"project_records"`
	ParsedProjectRecords int   `json:"parsed_project_records"`
	PrimaryFiles         int   `json:"primary_files"`
	PrimaryBytes         int64 `json:"primary_bytes"`
	RelatedFiles         int   `json:"related_files"`
	RelatedBytes         int64 `json:"related_bytes"`
	ContextInputs        int   `json:"context_inputs"`
	Relations            int   `json:"relations"`
	AmbiguousFiles       int   `json:"ambiguous_files"`
	UnresolvedFiles      int   `json:"unresolved_files"`
	OwnershipBarriers    int   `json:"ownership_barriers"`
	OmittedFiles         int64 `json:"omitted_files"`
	OmittedProjects      int64 `json:"omitted_projects"`
	OmittedRecords       int   `json:"omitted_records"`
	OmittedRelations     int   `json:"omitted_relations"`
	OmittedOutputRecords int   `json:"omitted_output_records"`
	Work                 int   `json:"work"`
}

type Project struct {
	ID     string `json:"id"`
	Root   string `json:"root"`
	Kind   string `json:"kind"`
	Parsed bool   `json:"parsed"`
}

// PopulationFile has qualified ownership. Ambiguous and unresolved files are
// reported as boundaries instead of being admitted to an scc population.
type PopulationFile struct {
	Path      string `json:"path"`
	Bytes     int64  `json:"bytes"`
	ProjectID string `json:"project_id"`
	Ownership string `json:"ownership"`
	Basis     string `json:"basis"`
}

type RelatedPopulation struct {
	Project Project          `json:"project"`
	Files   []PopulationFile `json:"files"`
}

// Context records why a declaration input is relevant while keeping candidate
// applicability separate from parser evidence.
type Context struct {
	ProjectID     string `json:"project_id"`
	Path          string `json:"path"`
	Kind          string `json:"kind"`
	Basis         string `json:"basis"`
	Applicability string `json:"applicability"`
	Evidence      string `json:"evidence"`
	State         string `json:"state"`
	Condition     string `json:"condition,omitempty"`
	Parsed        bool   `json:"parsed"`
}

// Relation is a static declaration edge. Target presence and declaration state
// do not assert runtime resolution or compiler source membership.
type Relation struct {
	SourceProject string `json:"source_project"`
	Target        string `json:"target,omitempty"`
	Kind          string `json:"kind"`
	Class         string `json:"class"`
	Value         string `json:"value"`
	State         string `json:"state"`
	TargetStatus  string `json:"target_status,omitempty"`
	Evidence      string `json:"evidence"`
	Condition     string `json:"condition,omitempty"`
}

// Boundary makes uncertainty explicit. It never means the omitted or
// unsupported material would have changed a compiler result.
type Boundary struct {
	Path       string   `json:"path,omitempty"`
	ProjectID  string   `json:"project_id,omitempty"`
	Reason     string   `json:"reason"`
	Candidates []string `json:"candidates,omitempty"`
	Evidence   string   `json:"evidence,omitempty"`
}

type Omission struct {
	Reason string `json:"reason"`
	Count  int64  `json:"count"`
}

type Report struct {
	Provider         string              `json:"provider"`
	ProviderVersion  string              `json:"provider_version"`
	Status           string              `json:"status"`
	Source           string              `json:"source"`
	Tree             string              `json:"tree,omitempty"`
	Scope            Scope               `json:"scope"`
	Limits           Limits              `json:"limits"`
	Coverage         Coverage            `json:"coverage"`
	PrimaryProject   *Project            `json:"primary_project,omitempty"`
	Primary          []PopulationFile    `json:"primary"`
	Related          []RelatedPopulation `json:"related"`
	Context          []Context           `json:"context"`
	Relations        []Relation          `json:"relations"`
	Boundaries       []Boundary          `json:"boundaries"`
	Omissions        []Omission          `json:"omissions"`
	AffectedProjects *ImpactQuery        `json:"affected_projects,omitempty"`
}

// Result separates the exact execution selection from bounded presentation.
// Output trimming never changes the paths admitted to the requested consumer.
type Result struct {
	Report       *Report
	PrimaryPaths map[string]struct{}
	RelatedPaths map[string]map[string]struct{}
	// SelectionComplete qualifies execution populations independently of report
	// presentation trimming.
	SelectionComplete bool

	contextsByProject map[string][]Context
	projectsByContext map[string][]ProjectImpact
}

type QueryOptions struct {
	Limit int
}

type ContextQuery struct {
	Status    string    `json:"status"`
	ProjectID string    `json:"project_id"`
	Contexts  []Context `json:"contexts"`
	Omitted   int       `json:"omitted"`
}

type ProjectImpact struct {
	ProjectID     string `json:"project_id"`
	Basis         string `json:"basis"`
	Applicability string `json:"applicability"`
	Evidence      string `json:"evidence"`
}

type ImpactQuery struct {
	Status   string          `json:"status"`
	Path     string          `json:"path"`
	Projects []ProjectImpact `json:"projects"`
	Omitted  int             `json:"omitted"`
}
