// Package providerjoin ingests bounded, caller-supplied analyzer reports and
// joins their neutral facts and run coverage to a dircue map. It never runs a
// provider and deliberately ignores findings, severities, and verdicts.
package providerjoin

import (
	"context"

	"dircue/pkg/mapdoc"
)

const Version = "1.0.0"

type Attachment struct {
	Kind string
	Path string
}

type Snapshot struct {
	Mode           string
	Tree           string
	Commit         string
	Digest         *mapdoc.Digest
	DigestComplete bool
	CallerAsserted bool
}

type Input struct {
	Root     string
	Snapshot Snapshot
	Nodes    []mapdoc.Node
	Edges    []mapdoc.Edge
}

type Options struct {
	MaxReportBytes int64
	MaxRecords     int
}

type Diagnostic struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type Binding string

const (
	BindingVerified       Binding = "verified"
	BindingMismatch       Binding = "mismatch"
	BindingUnknown        Binding = "unknown"
	BindingCallerAsserted Binding = "caller_asserted"
)

type CoverageEntry struct {
	Tool         string   `json:"tool"`
	ReportKind   string   `json:"report_kind"`
	Scope        string   `json:"scope"`
	Binding      Binding  `json:"binding"`
	Ran          bool     `json:"ran"`
	CoveredFiles []string `json:"covered_files"`
	State        string   `json:"state"`
	Reason       string   `json:"reason"`
}

type Prerequisite struct {
	Name     string `json:"name"`
	Observed bool   `json:"observed"`
	Reason   string `json:"reason,omitempty"`
}

type Plan struct {
	Tool          string         `json:"tool"`
	ComponentID   string         `json:"component_id,omitempty"`
	Applicable    bool           `json:"applicable"`
	Reason        string         `json:"reason"`
	Prerequisites []Prerequisite `json:"prerequisites"`
	Scope         string         `json:"scope"`
	Argv          []string       `json:"argv"`
	ReportKind    string         `json:"report_kind"`
}

type Descriptor struct {
	Tool              string   `json:"tool"`
	Version           string   `json:"version"`
	Languages         []string `json:"languages"`
	RequiresFramework bool     `json:"requires_framework,omitempty"`
	ReportKind        string   `json:"report_kind"`
}

type Result struct {
	Nodes       []mapdoc.Node             `json:"nodes"`
	Edges       []mapdoc.Edge             `json:"edges"`
	Coverage    []mapdoc.QuestionCoverage `json:"coverage"`
	Diagnostics []Diagnostic              `json:"diagnostics"`
	Ledger      []CoverageEntry           `json:"ledger"`
	Plans       []Plan                    `json:"plans"`
}

func Join(ctx context.Context, in Input, attachments []Attachment, opts Options) (Result, error) {
	return join(ctx, in, attachments, opts)
}
