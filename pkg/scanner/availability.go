package scanner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/war-and-code/dircue/pkg/availability"
	"github.com/war-and-code/dircue/pkg/profile"
)

type availabilityAccumulator struct {
	collector    *availability.Collector
	directory    bool
	maxFileBytes int64
	gitmodules   *job
}

func newAvailabilityAccumulator(opts Options, snapshot *gitSnapshot) *availabilityAccumulator {
	if !opts.Availability {
		return nil
	}
	source := availability.Source{Mode: "directory", Consistency: "live_directory", CheckoutMetadata: "confined_local_metadata"}
	if snapshot != nil {
		source = availability.Source{Mode: "git", Tree: snapshot.tree.Hash.String(), Consistency: "selected_git_tree", CheckoutMetadata: "not_inspected_for_git_tree"}
	}
	return &availabilityAccumulator{collector: availability.New(availability.Options{Source: source, MaxFileBytes: opts.MaxFileBytes}), directory: snapshot == nil, maxFileBytes: opts.MaxFileBytes}
}

func (a *availabilityAccumulator) add(value result, root *os.Root) error {
	if value.gitlink != nil {
		a.collector.AddGitlink(*value.gitlink)
	}
	if value.omission != "" {
		a.collector.MarkInventoryIncomplete(value.omission)
		return nil
	}
	if value.selectedJob == nil {
		return nil
	}
	selected := *value.selectedJob
	if selected.path == ".gitmodules" {
		a.gitmodules = &selected
	}
	attribute := "unknown"
	if selected.attrs.lfsTracked {
		attribute = "tracked"
	}
	a.collector.AddFile(availability.File{Path: selected.path, Size: selected.size, LFSAttribute: attribute, Read: selectedSourceAvailabilityRead(root, selected)})
	return nil
}

func selectedSourceAvailabilityRead(root *os.Root, selected job) availability.Read {
	return func(ctx context.Context, limit int64) ([]byte, int64, error) {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		if selected.read != nil {
			return selected.read(limit)
		}
		if limit <= 0 {
			return []byte{}, selected.size, nil
		}
		content, _, size, err := readBoundedSize(root, selected.path, limit-1)
		return content, size, err
	}
}

func (a *availabilityAccumulator) finish(ctx context.Context, root *os.Root, report *profile.Report) error {
	for _, warning := range report.Warnings {
		if warning.Code == "tree_size_limit" {
			a.collector.MarkInventoryIncomplete("tree_size_limit")
		}
	}
	if a.gitmodules != nil {
		if a.maxFileBytes > 0 && a.gitmodules.size > a.maxFileBytes {
			a.collector.OmitGitmodules(".gitmodules", "gitmodules_file_size_limit")
		} else {
			read := selectedSourceAvailabilityRead(root, *a.gitmodules)
			content, size, err := read(ctx, availability.DefaultGitmodulesBytes)
			if err != nil {
				return fmt.Errorf("read availability .gitmodules: %w", err)
			}
			if int64(len(content)) > availability.DefaultGitmodulesBytes {
				content = content[:availability.DefaultGitmodulesBytes]
			}
			a.collector.AddGitmodules(".gitmodules", content, size)
		}
	}
	if a.directory {
		if err := a.inspectCheckoutMetadata(ctx, root); err != nil {
			return err
		}
	}
	a.addMissingReferences(report)
	result, err := a.collector.Finish(ctx)
	if err != nil {
		return err
	}
	report.Availability = result
	return nil
}

func (a *availabilityAccumulator) addMissingReferences(report *profile.Report) {
	values := map[string]availability.MissingReference{}
	add := func(project, kind, target, evidence, status string) {
		if status != "missing" || target == "" {
			return
		}
		value := availability.MissingReference{Project: project, Kind: kind, Target: target, Evidence: evidence}
		key := strings.Join([]string{project, kind, target, evidence}, "\x00")
		values[key] = value
	}
	if report.Projects != nil {
		for _, project := range report.Projects.Projects {
			for _, ref := range project.References {
				add(project.ID, ref.Kind, ref.Target, ref.Evidence, ref.TargetStatus)
			}
		}
		for _, config := range report.Projects.Configurations {
			for _, ref := range config.References {
				add(config.Path, ref.Kind, ref.Target, ref.Evidence, ref.TargetStatus)
			}
		}
	}
	if report.Declarations != nil {
		for _, project := range report.Declarations.Projects {
			for _, ref := range project.References {
				add(project.ID, ref.Kind, ref.Target, ref.Evidence, ref.TargetStatus)
			}
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		a.collector.AddMissingReference(values[key])
	}
}

func (a *availabilityAccumulator) inspectCheckoutMetadata(ctx context.Context, root *os.Root) error {
	a.collector.MarkCheckoutMetadataInspected()
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := root.Lstat(".git")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect checkout metadata: %w", err)
	}
	if !info.IsDir() {
		a.collector.AddSparseIndication(availability.SparseIndication{Kind: "external_gitdir", Evidence: ".git", Supported: false})
		return nil
	}
	for _, name := range []string{".git/config", ".git/config.worktree"} {
		if err := a.inspectSparseConfig(ctx, root, name); err != nil {
			return err
		}
	}
	if err := a.inspectSparsePatterns(root); err != nil {
		return err
	}
	if err := a.inspectSparseIndex(ctx, root); err != nil {
		return err
	}
	return nil
}

func (a *availabilityAccumulator) inspectSparseConfig(ctx context.Context, root *os.Root, name string) error {
	content, size, found, regular, err := readOptionalMetadata(ctx, root, name, availability.DefaultGitmodulesBytes)
	if err != nil {
		return fmt.Errorf("inspect sparse checkout config: %w", err)
	}
	if !found {
		return nil
	}
	if !regular {
		a.collector.AddSparseIndication(availability.SparseIndication{Kind: "unsupported_config_file", Evidence: name, Supported: false})
		return nil
	}
	a.collector.AddSparseMetadata(availability.InspectSparseConfig(name, content, size, availability.DefaultGitmodulesBytes))
	return nil
}

func (a *availabilityAccumulator) inspectSparsePatterns(root *os.Root) error {
	const name = ".git/info/sparse-checkout"
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect sparse checkout patterns: %w", err)
	}
	a.collector.AddSparseIndication(availability.SparseIndication{Kind: "sparse_patterns_file", Evidence: name, Supported: info.Mode().IsRegular()})
	return nil
}

func (a *availabilityAccumulator) inspectSparseIndex(ctx context.Context, root *os.Root) error {
	const name = ".git/index"
	content, size, found, regular, err := readOptionalMetadata(ctx, root, name, availability.DefaultCheckoutMetadataBytes)
	if err != nil {
		return fmt.Errorf("inspect sparse checkout index: %w", err)
	}
	if !found {
		return nil
	}
	if !regular {
		a.collector.AddSparseIndication(availability.SparseIndication{Kind: "unsupported_index_file", Evidence: name, Supported: false})
		return nil
	}
	a.collector.AddSparseMetadata(availability.InspectGitIndex(name, content, size, availability.DefaultCheckoutMetadataBytes))
	return nil
}

func readOptionalMetadata(ctx context.Context, root *os.Root, name string, limit int64) (content []byte, size int64, found, regular bool, err error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, false, false, err
	}
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, false, false, nil
	}
	if err != nil {
		return nil, 0, false, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, info.Size(), true, false, nil
	}
	if limit <= 0 {
		return []byte{}, info.Size(), true, true, nil
	}
	content, _, size, err = readBoundedSize(root, name, limit-1)
	if int64(len(content)) > limit {
		content = content[:limit]
	}
	return content, size, true, true, err
}
