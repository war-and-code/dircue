// Package reportdiff compares explicitly supplied reports without inspecting
// their source directories, following evidence paths, or invoking profilers.
package reportdiff

import (
	"encoding/json"
	"errors"

	"dircue/pkg/profile"
)

const (
	SchemaVersion        = "1.0.0"
	MaxInputBytes        = 32 << 20
	MaxJSONDepth         = 64
	MaxJSONNodes         = 1000000
	MaxStringBytes       = 65536
	MaxChanges           = 4096
	MaxFieldValueBytes   = 2048
	MaxEvidencePaths     = 16
	MaxEvidencePathBytes = 1024
	MaxOutputBytes       = 8 << 20
)

var (
	ErrInvalid     = errors.New("invalid dircue profile JSON")
	ErrUnsupported = errors.New("unsupported dircue profile schema")
	ErrLimit       = errors.New("saved report exceeds comparison limits")
)

// Snapshot retains validated input. Its digest identifies the exact saved
// bytes, not a source tree or a promise of authentic provenance.
type Snapshot struct {
	profile   profile.Report
	sha256    string
	bytes     int64
	validated bool
}

type Identity struct {
	Role          string `json:"role"`
	ReportSHA256  string `json:"report_sha256"`
	ReportBytes   int64  `json:"report_bytes"`
	SchemaVersion string `json:"schema_version"`
	DeclaredRoot  string `json:"declared_root"`
}

// Value retains small typed fields. Larger values retain their digest and size
// instead of copying complete projects or dependency inventories into a diff.
type Value struct {
	SHA256  string          `json:"sha256"`
	Bytes   int             `json:"bytes"`
	Data    json.RawMessage `json:"data,omitempty"`
	Omitted bool            `json:"omitted"`
}

type FieldChange struct {
	Field string `json:"field"`
	Base  *Value `json:"base,omitempty"`
	Head  *Value `json:"head,omitempty"`
}

type Change struct {
	ID                  string        `json:"id"`
	Status              string        `json:"status"`
	Basis               string        `json:"basis"`
	Reason              string        `json:"reason,omitempty"`
	BaseEvidence        []string      `json:"base_evidence"`
	HeadEvidence        []string      `json:"head_evidence"`
	BaseEvidenceOmitted int           `json:"base_evidence_omitted"`
	HeadEvidenceOmitted int           `json:"head_evidence_omitted"`
	Fields              []FieldChange `json:"fields"`
}

type Counts struct {
	Added          int `json:"added"`
	Removed        int `json:"removed"`
	Changed        int `json:"changed"`
	Unchanged      int `json:"unchanged"`
	Unavailable    int `json:"unavailable"`
	OmittedChanges int `json:"omitted_changes"`
}

type Module struct {
	Name          string        `json:"name"`
	Scope         string        `json:"scope"`
	Status        string        `json:"status"`
	Compatibility string        `json:"compatibility"`
	Reasons       []string      `json:"reasons"`
	BaseStatus    string        `json:"base_status"`
	HeadStatus    string        `json:"head_status"`
	Metadata      []FieldChange `json:"metadata"`
	Counts        Counts        `json:"counts"`
	Changes       []Change      `json:"changes"`
}

type Limits struct {
	InputBytes        int `json:"input_bytes"`
	Depth             int `json:"depth"`
	Nodes             int `json:"nodes"`
	StringBytes       int `json:"string_bytes"`
	Changes           int `json:"changes"`
	FieldValueBytes   int `json:"field_value_bytes"`
	OutputBytes       int `json:"output_bytes"`
	EvidencePaths     int `json:"evidence_paths"`
	EvidencePathBytes int `json:"evidence_path_bytes"`
}

type Report struct {
	SchemaVersion string   `json:"schema_version"`
	Kind          string   `json:"kind"`
	Status        string   `json:"status"`
	SourcePairing string   `json:"source_pairing"`
	Base          Identity `json:"base"`
	Head          Identity `json:"head"`
	Limits        Limits   `json:"limits"`
	Modules       []Module `json:"modules"`
}
