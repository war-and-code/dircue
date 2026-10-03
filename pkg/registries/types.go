// Package registries observes selected registry configuration declarations.
// It never opens files, expands variables, contacts registries, or evaluates feeds.
package registries

import (
	"context"
	"errors"
)

const (
	RuleVersion                           = "1.1.0"
	MaxConfigurationsPerEcosystem         = 64
	MaxFileBytes                    int64 = 256 * 1024
	MaxDeclarationsPerConfiguration       = 256
	MaxDeclarationsPerEcosystem           = 1024
	MaxXMLDepth                           = 32
	MaxXMLTokens                          = 32768
	MaxTOMLDepth                          = 32
	MaxTOMLTokens                         = 32768
	MaxNPMLineBytes                       = 8192
	MaxMavenTextBytes                     = 8192
)

var (
	ErrOptions   = errors.New("invalid registry inventory options")
	ErrCandidate = errors.New("invalid registry configuration candidate")
	ErrState     = errors.New("registry collector already finished")
	ErrRead      = errors.New("cannot read selected registry configuration")
)

type Source struct {
	Mode        string `json:"mode"`
	Tree        string `json:"tree,omitempty"`
	Consistency string `json:"consistency"`
}
type Options struct{ MaxFileBytes int64 }

// Read receives a maximum byte count; its returned byte slice must not exceed
// that count. Its separate integer return is the full selected-file size, which
// may exceed the byte slice length. The caller owns reader/root lifetimes. Callbacks
// are trusted embedding code; the package itself performs no filesystem I/O.
type Candidate struct {
	Path string
	Size int64
	Read func(context.Context, int64) ([]byte, int64, error)
}

type Scope struct {
	SupportedConfigurations         []string `json:"supported_configurations"`
	Population                      string   `json:"population"`
	Evaluation                      string   `json:"evaluation"`
	ExternalConfiguration           bool     `json:"external_configuration"`
	EnvironmentExpansion            bool     `json:"environment_expansion"`
	NetworkAccess                   bool     `json:"network_access"`
	IdentifierDisclosure            string   `json:"identifier_disclosure"`
	MaxConfigurationsPerEcosystem   int      `json:"max_configurations_per_ecosystem"`
	MaxFileBytes                    int64    `json:"max_file_bytes"`
	MaxDeclarationsPerConfiguration int      `json:"max_declarations_per_configuration"`
	MaxDeclarationsPerEcosystem     int      `json:"max_declarations_per_ecosystem"`
}
type Coverage struct {
	CandidateFiles       int64 `json:"candidate_files"`
	AdmittedFiles        int64 `json:"admitted_files"`
	ReadFiles            int64 `json:"read_files"`
	ParsedFiles          int64 `json:"parsed_files"`
	BytesRead            int64 `json:"bytes_read"`
	ObservedDeclarations int64 `json:"observed_declarations"`
	RetainedDeclarations int64 `json:"retained_declarations"`
	EnumerationComplete  bool  `json:"enumeration_complete"`
}
type Report struct {
	Status         string           `json:"status"`
	Engine         string           `json:"engine"`
	RuleVersion    string           `json:"rule_version"`
	Source         Source           `json:"source"`
	Scope          Scope            `json:"scope"`
	Coverage       Coverage         `json:"coverage"`
	Configurations []Configuration  `json:"configurations"`
	Omissions      map[string]int64 `json:"omissions"`
}
type Configuration struct {
	Path                     string           `json:"path"`
	Ecosystem                string           `json:"ecosystem"`
	Status                   string           `json:"status"`
	SyntaxStatus             string           `json:"syntax_status"`
	AncestorCandidates       []string         `json:"ancestor_candidates"`
	ObservedDeclarations     int64            `json:"observed_declarations"`
	OmittedDeclarations      int64            `json:"omitted_declarations"`
	DeclarationCountComplete bool             `json:"declaration_count_complete"`
	Declarations             []Declaration    `json:"declarations"`
	Omissions                map[string]int64 `json:"omissions"`
}
type Declaration struct {
	Index         int64     `json:"index"`
	Section       string    `json:"section"`
	Operation     string    `json:"operation"`
	Semantics     string    `json:"semantics"`
	Applicability string    `json:"applicability,omitempty"`
	Name          *Label    `json:"name,omitempty"`
	Scope         *Label    `json:"scope,omitempty"`
	Pattern       *Label    `json:"pattern,omitempty"`
	Endpoint      *Endpoint `json:"endpoint,omitempty"`
	Disabled      *bool     `json:"disabled,omitempty"`
}
type Label struct {
	Status string `json:"status"`
	Value  string `json:"value,omitempty"`
}
type Endpoint struct {
	Status string `json:"status"`
	Origin string `json:"origin,omitempty"`
}

func configuration(path, ecosystem string) Configuration {
	return Configuration{Path: path, Ecosystem: ecosystem, Status: "complete", SyntaxStatus: "complete", AncestorCandidates: []string{}, DeclarationCountComplete: true, Declarations: []Declaration{}, Omissions: map[string]int64{}}
}
func (c *Configuration) omit(reason string) { c.Status = "partial"; c.Omissions[reason]++ }
func (c *Configuration) add(d Declaration) {
	c.ObservedDeclarations++
	d.Index = c.ObservedDeclarations
	if len(c.Declarations) == MaxDeclarationsPerConfiguration {
		c.OmittedDeclarations++
		c.omit("declaration_limit")
		return
	}
	c.Declarations = append(c.Declarations, d)
}
func (c *Configuration) fail(code, status string) {
	c.SyntaxStatus = status
	c.DeclarationCountComplete = false
	c.Declarations = []Declaration{}
	c.ObservedDeclarations = 0
	c.OmittedDeclarations = 0
	c.Omissions = map[string]int64{}
	c.omit(code)
}
