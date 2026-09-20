package registries

import (
	"context"
	"errors"
	"math"
	"path"
	"slices"
	"strings"
)

type Collector struct {
	report   Report
	pending  map[string][]Candidate
	finished bool
}

func New(source Source, opts Options) (*Collector, error) {
	if opts.MaxFileBytes < 0 || source.Mode != "git" && source.Mode != "directory" {
		return nil, ErrOptions
	}
	if source.Mode == "git" {
		if len(source.Tree) != 40 || strings.Trim(source.Tree, "0123456789abcdef") != "" {
			return nil, ErrOptions
		}
		source.Consistency = "selected_git_tree"
	} else {
		if source.Tree != "" {
			return nil, ErrOptions
		}
		source.Consistency = "live_directory"
	}
	limit := MaxFileBytes
	if opts.MaxFileBytes > 0 {
		limit = min(limit, opts.MaxFileBytes)
	}
	return &Collector{
		pending: map[string][]Candidate{},
		report: Report{
			Status: "complete", Engine: "dircue-registry-declarations", RuleVersion: RuleVersion, Source: source,
			Scope: Scope{
				SupportedConfigurations:         []string{"nuget_config_basename_case_insensitive", "npmrc_basename_exact"},
				Population:                      "selected_regular_files_including_vendor_and_data",
				Evaluation:                      "declarations_only_no_effective_source_set",
				IdentifierDisclosure:            "qualified_identifiers_and_origins_may_reveal_internal_names_not_secret_detection",
				MaxConfigurationsPerEcosystem:   MaxConfigurationsPerEcosystem,
				MaxFileBytes:                    limit,
				MaxDeclarationsPerConfiguration: MaxDeclarationsPerConfiguration,
				MaxDeclarationsPerEcosystem:     MaxDeclarationsPerEcosystem,
			},
			Coverage:       Coverage{EnumerationComplete: true},
			Configurations: []Configuration{}, Omissions: map[string]int64{},
		},
	}, nil
}

// Add must be called once per selected file by the embedding scanner. Only
// admitted candidates retain callbacks; no file content is retained here.
func (c *Collector) Add(file Candidate) error {
	if c.finished {
		return ErrState
	}
	eco, ok := MatchPath(file.Path)
	if !ok {
		return nil
	}
	if c.report.Coverage.CandidateFiles == math.MaxInt64 {
		return ErrOptions
	}
	c.report.Coverage.CandidateFiles++
	if !safePath(file.Path) {
		return c.Omit("unsupported_path", 1)
	}
	if file.Size < 0 || file.Read == nil {
		return ErrCandidate
	}
	files := c.pending[eco]
	i, exists := slices.BinarySearchFunc(files, file.Path, func(a Candidate, p string) int { return strings.Compare(a.Path, p) })
	if exists {
		return ErrCandidate
	}
	if len(files) == MaxConfigurationsPerEcosystem {
		if err := c.Omit("configuration_limit", 1); err != nil {
			return err
		}
		if i == len(files) {
			return nil
		}
		files[len(files)-1] = Candidate{}
		files = files[:len(files)-1]
	}
	c.pending[eco] = slices.Insert(files, i, file)
	return nil
}

// Omit accepts only fixed reason codes. Tree-size omissions do not imply that
// the number of unvisited configuration files is known.
func (c *Collector) Omit(reason string, count int64) error {
	if c.finished {
		return ErrState
	}
	if count < 1 {
		return ErrOptions
	}
	switch reason {
	case "tree_size_limit", "non_regular_file", "unsupported_path", "file_size_limit", "incomplete_content", "configuration_limit":
	default:
		return ErrOptions
	}
	if count > math.MaxInt64-c.report.Omissions[reason] {
		return ErrOptions
	}
	c.report.Status = "partial"
	c.report.Omissions[reason] += count
	if reason == "tree_size_limit" {
		c.report.Coverage.EnumerationComplete = false
	}
	return nil
}

// Finish consumes the selected callbacks once. Read failures return a fixed
// error, without returning a partial report or exposing the callback's message.
func (c *Collector) Finish(ctx context.Context) (*Report, error) {
	if c.finished {
		return nil, ErrState
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.finished = true
	defer func() { clear(c.pending); c.pending = nil }()
	var files []Candidate
	for _, group := range c.pending {
		files = append(files, group...)
	}
	slices.SortFunc(files, func(a, b Candidate) int { return strings.Compare(a.Path, b.Path) })
	retained := map[string]int{}
	c.report.Coverage.AdmittedFiles = int64(len(files))
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		eco, _ := MatchPath(file.Path)
		cfg := configuration(file.Path, eco)
		if file.Size > c.report.Scope.MaxFileBytes {
			cfg.fail("file_size_limit", "not_read")
		} else {
			content, size, err := file.Read(ctx, c.report.Scope.MaxFileBytes+1)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if errors.Is(err, context.Canceled) {
				return nil, context.Canceled
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, context.DeadlineExceeded
			}
			if err != nil {
				return nil, ErrRead
			}
			if int64(len(content)) > c.report.Scope.MaxFileBytes+1 {
				return nil, ErrRead
			}
			c.report.Coverage.ReadFiles++
			c.report.Coverage.BytesRead += int64(len(content))
			if size != file.Size || int64(len(content)) != size || int64(len(content)) > c.report.Scope.MaxFileBytes {
				cfg.fail("incomplete_content", "incomplete")
			} else {
				cfg, err = Parse(file.Path, content)
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				if err != nil {
					return nil, ErrCandidate
				}
				if cfg.DeclarationCountComplete {
					c.report.Coverage.ParsedFiles++
				}
			}
		}
		c.report.Coverage.ObservedDeclarations += cfg.ObservedDeclarations
		available := MaxDeclarationsPerEcosystem - retained[eco]
		if len(cfg.Declarations) > available {
			omitted := len(cfg.Declarations) - available
			cfg.OmittedDeclarations += int64(omitted)
			cfg.Omissions["ecosystem_declaration_limit"] += int64(omitted)
			cfg.Status = "partial"
			clear(cfg.Declarations[available:])
			cfg.Declarations = cfg.Declarations[:available]
		}
		retained[eco] += len(cfg.Declarations)
		c.report.Coverage.RetainedDeclarations += int64(len(cfg.Declarations))
		if cfg.Status != "complete" {
			c.report.Status = "partial"
		}
		c.report.Configurations = append(c.report.Configurations, cfg)
	}
	for i := range c.report.Configurations {
		cfg := &c.report.Configurations[i]
		if cfg.Ecosystem != "nuget" {
			continue
		}
		for _, other := range c.report.Configurations {
			if other.Ecosystem != "nuget" || other.Path == cfg.Path {
				continue
			}
			parent := path.Dir(other.Path)
			child := path.Dir(cfg.Path)
			if parent == "." && child != "." || strings.HasPrefix(child, parent+"/") {
				cfg.AncestorCandidates = append(cfg.AncestorCandidates, other.Path)
			}
		}
	}
	if !c.report.Coverage.EnumerationComplete && len(files) == 0 {
		c.report.Status = "skipped"
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &c.report, nil
}
