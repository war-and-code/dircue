package scanner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"

	"dircue/pkg/codemetrics"
	"dircue/pkg/profile"
)

const DefaultMetricsMaxFileBytes int64 = 16 * 1024 * 1024
const MaxMetricsFileBytes int64 = 256 * 1024 * 1024

// MetricsOptions opts into full-file counting. Source scope follows language
// inclusion; text scope also counts supported data, generated and vendored text.
// MaxFileBytes bounds each counting input, not process RSS. Zero uses the default.
type MetricsOptions struct {
	Scope        string
	MaxFileBytes int64
	IncludeFiles bool
}

func normalizeMetricsOptions(opts MetricsOptions) (MetricsOptions, error) {
	if opts.Scope == "" {
		opts.Scope = "source"
	}
	if opts.Scope != "source" && opts.Scope != "text" {
		return opts, errors.New("metrics scope must be source or text")
	}
	if opts.MaxFileBytes == 0 {
		opts.MaxFileBytes = DefaultMetricsMaxFileBytes
	}
	if opts.MaxFileBytes < 0 || opts.MaxFileBytes > MaxMetricsFileBytes {
		return opts, fmt.Errorf("metrics max file bytes must be between 1 and %d", MaxMetricsFileBytes)
	}
	return opts, nil
}

func newMetricsReport(opts MetricsOptions, snapshot *gitSnapshot) *profile.MetricsReport {
	m := &profile.MetricsReport{
		Engine: "scc", EngineVersion: codemetrics.EngineVersion, Status: "complete", Scope: opts.Scope,
		MaxFileBytes: opts.MaxFileBytes, Source: "directory", Languages: []profile.LanguageMetrics{},
		Directories: []profile.DirectoryMetrics{}, Skipped: []profile.MetricSkip{},
	}
	if snapshot != nil {
		m.Source = "git"
		m.Tree = snapshot.tree.Hash.String()
	}
	if opts.IncludeFiles {
		files := []profile.FileMetrics{}
		m.Files = &files
	}
	return m
}

func countFileMetrics(ctx context.Context, root *os.Root, item job, opts Options, value *result, language string, prefix []byte, size int64, included bool) error {
	m := value.metrics
	m.Language = language
	if opts.Metrics.Scope == "source" && !included {
		return nil
	}
	if item.attrs.lfsTracked && isLFSPointer(prefix) {
		m.Reason = "lfs_pointer"
		return nil
	}
	grammar, ok := codemetrics.Grammar(item.path, language)
	if !ok {
		m.Reason = "unsupported_language"
		return nil
	}
	m.Grammar = grammar
	limit := opts.Metrics.MaxFileBytes
	if size > limit {
		m.Reason = "file_too_large"
		return nil
	}
	content := prefix
	if int64(len(content)) != size {
		var actual int64
		var err error
		if item.read != nil {
			content, actual, err = item.read(limit + 1)
		} else {
			content, _, actual, err = readBoundedSize(root, item.path, limit)
		}
		if err != nil {
			return err
		}
		if actual > limit || int64(len(content)) > limit {
			m.Reason = "file_too_large"
			return nil
		}
		// Both reads must describe the same input. Never count a classification
		// prefix as a full file, or mix a changed file with its previous language.
		if actual != size || int64(len(content)) != actual || !bytes.HasPrefix(content, prefix) {
			m.Reason = "input_changed"
			return nil
		}
	}
	if int64(len(content)) > limit {
		m.Reason = "file_too_large"
		return nil
	}
	counts, err := codemetrics.Count(ctx, item.path, language, content)
	if err != nil {
		switch {
		case errors.Is(err, codemetrics.ErrUnsupported):
			m.Reason = "unsupported_language"
		case errors.Is(err, codemetrics.ErrBinary):
			m.Reason = "binary"
		case errors.Is(err, codemetrics.ErrEncoding):
			m.Reason = "unsupported_encoding"
		default:
			return fmt.Errorf("count %s: %w", item.path, err)
		}
		return nil
	}
	m.Status = "counted"
	m.Reason = ""
	m.Counts = &profile.Counts{Files: 1, Bytes: int64(len(content)), Lines: counts.Lines, Code: counts.Code, Comment: counts.Comment, Blank: counts.Blank, Complexity: counts.Complexity}
	return nil
}

type metricsAccumulator struct {
	report      *profile.MetricsReport
	languages   map[string]*profile.LanguageMetrics
	directories map[string]*profile.DirectoryMetrics
	skipped     map[string]int64
}

func newMetricsAccumulator(report *profile.MetricsReport) *metricsAccumulator {
	return &metricsAccumulator{report: report, languages: make(map[string]*profile.LanguageMetrics), directories: make(map[string]*profile.DirectoryMetrics), skipped: make(map[string]int64)}
}

func addCounts(to *profile.Counts, from profile.Counts) {
	to.Files += from.Files
	to.Bytes += from.Bytes
	to.Lines += from.Lines
	to.Code += from.Code
	to.Comment += from.Comment
	to.Blank += from.Blank
	to.Complexity += from.Complexity
}

func (a *metricsAccumulator) add(value result) {
	for _, warning := range value.warnings {
		if warning.Code == "tree_size_limit" {
			a.report.Status = "skipped"
			a.skipped["tree_size_limit"] = 0
		}
	}
	if value.path == "" {
		return
	}
	m := value.metrics
	if m == nil {
		m = &profile.FileMetrics{Path: value.path, Status: "skipped", Reason: omissionReason(value, "non_regular_file")}
	}
	if a.report.Files != nil {
		*a.report.Files = append(*a.report.Files, *m)
	}
	if m.Status != "counted" {
		a.skipped[m.Reason]++
		switch m.Reason {
		case "outside_scope", "binary", "non_regular_file":
		default:
			if a.report.Status != "skipped" {
				a.report.Status = "partial"
			}
		}
		return
	}
	addCounts(&a.report.Totals, *m.Counts)
	key := m.Language + "\x00" + m.Grammar
	language := a.languages[key]
	if language == nil {
		language = &profile.LanguageMetrics{Language: m.Language, Grammar: m.Grammar}
		a.languages[key] = language
	}
	addCounts(&language.Counts, *m.Counts)
	dir := path.Dir(m.Path)
	directory := a.directories[dir]
	if directory == nil {
		directory = &profile.DirectoryMetrics{Path: dir}
		a.directories[dir] = directory
	}
	addCounts(&directory.Counts, *m.Counts)
}

func (a *metricsAccumulator) finish() {
	for _, language := range a.languages {
		a.report.Languages = append(a.report.Languages, *language)
	}
	slices.SortFunc(a.report.Languages, func(x, y profile.LanguageMetrics) int {
		if n := strings.Compare(x.Language, y.Language); n != 0 {
			return n
		}
		return strings.Compare(x.Grammar, y.Grammar)
	})
	for _, dir := range a.directories {
		a.report.Directories = append(a.report.Directories, *dir)
	}
	slices.SortFunc(a.report.Directories, func(x, y profile.DirectoryMetrics) int { return strings.Compare(x.Path, y.Path) })
	for reason, files := range a.skipped {
		skip := profile.MetricSkip{Reason: reason}
		if reason != "tree_size_limit" {
			skip.Files = &files
		}
		a.report.Skipped = append(a.report.Skipped, skip)
	}
	slices.SortFunc(a.report.Skipped, func(x, y profile.MetricSkip) int { return strings.Compare(x.Reason, y.Reason) })
	if a.report.Files != nil {
		slices.SortFunc(*a.report.Files, func(x, y profile.FileMetrics) int { return strings.Compare(x.Path, y.Path) })
	}
}
