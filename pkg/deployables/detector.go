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

// Collector is a concurrent scanner detector. It retains the lexically first
// declaration views within its input budget and parses them after scanning.
type Collector struct {
	mu           sync.Mutex
	report       Report
	pending      []pendingFile
	pendingBytes int64
	budgetDrops  int64
	cutoff       string
}

type pendingFile struct {
	path    string
	content []byte
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
		c.report.omit("invalid_path", 1, bounded(file.Path), "A candidate path was invalid or exceeded the string limit.")
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
	c.mu.Lock()
	defer c.mu.Unlock()
	c.report.Coverage.SelectedFiles++
	c.report.Coverage.CandidateFiles++
	if c.cutoff != "" && file.Path >= c.cutoff {
		c.budgetDrops++
		return nil, nil
	}
	item := pendingFile{path: file.Path, content: append([]byte(nil), file.Content...)}
	index, _ := slices.BinarySearchFunc(c.pending, item, func(a, b pendingFile) int { return strings.Compare(a.path, b.path) })
	c.pending = slices.Insert(c.pending, index, item)
	c.pendingBytes += file.Size
	for len(c.pending) > c.report.Limits.Files || c.pendingBytes > c.report.Limits.InputBytes {
		last := len(c.pending) - 1
		if c.cutoff == "" || c.pending[last].path < c.cutoff {
			c.cutoff = c.pending[last].path
		}
		c.pendingBytes -= int64(len(c.pending[last].content))
		c.pending[last].content = nil
		c.pending = c.pending[:last]
		c.budgetDrops++
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
	if c.budgetDrops > 0 {
		out.omit("selection_budget", c.budgetDrops, "", "Only the lexically first declaration candidates within the file and input-byte budgets were parsed.")
	}
	for _, file := range c.pending {
		out.Coverage.ReadFiles++
		out.Coverage.InspectedBytes += int64(len(file.content))
		defs, recognized, err := parse(file.path, file.content)
		if err != nil {
			out.omit("parse_error", 1, file.path, err.Error())
			continue
		}
		if !recognized {
			continue
		}
		out.Coverage.ParsedFiles++
		digest := fmt.Sprintf("%x", sha256.Sum256(file.content))
		for i := range defs {
			defs[i].Path, defs[i].SourceSHA256 = file.path, digest
			defs[i].ID = stableID(defs[i])
			slices.SortFunc(defs[i].Evidence, compareEvidence)
			slices.SortFunc(defs[i].References, compareReference)
			out.Definitions = append(out.Definitions, defs[i])
		}
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
