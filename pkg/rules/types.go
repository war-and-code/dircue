// Package rules evaluates explicit caller-supplied observation rules without
// filesystem access, repository configuration discovery, or code execution.
package rules

import "errors"

const (
	ConfigVersion     = "1.0.0"
	EvaluatorVersion  = "1.0.0"
	MaxConfigBytes    = 256 << 10
	MaxRules          = 256
	MaxSelectorValues = 64
	MaxTotalSelectors = 4096
	MaxLiteralBytes   = 4096
	MaxPathBytes      = 4096
	MaxContentBytes   = 64 << 10
	MaxContentFiles   = 256
	MaxObservations   = 1024
)

var (
	ErrConfig          = errors.New("invalid observation rules")
	ErrFile            = errors.New("invalid rule input file")
	ErrContentTooLarge = errors.New("rule content exceeds complete-file limit")
	ErrIncomplete      = errors.New("rule content is incomplete or changed")
	ErrInvalidUTF8     = errors.New("rule content is not valid UTF-8")
	ErrNotCandidate    = errors.New("file is not a content-rule candidate")
	ErrSequence        = errors.New("invalid rule collector sequence")
	ErrCounter         = errors.New("rule counter overflow")
	ErrContext         = errors.New("invalid rule collector context")
)

// File describes one selected regular file. Path uses clean, relative slash
// separators; Size is the complete selected file size, not a prefix length.
type File struct {
	Path string
	Size int64
}

type Observation struct {
	Path         string `json:"path"`
	RuleID       string `json:"rule_id"`
	Evidence     string `json:"evidence"`
	Size         int64  `json:"size"`
	SourceSHA256 string `json:"source_sha256,omitempty"`
}

// Decision contains metadata matches and the rules that still require content.
// The caller owns these slices. Changing them cannot modify the Program.
type Decision struct {
	Matches        []Observation
	ContentRuleIDs []string
}

func (d Decision) NeedsContent() bool { return len(d.ContentRuleIDs) != 0 }

type Source struct {
	Kind string `json:"kind"`
	Tree string `json:"tree,omitempty"`
}

type Limits struct {
	ConfigBytes        int   `json:"config_bytes"`
	Rules              int   `json:"rules"`
	SelectorValues     int   `json:"selector_values"`
	TotalSelectors     int   `json:"total_selectors"`
	LiteralBytes       int   `json:"literal_bytes"`
	PathBytes          int   `json:"path_bytes"`
	ContentBytes       int   `json:"content_bytes"`
	ContentFiles       int   `json:"content_files"`
	Observations       int   `json:"observations"`
	CallerMaxFileBytes int64 `json:"caller_max_file_bytes"`
}

func EffectiveLimits() Limits {
	return Limits{ConfigBytes: MaxConfigBytes, Rules: MaxRules, SelectorValues: MaxSelectorValues,
		TotalSelectors: MaxTotalSelectors, LiteralBytes: MaxLiteralBytes, PathBytes: MaxPathBytes,
		ContentBytes: MaxContentBytes, ContentFiles: MaxContentFiles, Observations: MaxObservations}
}

type Options struct {
	MaxFileBytes int64
}

type RuleSummary struct {
	ID                  string `json:"id"`
	RequiresContent     bool   `json:"requires_content"`
	CandidateFiles      uint64 `json:"candidate_files"`
	EvaluatedFiles      uint64 `json:"evaluated_files"`
	MatchedFiles        uint64 `json:"matched_files"`
	ContentOmittedFiles uint64 `json:"content_omitted_files"`
}

type Report struct {
	Provider              string                    `json:"provider"`
	EvaluatorVersion      string                    `json:"evaluator_version"`
	RulesSchemaVersion    string                    `json:"rules_schema_version"`
	RulesSHA256           string                    `json:"rules_sha256"`
	Source                Source                    `json:"source"`
	SourceConsistency     string                    `json:"source_consistency"`
	Scope                 string                    `json:"scope"`
	Status                string                    `json:"status"`
	Order                 string                    `json:"order"`
	Limits                Limits                    `json:"limits"`
	InventoryFiles        uint64                    `json:"inventory_files"`
	CandidateFiles        uint64                    `json:"candidate_files"`
	ContentCandidateFiles uint64                    `json:"content_candidate_files"`
	ContentEligibleFiles  uint64                    `json:"content_eligible_files"`
	ContentAdmittedFiles  uint64                    `json:"content_admitted_files"`
	ContentEvaluatedFiles uint64                    `json:"content_evaluated_files"`
	ContentOmittedFiles   uint64                    `json:"content_omitted_files"`
	TotalMatches          uint64                    `json:"total_matches"`
	OmittedMatches        uint64                    `json:"omitted_matches"`
	Omissions             map[OmissionReason]uint64 `json:"omissions"`
	Rules                 []RuleSummary             `json:"rules"`
	Observations          []Observation             `json:"observations"`
}

type OmissionReason string

const (
	NonRegularFile  OmissionReason = "non_regular_file"
	UnsupportedPath OmissionReason = "unsupported_path"
	TreeSizeLimit   OmissionReason = "tree_size_limit"
	FileTooLarge    OmissionReason = "file_too_large"
	CallerFileLimit OmissionReason = "caller_file_limit"
	ContentBudget   OmissionReason = "content_budget"
	Incomplete      OmissionReason = "incomplete_content"
	InvalidUTF8     OmissionReason = "invalid_utf8"
	NotEvaluated    OmissionReason = "not_evaluated"
)
