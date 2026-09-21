package scanner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"slices"

	"dircue/pkg/declarations"
	"dircue/pkg/environments"
	"dircue/pkg/profile"
)

// environmentAccumulator retains selected configuration readers, never paths
// to reopen through a different source selection.
type environmentAccumulator struct {
	jobs              map[string]job
	files             map[string]environments.File
	maxFileBytes      int64
	inventoryComplete bool
	inventoryOmission string
}

func newEnvironmentAccumulator(opts Options) *environmentAccumulator {
	if !opts.Environments {
		return nil
	}
	return &environmentAccumulator{jobs: make(map[string]job), files: make(map[string]environments.File), maxFileBytes: opts.MaxFileBytes, inventoryComplete: true}
}

func (a *environmentAccumulator) add(value result) error {
	for _, warning := range value.warnings {
		if warning.Code == "tree_size_limit" {
			a.inventoryComplete = false
			a.inventoryOmission = "tree_size_limit"
		}
	}
	if value.omission != "" {
		a.inventoryComplete = false
		a.inventoryOmission = value.omission
	}
	base := path.Base(value.path)
	if base != "global.json" && base != "Directory.Build.props" {
		return nil
	}
	if len(a.files) >= environments.DefaultMaxInventoryPaths {
		return errors.New("environment configuration inventory limit reached")
	}
	if _, ok := a.files[value.path]; ok {
		return errors.New("duplicate environment configuration path")
	}
	file := environments.File{Path: value.path, NonRegular: value.selectedJob == nil}
	if value.selectedJob != nil && base == "global.json" {
		item := *value.selectedJob
		file.Size = item.size
		a.jobs[value.path] = item
	}
	a.files[value.path] = file
	return nil
}

func (a *environmentAccumulator) finish(ctx context.Context, root *os.Root, collector *declarations.Collector, report *profile.Report) error {
	if !a.inventoryComplete {
		source, tree := "directory", ""
		if report.Declarations != nil {
			source, tree = report.Declarations.Source, report.Declarations.Tree
		}
		reason := a.inventoryOmission
		if reason == "" {
			reason = "inventory_incomplete"
		}
		report.Environments = environments.Skip(source, tree, reason)
		return nil
	}
	if collector == nil || report.Declarations == nil {
		return errors.New("environments require declaration evidence")
	}
	paths := make([]string, 0, len(a.files))
	for name := range a.files {
		paths = append(paths, name)
	}
	slices.Sort(paths)
	inventory := make([]environments.File, 0, len(paths))
	for _, name := range paths {
		inventory = append(inventory, a.files[name])
	}
	limits := environments.Limits{}
	if a.maxFileBytes > 0 && a.maxFileBytes < environments.DefaultMaxGlobalJSONBytes {
		limits.GlobalJSONBytes = a.maxFileBytes
	}
	input := environments.Input{
		Source: report.Declarations.Source, Tree: report.Declarations.Tree,
		Inventory: inventory, InventoryComplete: true,
		Declarations: *report.Declarations, ProjectRecords: collector.ProjectRecords(),
		ReadSelected: func(ctx context.Context, name string, limit int64) ([]byte, int64, error) {
			selected, ok := a.jobs[name]
			if !ok {
				return nil, 0, errors.New("environment configuration is not a selected regular file")
			}
			return selectedSourceAvailabilityRead(root, selected)(ctx, limit)
		},
	}
	var err error
	report.Environments, err = environments.Analyze(ctx, input, limits)
	if err != nil {
		return fmt.Errorf("analyze environments: %w", err)
	}
	return nil
}
