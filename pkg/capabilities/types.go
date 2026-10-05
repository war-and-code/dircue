// Package capabilities describes the small, versioned set of dircue modules
// understood by the saved-report planner. It does not inspect the host or PATH.
package capabilities

import (
	"slices"

	"github.com/war-and-code/dircue/pkg/codemetrics"
	"github.com/war-and-code/dircue/pkg/structure"
)

const SchemaVersion = "1.0.0"

type Command struct {
	ArgvPrefix []string `json:"argv_prefix"`
}

type Cost struct {
	Inspection      string `json:"inspection"`
	ExternalProcess bool   `json:"external_process"`
}

type Module struct {
	ID                   string   `json:"id"`
	Question             string   `json:"question"`
	Command              Command  `json:"command"`
	Prerequisites        []string `json:"prerequisites"`
	RequiredInputs       []string `json:"required_inputs"`
	ExpectedEvidence     []string `json:"expected_evidence"`
	AggregateSchemaSince string   `json:"aggregate_schema_since"`
	ProviderVersion      string   `json:"provider_version"`
	Cost                 Cost     `json:"cost"`
	SupportedLanguages   []string `json:"supported_languages"`
}

type Descriptor struct {
	SchemaVersion   string   `json:"schema_version"`
	Kind            string   `json:"kind"`
	Provider        string   `json:"provider"`
	ProviderVersion string   `json:"provider_version"`
	Scope           string   `json:"scope"`
	Modules         []Module `json:"modules"`
}

// Dircue returns a static registry. providerVersion is supplied by the caller
// so release builds report their actual producer version.
func Dircue(providerVersion string) Descriptor {
	modules := []Module{
		{ID: "assessment", Question: "repository-measurements", Command: Command{[]string{"dircue", "analyze", "assessment", "--json"}}, RequiredInputs: []string{"source"}, ExpectedEvidence: []string{"selected files and logical bytes", "parsed project and relationship populations", "lockfile associations and metric completeness"}, AggregateSchemaSince: "1.9.0", ProviderVersion: "1.0.0", Cost: Cost{"metadata-and-bounded-content", false}},
		{ID: "availability", Question: "source-availability", Command: Command{[]string{"dircue", "analyze", "availability", "--json"}}, RequiredInputs: []string{"source"}, ExpectedEvidence: []string{"acquisition boundaries", "coverage and omissions"}, AggregateSchemaSince: "1.6.0", ProviderVersion: "1.0.0", Cost: Cost{"bounded-content", false}},
		{ID: "declarations", Question: "project-declarations", Command: Command{[]string{"dircue", "analyze", "declarations", "--json"}}, RequiredInputs: []string{"source"}, ExpectedEvidence: []string{"parsed project declarations", "requirements and interfaces", "coverage and omissions"}, AggregateSchemaSince: "1.4.0", ProviderVersion: "1.0.0", Cost: Cost{"bounded-content", false}},
		{ID: "discovery", Question: "content-inventory", Command: Command{[]string{"dircue", "analyze", "discovery", "--json"}}, RequiredInputs: []string{"source"}, ExpectedEvidence: []string{"metadata inventory", "candidate paths", "coverage and omissions"}, AggregateSchemaSince: "1.3.0", ProviderVersion: "1.0.0", Cost: Cost{"metadata", false}},
		{ID: "environments", Question: "environments", Command: Command{[]string{"dircue", "analyze", "environments", "--json"}}, RequiredInputs: []string{"source"}, ExpectedEvidence: []string{"declared environment constraints", "provider and coverage"}, AggregateSchemaSince: "1.7.0", ProviderVersion: "1.1.0", Cost: Cost{"bounded-content", false}},
		{ID: "focus", Question: "project-scope", Command: Command{[]string{"dircue", "analyze", "focus", "--json"}}, Prerequisites: []string{"declarations"}, RequiredInputs: []string{"source", "project"}, ExpectedEvidence: []string{"qualified project population", "context and boundaries"}, AggregateSchemaSince: "1.6.0", ProviderVersion: "1.0.0", Cost: Cost{"metadata-and-bounded-content", false}},
		{ID: "formats", Question: "content-formats", Command: Command{[]string{"dircue", "analyze", "formats", "--json"}}, RequiredInputs: []string{"source"}, ExpectedEvidence: []string{"bounded signature observations", "read coverage and omissions"}, AggregateSchemaSince: "1.5.0", ProviderVersion: "1.0.0", Cost: Cost{"bounded-content", false}},
		{ID: "lockfiles", Question: "lockfile-declarations", Command: Command{[]string{"dircue", "analyze", "lockfiles", "--json"}}, RequiredInputs: []string{"source"}, ExpectedEvidence: []string{"manifest-lockfile associations", "named static declaration checks", "coverage and boundaries"}, AggregateSchemaSince: "1.8.0", ProviderVersion: "1.0.0", Cost: Cost{"bounded-content", false}},
		{ID: "metrics", Question: "code-metrics", Command: Command{[]string{"dircue", "analyze", "metrics", "--json"}}, RequiredInputs: []string{"source"}, ExpectedEvidence: []string{"line and lexical complexity counts", "counting coverage"}, AggregateSchemaSince: "1.1.0", ProviderVersion: codemetrics.EngineVersion, Cost: Cost{"full-content", false}},
		{ID: "structure", Question: "source-structure", Command: Command{[]string{"dircue", "analyze", "structure", "--json"}}, RequiredInputs: []string{"source", "structural-worker"}, ExpectedEvidence: []string{"syntax observations", "parser coverage and omissions"}, AggregateSchemaSince: "1.2.0", ProviderVersion: "worker-reported", Cost: Cost{"full-content", true}},
	}
	for i := range modules {
		if modules[i].Prerequisites == nil {
			modules[i].Prerequisites = []string{}
		}
		if modules[i].RequiredInputs == nil {
			modules[i].RequiredInputs = []string{}
		}
		if modules[i].ExpectedEvidence == nil {
			modules[i].ExpectedEvidence = []string{}
		}
		if modules[i].SupportedLanguages == nil {
			modules[i].SupportedLanguages = []string{}
		}
	}
	for i := range modules {
		if modules[i].ID == "structure" {
			for _, c := range structure.Capabilities() {
				modules[i].SupportedLanguages = append(modules[i].SupportedLanguages, c.Language)
			}
		}
	}
	slices.SortFunc(modules, func(a, b Module) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return Descriptor{SchemaVersion, "dircue-planner-capabilities", "dircue", providerVersion, "planner-supported-modules", modules}
}
