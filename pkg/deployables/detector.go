package deployables

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/war-and-code/dircue/pkg/profile"
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
	dirs         map[string]bool
	selected     map[string]bool
}

type pendingFile struct {
	path    string
	content []byte
}

func NewCollector(options Options) *Collector {
	limits := normalizedLimits(options)
	return &Collector{report: Report{Provider: "dircue", ProviderVersion: ProviderVersion, Status: "complete", Source: options.Source,
		Selection: "supported-static-declarations-in-selected-regular-files", Limits: limits,
		Definitions: []Definition{}, Diagnostics: []Diagnostic{}, Omissions: map[string]int64{}}, dirs: map[string]bool{}, selected: map[string]bool{}}
}

func (*Collector) Name() string { return "deployables" }

func (c *Collector) Detect(ctx context.Context, file profile.File) ([]profile.Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	addDirectories(c.dirs, file.Path)
	c.selected[file.Path] = true
	c.mu.Unlock()
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
	out.Directories = c.dirs
	selectedFiles := make(map[string]bool, len(c.selected))
	for name, present := range c.selected {
		selectedFiles[name] = present
	}
	out.Definitions = slices.Clone(c.report.Definitions)
	out.Diagnostics = slices.Clone(c.report.Diagnostics)
	out.Omissions = make(map[string]int64, len(c.report.Omissions))
	for k, v := range c.report.Omissions {
		out.Omissions[k] = v
	}
	if c.budgetDrops > 0 {
		out.omit("selection_budget", c.budgetDrops, "", "Only the lexically first declaration candidates within the file and input-byte budgets were parsed.")
	}
	var privateContexts []BuildContext
	for _, file := range c.pending {
		out.Coverage.ReadFiles++
		out.Coverage.InspectedBytes += int64(len(file.content))
		defs, recognized, err := parse(file.path, file.content)
		if err != nil {
			var procfileErr *procfileIssuesError
			if errors.As(err, &procfileErr) {
				recordProcfileIssues(&out, file.path, procfileErr)
			} else {
				var limitErr *yamlDocLimitError
				if errors.As(err, &limitErr) {
					out.omit("yaml_document_limit", 1, file.path, "File has more than 128 YAML documents; only the first 128 were parsed.")
					// fall through and use partial defs
				} else {
					out.omit("parse_error", 1, file.path, err.Error())
					continue
				}
			}
		}
		if !recognized {
			continue
		}
		out.Coverage.ParsedFiles++
		digest := fmt.Sprintf("%x", sha256.Sum256(file.content))
		if unresolved := resolveProcfileTargets(defs, selectedFiles); unresolved > 0 {
			out.omit("procfile_target_unresolved", unresolved, file.path, "Some Procfile targets did not resolve to one selected source file.")
		}
		for i := range defs {
			defs[i].Path, defs[i].SourceSHA256 = file.path, digest
			defs[i].ID = stableID(defs[i])
			slices.SortFunc(defs[i].Evidence, compareEvidence)
			slices.SortFunc(defs[i].References, compareReference)
			if defs[i].Provider == "makefile" {
				var fileRef, contextRef *Reference
				for j := range defs[i].References {
					ref := &defs[i].References[j]
					if ref.Kind == "dockerfile" && ref.Qualification == "local" {
						fileRef = ref
					}
					if ref.Kind == "build_context" && ref.Qualification == "local" {
						contextRef = ref
					}
				}
				if fileRef != nil && contextRef != nil {
					privateContexts = append(privateContexts, BuildContext{SourcePath: file.path, Context: contextRef.Value, Dockerfile: fileRef.Value, ContextEvidence: contextRef.Evidence, FileEvidence: fileRef.Evidence})
				}
				continue
			}
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
	for _, binding := range privateContexts {
		if refs+2 > out.Limits.References {
			out.Omissions["reference_limit"]++
			out.Status = "partial"
			continue
		}
		out.BuildContexts = append(out.BuildContexts, binding)
		refs += 2
	}
	out.Coverage.RetainedReferences = refs
	out.Coverage.RetainedDefinitions = len(out.Definitions)
	out.Coverage.RetainedReferences = refs
	slices.SortFunc(out.Diagnostics, func(a, b Diagnostic) int { return strings.Compare(a.Path+"\x00"+a.Code, b.Path+"\x00"+b.Code) })
	return &out
}

var _ profile.Detector = (*Collector)(nil)
