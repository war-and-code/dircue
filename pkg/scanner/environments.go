package scanner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"slices"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/environments"
	"github.com/war-and-code/dircue/pkg/profile"
)

// environmentAccumulator retains selected configuration readers, never paths
// to reopen through a different source selection.
type environmentAccumulator struct {
	jobs              map[string]job
	files             map[string]environments.File
	maxFileBytes      int64
	inventoryComplete bool
	inventoryOmission string
	errorPolicy       string
}

func newEnvironmentAccumulator(opts Options) *environmentAccumulator {
	if !opts.Environments {
		return nil
	}
	return &environmentAccumulator{jobs: make(map[string]job), files: make(map[string]environments.File), maxFileBytes: opts.MaxFileBytes, inventoryComplete: true, errorPolicy: string(opts.ErrorPolicy)}
}

func (a *environmentAccumulator) add(value result) error {
	for _, warning := range value.warnings {
		if warning.Code == "tree_size_limit" {
			a.inventoryComplete = false
			a.inventoryOmission = "tree_size_limit"
		}
	}
	base := path.Base(value.path)
	toolchain := base == ".python-version" || base == ".node-version" || base == ".nvmrc" || base == "rust-toolchain" || base == "rust-toolchain.toml"
	envRelevant := base == "global.json" || base == "Directory.Build.props" || toolchain
	// A file_read_error under continue is attributable to a specific
	// enumerated path, so the inventory itself is not invalidated: an
	// env-relevant candidate is still recorded (Analyze will report it as
	// unresolved through a per-path diagnostic when its bounded read fails
	// again), and an unrelated per-file failure never affected this module.
	// Non-continue omissions and non-attributable omissions
	// (missing_git_object, tree_size_limit surfaced elsewhere) preserve the
	// pre-existing defensive posture that marks the inventory incomplete.
	if value.omission != "" && !(a.errorPolicy == "continue" && value.omission == "file_read_error") {
		a.inventoryComplete = false
		a.inventoryOmission = value.omission
	}
	if !envRelevant {
		return nil
	}
	if len(a.files) >= environments.DefaultMaxInventoryPaths {
		return errors.New("environment configuration inventory limit reached")
	}
	if _, ok := a.files[value.path]; ok {
		return errors.New("duplicate environment configuration path")
	}
	// An unreadable candidate is still recorded in the inventory so nearest()
	// resolves to it. Its ReadSelected callback will fail again during Analyze
	// and, under the continue policy, produce a per-path "file-read-error"
	// diagnostic rather than aborting or invalidating the whole inventory.
	file := environments.File{Path: value.path, NonRegular: value.selectedJob == nil}
	if value.selectedJob != nil && (base == "global.json" || toolchain) {
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
	if a.maxFileBytes > 0 && a.maxFileBytes < environments.DefaultMaxToolchainFileBytes {
		limits.ToolchainFileBytes = a.maxFileBytes
	}
	input := environments.Input{
		Source: report.Declarations.Source, Tree: report.Declarations.Tree,
		Inventory: inventory, InventoryComplete: true,
		Declarations: *report.Declarations, ProjectRecords: collector.ProjectRecords(),
		ErrorPolicy: a.errorPolicy,
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
