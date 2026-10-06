// Package declarations reads project manifests without evaluating repository code.
package declarations

import "github.com/war-and-code/dircue/pkg/projects"

const (
	MaxManifestBytes           = projects.MaxManifestBytes
	MaxObservationsPerManifest = 2048
	MaxStringBytes             = 8192
	MaxDocuments               = 4096
	MaxTotalObservations       = 65536
	MaxPatterns                = 128
)

type Requirement = projects.Requirement
type Reference = projects.Reference
type Diagnostic = projects.Diagnostic

// Interface records a declared name or target, never a shell command to execute.
type Interface struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Target    string `json:"target,omitempty"`
	State     string `json:"state"`
	Evidence  string `json:"evidence"`
	Condition string `json:"condition,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

type Project struct {
	ID           string        `json:"id"`
	Root         string        `json:"root"`
	Kind         string        `json:"kind"`
	Name         string        `json:"name,omitempty"`
	Version      string        `json:"version,omitempty"`
	Requirements []Requirement `json:"requirements"`
	References   []Reference   `json:"references"`
	Interfaces   []Interface   `json:"interfaces"`
}

// Data belongs to an adapter and is discarded after inventory-only resolution.
type Document struct {
	Project           *Project
	Diagnostics       []Diagnostic
	Data              any
	Parsed            bool
	workspaceDeclared bool
	retainedBytes     int
	limited           bool
}

type Coverage struct {
	SelectedFiles        int64 `json:"selected_files"`
	ManifestCandidates   int64 `json:"manifest_candidates"`
	ParsedManifests      int   `json:"parsed_manifests"`
	OmittedFiles         int64 `json:"omitted_files"`
	RetainedObservations int   `json:"retained_observations"`
	OmittedDiagnostics   int   `json:"omitted_diagnostics"`
}

type Limits struct {
	ManifestBytes           int64          `json:"manifest_bytes"`
	Documents               int            `json:"documents"`
	ObservationsPerManifest int            `json:"observations_per_manifest"`
	TotalObservations       int            `json:"total_observations"`
	StringBytes             int            `json:"string_bytes"`
	Patterns                int            `json:"patterns"`
	InventoryPaths          int            `json:"inventory_paths"`
	InputBytes              int64          `json:"input_bytes"`
	OutputBytes             int            `json:"output_bytes"`
	ResolutionWork          map[string]int `json:"resolution_work"`
}

type Report struct {
	Provider            string       `json:"provider"`
	ProviderVersion     string       `json:"provider_version"`
	SupportedEcosystems []string     `json:"supported_ecosystems"`
	Selection           string       `json:"selection"`
	Status              string       `json:"status"`
	Source              string       `json:"source"`
	Tree                string       `json:"tree,omitempty"`
	Coverage            Coverage     `json:"coverage"`
	Limits              Limits       `json:"limits"`
	Projects            []Project    `json:"projects"`
	Diagnostics         []Diagnostic `json:"diagnostics"`
}
