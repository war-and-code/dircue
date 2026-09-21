// Package explain turns bounded, retained evidence into deterministic
// explanations. It does not read repositories or repeat classifier decisions.
package explain

const (
	SchemaVersion   = "1.0.0"
	Provider        = "dircue"
	ProviderVersion = "1.0.0"

	MaxSteps       = 32
	MaxOverrides   = 16
	MaxFacts       = 256
	MaxOmissions   = 16
	MaxStringBytes = 8192
)

// Source identifies the selected source session that supplied the evidence.
// Git trees are immutable. Directory consistency is explicitly non-atomic.
type Source struct {
	Mode         string `json:"mode"`
	Tree         string `json:"tree,omitempty"`
	Consistency  string `json:"consistency"`
	ReportSHA256 string `json:"report_sha256,omitempty"`
}

type Query struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

// Scope discloses the module and contextual work needed for the targeted
// answer. Evidence is fresh or retained; neither implies historical identity.
type Scope struct {
	Module    string `json:"module"`
	Engine    string `json:"engine"`
	Evidence  string `json:"evidence"`
	Inventory string `json:"inventory"`
	Content   string `json:"content"`
}

type Extent struct {
	FileBytes       int64 `json:"file_bytes"`
	ReadBytes       int64 `json:"read_bytes"`
	ClassifiedBytes int64 `json:"classified_bytes"`
	ContentComplete bool  `json:"content_complete"`
}

type Limits struct {
	ClassificationBytes int64 `json:"classification_bytes"`
	Steps               int   `json:"steps"`
	Overrides           int   `json:"overrides"`
	Facts               int   `json:"facts"`
	StringBytes         int   `json:"string_bytes"`
}

// Override records the final relevant attribute assignment. Provenance is
// retained or unavailable. A line is reported only for retained provenance.
type Override struct {
	Attribute  string `json:"attribute"`
	Value      string `json:"value"`
	Source     string `json:"source,omitempty"`
	Line       int    `json:"line,omitempty"`
	Provenance string `json:"provenance"`
}

// Step is one actual decision checkpoint in evaluation order.
type Step struct {
	Rule     string `json:"rule"`
	Provider string `json:"provider"`
	Outcome  string `json:"outcome"`
	Evidence string `json:"evidence,omitempty"`
}

type Decision struct {
	Status           string `json:"status"`
	Reason           string `json:"reason"`
	DetectedLanguage string `json:"detected_language,omitempty"`
	ReportedLanguage string `json:"reported_language,omitempty"`
	Strategy         string `json:"strategy,omitempty"`
}

// Fact is retained project, relationship, focus, or availability evidence. It
// states only what the producing module recorded.
type Fact struct {
	Kind      string `json:"kind"`
	State     string `json:"state"`
	Project   string `json:"project,omitempty"`
	Target    string `json:"target,omitempty"`
	Evidence  string `json:"evidence,omitempty"`
	Condition string `json:"condition,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type Omission struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

type Report struct {
	SchemaVersion   string     `json:"schema_version"`
	Provider        string     `json:"provider"`
	ProviderVersion string     `json:"provider_version"`
	ObservationID   string     `json:"observation_id"`
	Status          string     `json:"status"`
	Source          Source     `json:"source"`
	Query           Query      `json:"query"`
	Scope           Scope      `json:"scope"`
	Extent          Extent     `json:"extent"`
	Limits          Limits     `json:"limits"`
	Overrides       []Override `json:"overrides"`
	Facts           []Fact     `json:"facts"`
	Steps           []Step     `json:"steps"`
	Decision        Decision   `json:"decision"`
	Omissions       []Omission `json:"omissions"`
}

// LanguageTrace is filled by the scanner's actual language decision path.
// Call LanguageReport to validate, bound, and identify it for presentation.
type LanguageTrace struct {
	Source              Source
	Path                string
	Scope               Scope
	Extent              Extent
	ClassificationLimit int64
	Overrides           []Override
	Facts               []Fact
	Steps               []Step
	Decision            Decision
	Omissions           []Omission
}
