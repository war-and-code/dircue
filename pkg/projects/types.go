// Package projects observes project declarations without evaluating build code.
package projects

// Requirement preserves a declared value and the condition attached to it.
// State is declared, conditional, or unresolved; it never asserts tool availability.
type Requirement struct {
	Kind      string `json:"kind"`
	Value     string `json:"value"`
	State     string `json:"state"`
	Evidence  string `json:"evidence"`
	Condition string `json:"condition,omitempty"`
}

// Reference is an edge declared by a manifest. Target is root-relative when
// statically resolvable; unresolved references retain their original Value.
type Reference struct {
	TargetStatus string `json:"target_status,omitempty"`
	Kind         string `json:"kind"`
	Value        string `json:"value"`
	Target       string `json:"target,omitempty"`
	State        string `json:"state"`
	Evidence     string `json:"evidence"`
	Condition    string `json:"condition,omitempty"`
}

type Project struct {
	ConfigurationCandidates []string      `json:"configuration_candidates"`
	ID                      string        `json:"id"`
	Root                    string        `json:"root"`
	Kind                    string        `json:"kind"`
	Evidence                []string      `json:"evidence"`
	Requirements            []Requirement `json:"requirements"`
	References              []Reference   `json:"references"`
	Files                   int64         `json:"files"`
	Bytes                   int64         `json:"bytes"`
}

type Diagnostic struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Document holds observations extracted from one complete manifest.
type Document struct {
	Projects     []Project
	Requirements []Requirement
	References   []Reference
	Diagnostics  []Diagnostic
}

type Counts struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}

type Role struct {
	Name  string `json:"name"`
	Basis string `json:"basis"`
	Counts
}

type Configuration struct {
	Path         string        `json:"path"`
	Requirements []Requirement `json:"requirements"`
	References   []Reference   `json:"references"`
}

type Report struct {
	Status           string          `json:"status"`
	Source           string          `json:"source"`
	Tree             string          `json:"tree,omitempty"`
	Attribution      string          `json:"attribution"`
	MaxManifestBytes int64           `json:"max_manifest_bytes"`
	Projects         []Project       `json:"projects"`
	Configurations   []Configuration `json:"configurations"`
	Composition      []Role          `json:"composition"`
	Unassigned       Counts          `json:"unassigned"`
	Ambiguous        Counts          `json:"ambiguous"`
	OmittedFiles     int64           `json:"omitted_files"`
	Diagnostics      []Diagnostic    `json:"diagnostics"`
}
