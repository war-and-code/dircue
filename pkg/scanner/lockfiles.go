package scanner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/lockfiles"
	"github.com/war-and-code/dircue/pkg/profile"
)

// lockfileAccumulator retains selected configuration readers, never paths
// to reopen through a different source selection.
type lockfileAccumulator struct {
	jobs              map[string]job
	files             map[string]lockfiles.File
	maxFileBytes      int64
	inventoryComplete bool
	inventoryOmission string
	errorPolicy       string
}

func newLockfileAccumulator(opts Options) *lockfileAccumulator {
	if !opts.Lockfiles {
		return nil
	}
	return &lockfileAccumulator{jobs: make(map[string]job), files: make(map[string]lockfiles.File), maxFileBytes: opts.MaxFileBytes, inventoryComplete: true, errorPolicy: string(opts.ErrorPolicy)}
}

func (a *lockfileAccumulator) add(value result) error {
	for _, warning := range value.warnings {
		if warning.Code == "tree_size_limit" {
			a.inventoryComplete = false
			a.inventoryOmission = "tree_size_limit"
		}
	}
	base := path.Base(value.path)
	lockCandidate := base == "package-lock.json" || base == "npm-shrinkwrap.json" || base == "packages.lock.json" || (strings.HasPrefix(base, "packages.") && strings.HasSuffix(base, ".lock.json"))
	manifest := strings.EqualFold(path.Ext(base), ".csproj")
	sharedInput := strings.EqualFold(base, "Directory.Build.props") || strings.EqualFold(base, "Directory.Build.targets") || strings.EqualFold(base, "Directory.Packages.props")
	relevant := lockCandidate || manifest || sharedInput
	// Continue-mode read failures retain the known path. Non-attributable
	// omissions invalidate absence checks for the selected inventory.
	if value.omission != "" && !(a.errorPolicy == "continue" && value.omission == "file_read_error") {
		a.inventoryComplete = false
		a.inventoryOmission = value.omission
	}
	if !relevant {
		return nil
	}
	if len(a.files) >= lockfiles.DefaultMaxInventoryPaths {
		return errors.New("lockfile inventory limit reached")
	}
	if _, ok := a.files[value.path]; ok {
		return errors.New("duplicate lockfile path")
	}
	// Keep unreadable and non-regular candidates visible. Only selected regular
	// lockfiles and project manifests retain a reader. Shared inputs are hints.
	file := lockfiles.File{Path: value.path, NonRegular: value.selectedJob == nil}
	if value.selectedJob != nil && (lockCandidate || manifest) {
		item := *value.selectedJob
		file.Size = item.size
		a.jobs[value.path] = item
	}
	a.files[value.path] = file
	return nil
}

func (a *lockfileAccumulator) finish(ctx context.Context, root *os.Root, collector *declarations.Collector, report *profile.Report) error {
	if !a.inventoryComplete {
		source, tree := "directory", ""
		if report.Declarations != nil {
			source, tree = report.Declarations.Source, report.Declarations.Tree
		}
		reason := a.inventoryOmission
		if reason == "" {
			reason = "inventory_incomplete"
		}
		report.Lockfiles = lockfiles.Skip(source, tree, reason)
		return nil
	}
	if collector == nil || report.Declarations == nil {
		return errors.New("lockfiles require declaration evidence")
	}
	paths := make([]string, 0, len(a.files))
	for name := range a.files {
		paths = append(paths, name)
	}
	slices.Sort(paths)
	inventory := make([]lockfiles.File, 0, len(paths))
	for _, name := range paths {
		inventory = append(inventory, a.files[name])
	}
	limits := lockfiles.Limits{}
	if a.maxFileBytes > 0 && a.maxFileBytes < lockfiles.DefaultMaxFileBytes {
		limits.FileBytes = a.maxFileBytes
	}
	input := lockfiles.Input{
		Source: report.Declarations.Source, Tree: report.Declarations.Tree,
		Inventory: inventory, InventoryComplete: true,
		Declarations: *report.Declarations, ProjectRecords: collector.ProjectRecords(),
		ErrorPolicy: a.errorPolicy,
		ReadSelected: func(ctx context.Context, name string, limit int64) ([]byte, int64, error) {
			selected, ok := a.jobs[name]
			if !ok {
				return nil, 0, errors.New("lockfile is not a selected regular file")
			}
			return selectedSourceAvailabilityRead(root, selected)(ctx, limit)
		},
	}
	var err error
	report.Lockfiles, err = lockfiles.Analyze(ctx, input, limits)
	if err != nil {
		return fmt.Errorf("analyze lockfiles: %w", err)
	}
	return nil
}
