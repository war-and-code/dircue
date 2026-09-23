package deployables

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"sync"

	"dircue/pkg/profile"
)

// Collector is a concurrent scanner detector. Detect parses each bounded view
// immediately and retains only structural observations, never file content.
type Collector struct {
	mu     sync.Mutex
	report Report
}

func NewCollector(options Options) *Collector {
	limits := normalizedLimits(options)
	return &Collector{report: Report{Provider: "dircue", ProviderVersion: ProviderVersion, Status: "complete", Source: options.Source,
		Selection: "supported-static-declarations-in-selected-regular-files", Limits: limits,
		Definitions: []Definition{}, Diagnostics: []Diagnostic{}, Omissions: map[string]int64{}}}
}

func (*Collector) Name() string { return "deployables" }

func (c *Collector) Detect(ctx context.Context, file profile.File) ([]profile.Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !IsCandidate(file.Path) {
		c.mu.Lock()
		c.report.Coverage.SelectedFiles++
		c.mu.Unlock()
		return nil, nil
	}
	if !validPath(file.Path, c.report.Limits.StringBytes) {
		c.mu.Lock()
		c.report.Coverage.SelectedFiles++
		c.report.Coverage.CandidateFiles++
		c.report.omit("invalid_path", 1, "", "A candidate path was invalid or exceeded the string limit.")
		c.mu.Unlock()
		return nil, nil
	}
	// Scanner detector content is a bounded prefix. Static declarations require
	// a complete view so a truncated document cannot be promoted to evidence.
	if file.Size != int64(len(file.Content)) || file.Size > c.report.Limits.FileBytes {
		c.mu.Lock()
		c.report.Coverage.SelectedFiles++
		c.report.Coverage.CandidateFiles++
		c.report.omit("incomplete_read", 1, file.Path, "The scanner's bounded view did not contain the complete declaration.")
		c.mu.Unlock()
		return nil, nil
	}
	defs, recognized, err := parse(file.Path, file.Content)
	digest := fmt.Sprintf("%x", sha256.Sum256(file.Content))
	c.mu.Lock()
	defer c.mu.Unlock()
	c.report.Coverage.SelectedFiles++
	c.report.Coverage.CandidateFiles++
	c.report.Coverage.ReadFiles++
	c.report.Coverage.InspectedBytes += file.Size
	if err != nil {
		c.report.omit("parse_error", 1, file.Path, err.Error())
		return nil, nil
	}
	if !recognized {
		return nil, nil
	}
	c.report.Coverage.ParsedFiles++
	for i := range defs {
		defs[i].Path, defs[i].SourceSHA256 = file.Path, digest
		defs[i].ID = stableID(defs[i])
		slices.SortFunc(defs[i].Evidence, compareEvidence)
		slices.SortFunc(defs[i].References, compareReference)
		c.report.Definitions = append(c.report.Definitions, defs[i])
	}
	return nil, nil
}

// Finish snapshots the deterministic fragment accumulated by Detect. Calls may
// follow Detect only after scanner workers have joined.
func (c *Collector) Finish() *Report {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.report
	out.Definitions = slices.Clone(c.report.Definitions)
	out.Diagnostics = slices.Clone(c.report.Diagnostics)
	out.Omissions = make(map[string]int64, len(c.report.Omissions))
	for k, v := range c.report.Omissions {
		out.Omissions[k] = v
	}
	slices.SortFunc(out.Definitions, func(a, b Definition) int { return strings.Compare(a.ID, b.ID) })
	if len(out.Definitions) > out.Limits.Definitions {
		out.Omissions["definition_limit"] += int64(len(out.Definitions) - out.Limits.Definitions)
		out.Definitions = out.Definitions[:out.Limits.Definitions]
		out.Status = "partial"
	}
	refs := 0
	for i := range out.Definitions {
		if len(out.Definitions[i].References) > out.Limits.References-refs {
			keep := max(0, out.Limits.References-refs)
			out.Omissions["reference_limit"] += int64(len(out.Definitions[i].References) - keep)
			out.Definitions[i].References = out.Definitions[i].References[:keep]
			out.Definitions[i].Coverage = "qualified"
			out.Status = "partial"
		}
		refs += len(out.Definitions[i].References)
	}
	out.Coverage.RetainedDefinitions = len(out.Definitions)
	out.Coverage.RetainedReferences = refs
	slices.SortFunc(out.Diagnostics, func(a, b Diagnostic) int { return strings.Compare(a.Path+"\x00"+a.Code, b.Path+"\x00"+b.Code) })
	return &out
}

var _ profile.Detector = (*Collector)(nil)
