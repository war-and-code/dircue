// Package planning builds inert, bounded follow-up plans from validated saved
// profile evidence. It does not read source paths, inspect PATH, or execute work.
package planning

import (
	"errors"

	"github.com/war-and-code/dircue/pkg/capabilities"
	"github.com/war-and-code/dircue/pkg/profile"
)

const (
	SchemaVersion        = "1.0.0"
	MaxRequests          = 64
	MaxProjects          = 1
	MaxEvidencePerStep   = 16
	MaxEvidence          = 256
	MaxKeyBytes          = 256
	MaxReportedRootBytes = 64 << 10
	MaxOutputBytes       = 1 << 20
)

var (
	ErrInvalid = errors.New("invalid planning input")
	ErrLimit   = errors.New("planning limit exceeded")
)

type Selection struct {
	Questions []string
	Modules   []string
	Projects  []string
	Inputs    []string
}

type Input struct {
	Profile      *profile.Report
	ReportSHA256 string
	Capabilities capabilities.Descriptor
	Selection    Selection
}

type Identity struct {
	ReportSHA256            string         `json:"report_sha256"`
	ProfileSchemaVersion    string         `json:"profile_schema_version"`
	DeclaredRoot            string         `json:"declared_root"`
	CapabilitySchemaVersion string         `json:"capability_schema_version"`
	CapabilityProvider      string         `json:"capability_provider"`
	CapabilityVersion       string         `json:"capability_version"`
	Source                  SourceIdentity `json:"source"`
}

type SourceIdentity struct {
	Mode   string `json:"mode"`
	Tree   string `json:"tree,omitempty"`
	Status string `json:"status"`
}

type Evidence struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Path  string `json:"path,omitempty"`
	Value string `json:"value,omitempty"`
}

type Scope struct {
	Kind           string   `json:"kind"`
	Projects       []string `json:"projects"`
	CandidatePaths []string `json:"candidate_paths"`
	CandidateFiles int64    `json:"candidate_files"`
	CandidateBytes int64    `json:"candidate_bytes"`
	EvidenceStatus string   `json:"evidence_status"`
}

type Cost struct {
	Class           string `json:"class"`
	Inspection      string `json:"inspection"`
	CandidateFiles  int64  `json:"candidate_files"`
	CandidateBytes  int64  `json:"candidate_bytes"`
	ExternalProcess bool   `json:"external_process"`
	Qualification   string `json:"qualification"`
}

type Command struct {
	Argv                 []string `json:"argv"`
	Executable           bool     `json:"executable"`
	SourcePlaceholder    string   `json:"source_placeholder"`
	RevalidationRequired []string `json:"revalidation_required"`
}

type Decision struct {
	ID                       string   `json:"id"`
	Question                 string   `json:"question"`
	Module                   string   `json:"module"`
	Applicability            string   `json:"applicability"`
	Reasons                  []string `json:"reasons"`
	SupportingObservationIDs []string `json:"supporting_observation_ids"`
	UnresolvedInputs         []string `json:"unresolved_inputs"`
}

type Step struct {
	ID                       string   `json:"id"`
	DecisionID               string   `json:"decision_id"`
	Question                 string   `json:"question"`
	Module                   string   `json:"module"`
	Scope                    Scope    `json:"scope"`
	Prerequisites            []string `json:"prerequisites"`
	UnresolvedInputs         []string `json:"unresolved_inputs"`
	ExpectedEvidence         []string `json:"expected_evidence"`
	SupportingObservationIDs []string `json:"supporting_observation_ids"`
	Cost                     Cost     `json:"cost"`
	Command                  Command  `json:"command"`
}

type Limits struct {
	Requests        int `json:"requests"`
	Projects        int `json:"projects"`
	EvidencePerStep int `json:"evidence_per_step"`
	Evidence        int `json:"evidence"`
	KeyBytes        int `json:"key_bytes"`
	OutputBytes     int `json:"output_bytes"`
}

type Report struct {
	SchemaVersion string     `json:"schema_version"`
	Kind          string     `json:"kind"`
	Status        string     `json:"status"`
	Identity      Identity   `json:"identity"`
	Limits        Limits     `json:"limits"`
	Evidence      []Evidence `json:"evidence"`
	Decisions     []Decision `json:"decisions"`
	Steps         []Step     `json:"steps"`
}
