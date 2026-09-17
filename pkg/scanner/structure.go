package scanner

import (
	"context"
	"dircue/pkg/profile"
	"dircue/pkg/structure"
	"fmt"
	"os"
	"slices"
	"strings"
)

func newStructureReport(opts Options, snapshot *gitSnapshot) *profile.StructureReport {
	r := &profile.StructureReport{Engine: "big-code-analysis", EngineVersion: "2.2.0", Status: "complete", Scope: "source", Source: "directory", MaxFileBytes: opts.Structure.MaxFileBytes(), Omissions: map[string]int64{}, Observations: map[string]uint64{}, SupportedLanguages: structure.Capabilities(), ObservationFiles: map[string]int64{}}
	if snapshot != nil {
		r.Source = "git"
		r.Tree = snapshot.tree.Hash.String()
	}
	if opts.StructureFiles {
		f := []structure.File{}
		r.Files = &f
	}
	return r
}
func countStructure(ctx context.Context, root *os.Root, item job, opts Options, value *result, language string, prefix []byte, size int64, included bool) error {
	f := structure.File{Path: item.path, Language: language, SourceBytes: size, Status: "skipped"}
	value.structural = &f
	if !included {
		f.Reason = "outside_scope"
		return nil
	}
	if !structure.Supports(language) {
		f.Reason = "unsupported_language"
		return nil
	}
	limit := opts.Structure.MaxFileBytes()
	if size > limit {
		f.Reason = "file_too_large"
		return nil
	}
	select {
	case opts.structureGate <- struct{}{}:
		defer func() { <-opts.structureGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	data := prefix
	if int64(len(data)) != size {
		var actual int64
		var err error
		if item.read != nil {
			data, actual, err = item.read(limit + 1)
		} else {
			data, _, actual, err = readBoundedSize(root, item.path, limit)
		}
		if err != nil {
			return err
		}
		if actual > limit || int64(len(data)) > limit {
			f.Reason = "file_too_large"
			return nil
		}
		if actual != size || int64(len(data)) != size {
			f.Reason = "file_changed"
			return nil
		}
	}
	observed, err := opts.Structure.Analyze(ctx, item.path, language, data)
	if err != nil {
		return fmt.Errorf("structure %s: %w", item.path, err)
	}
	value.structural = &observed
	return nil
}
func addStructure(r *profile.StructureReport, value result) {
	for _, w := range value.warnings {
		if w.Code == "tree_size_limit" {
			r.Status = "skipped"
			r.Omissions["tree_size_limit"]++
			return
		}
	}
	if value.path == "" {
		return
	}
	f := value.structural
	if f == nil {
		reason := "outside_scope"
		for _, warning := range value.warnings {
			if warning.Code == "file_too_large" || warning.Code == "non_regular_file" {
				reason = warning.Code
			}
		}
		f = &structure.File{Path: value.path, Status: "skipped", Reason: reason, SourceBytes: value.size}
	}
	if f.Status == "skipped" {
		r.Omissions[f.Reason]++
	} else {
		r.AnalyzedFiles++
		r.ParseCount += int64(f.ParseCount)
		if f.Status == "partial" {
			r.PartialFiles++
		}
		for k, v := range f.Observations {
			r.Observations[k] += v
			r.ObservationFiles[k]++
		}
	}
	if r.Files != nil {
		*r.Files = append(*r.Files, *f)
	}
}
func finishStructure(r *profile.StructureReport) {
	if r.Status == "skipped" {
		return
	}
	if r.PartialFiles > 0 {
		r.Status = "partial"
	}
	for reason := range r.Omissions {
		if reason != "outside_scope" {
			r.Status = "partial"
		}
	}
	if r.AnalyzedFiles == 0 && r.Status == "complete" {
		r.Status = "skipped"
	}
	if r.Files != nil {
		slices.SortFunc(*r.Files, func(a, b structure.File) int { return strings.Compare(a.Path, b.Path) })
	}
}
