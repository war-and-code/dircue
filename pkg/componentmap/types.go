// Package componentmap normalizes parsed project declarations into one
// ecosystem-neutral component population. It does not read files or evaluate
// build tools.
package componentmap

// Component is a buildable, installable, or aggregating unit identified by
// its root-relative manifest path.
type Component struct {
	Key                string                `json:"key"`
	Root               string                `json:"root"`
	Manifest           string                `json:"manifest"`
	Ecosystem          string                `json:"ecosystem"`
	Kind               string                `json:"kind"`
	Name               string                `json:"name,omitempty"`
	Version            string                `json:"version,omitempty"`
	Coverage           string                `json:"coverage"`
	Requirements       []DeclaredRequirement `json:"requirements"`
	gradleSettingsRoot bool
}

type DeclaredRequirement struct {
	Kind      string `json:"kind"`
	Value     string `json:"value"`
	State     string `json:"state"`
	Evidence  string `json:"evidence"`
	Condition string `json:"condition,omitempty"`
}

// Relationship is a local relationship between two retained components.
// DeclarationKind preserves the adapter-specific observation that supports it.
type Relationship struct {
	Type                   string `json:"type"`
	From                   string `json:"from"`
	To                     string `json:"to"`
	DeclarationKind        string `json:"declaration_kind"`
	Evidence               string `json:"evidence"`
	State                  string `json:"state"`
	Condition              string `json:"condition,omitempty"`
	Coverage               string `json:"coverage"`
	gradleSettingsEvidence bool
}

// QualifiedReference preserves a reference that cannot safely become a local
// graph edge. Unknown and missing targets are observations, never absence.
type QualifiedReference struct {
	From                   string `json:"from"`
	DeclarationKind        string `json:"declaration_kind"`
	Value                  string `json:"value"`
	Target                 string `json:"target,omitempty"`
	TargetStatus           string `json:"target_status,omitempty"`
	Evidence               string `json:"evidence"`
	State                  string `json:"state"`
	Condition              string `json:"condition,omitempty"`
	Reason                 string `json:"reason"`
	gradleSettingsEvidence bool
}

type Coverage struct {
	Status              string `json:"status"`
	InputStatus         string `json:"input_status"`
	Components          int    `json:"components"`
	Relationships       int    `json:"relationships"`
	QualifiedReferences int    `json:"qualified_references"`
}

type Fragment struct {
	Components          []Component          `json:"components"`
	Relationships       []Relationship       `json:"relationships"`
	QualifiedReferences []QualifiedReference `json:"qualified_references"`
	Coverage            Coverage             `json:"coverage"`
}
