// Package scanner profiles an arbitrary directory without requiring Git or
// executing files from the checkout. Detector hooks run concurrently.
package scanner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"dircue/pkg/availability"
	"dircue/pkg/declarations"
	"dircue/pkg/discovery"
	"dircue/pkg/environments"
	"dircue/pkg/explain"
	"dircue/pkg/focus"
	"dircue/pkg/formats"
	"dircue/pkg/profile"
	"dircue/pkg/projects"
	"dircue/pkg/registries"
	"dircue/pkg/rules"
	"dircue/pkg/structure"
	enry "github.com/go-enry/go-enry/v2"
)

const DefaultMaxFileBytes int64 = 0
const DefaultMaxTreeSize = 100_000
const ClassificationBytes int64 = 128 * 1024

type Options struct {
	// Environments reuses declarations and reads selected global.json inputs.
	Environments bool
	// Focus selects a declared project population and contextual inputs.
	Focus *focus.Request
	// Availability observes source-acquisition boundaries only when requested.
	Availability     bool
	AvailabilityOnly bool
	// ExplainPath selects one root-relative language decision for a fresh trace.
	ExplainPath string
	// Discovery adds metadata evidence independently of language inclusion.
	Discovery bool
	// Formats inspects bounded prefixes independently of language inclusion.
	Formats bool
	// FormatsOnly inspects formats without language classification or other profilers.
	FormatsOnly bool
	// DiscoveryOnly inventories metadata without language or content analysis.
	DiscoveryOnly bool
	// Rules evaluates explicit caller-supplied observations independently of language inclusion.
	Rules *rules.Program
	// RulesOnly skips classifiers and other content profilers.
	RulesOnly bool
	// Registries observes selected repository-owned registry configuration declarations.
	Registries bool
	// RegistriesOnly skips language classifiers and other content profilers.
	RegistriesOnly bool
	// Source selects committed Git content when auto discovers a repository.
	Source   string
	Revision string
	// Tree selects an exact Git tree object by its full SHA-1 object ID.
	Tree string
	// ErrorPolicy controls recoverable per-file read failures. Empty means fail.
	ErrorPolicy       ErrorPolicy
	MaxTreeSize       int
	Workers           int
	MaxFileBytes      int64
	IncludeFiles      bool
	IncludeStrategies bool
	Detectors         []profile.Detector
	Metrics           *MetricsOptions
	Projects          bool
	Declarations      bool
	// DeclarationsOnly reads supported manifests without language classification.
	DeclarationsOnly bool
	Structure        *structure.Client
	StructureFiles   bool
	// GitReadMetrics enables internal, opt-in Git work counters. Counters do
	// not enter deterministic reports and may vary with scheduling/cache state.
	GitReadMetrics *GitReadMetrics
	structureGate  chan struct{}
	languageTrace  *explain.LanguageTrace
}

type ErrorPolicy string

const (
	ErrorPolicyFail     ErrorPolicy = "fail"
	ErrorPolicyContinue ErrorPolicy = "continue"
)

type recoverableFileError struct{ err error }

func (e recoverableFileError) Error() string { return e.err.Error() }
func (e recoverableFileError) Unwrap() error { return e.err }

func recoverable(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return recoverableFileError{err: err}
}

func isRecoverableFileError(err error) bool {
	var target recoverableFileError
	return errors.As(err, &target) || errors.Is(err, registries.ErrRead)
}

type job struct {
	gitlink        *availability.Gitlink
	path           string
	size           int64
	attrs          overrides
	traceOverrides []explain.Override
	read           func(int64) ([]byte, int64, error)
}
type result struct {
	languageTrace       *explain.LanguageTrace
	gitlink             *availability.Gitlink
	selectedJob         *job
	formatFile          *formats.Candidate
	declarationFile     *declarations.Candidate
	declarationSelected bool
	registryFile        *registries.Candidate
	discoveryFile       *discovery.File
	rulesFile           *rules.File
	rulesRead           func(int64) ([]byte, int64, error)
	path                string
	size                int64
	language            string
	strategy            string
	skipped             bool
	warnings            []profile.Warning
	findings            []profile.Finding
	metrics             *profile.FileMetrics
	projectDocument     projects.Document
	inventorySize       int64
	inventoried         bool
	role                string
	omission            string
	structural          *structure.File
}

// Scan returns a deterministic report. A zero worker count uses GOMAXPROCS,
// capped at 16. By default only a 128 KiB prefix is classified, while full file
// sizes contribute to totals, matching Linguist LazyBlob. I/O failures are
// fatal; detector failures become warnings and preserve partial findings.
// An explicit MaxFileBytes limit skips larger file contents; metadata discovery
// still inventories their names and full sizes.
func Scan(ctx context.Context, directory string, opts Options) (out *profile.Report, returnErr error) {
	if err := validateTargetedOptions(opts); err != nil {
		return nil, err
	}
	if opts.Focus != nil || opts.Environments {
		opts.Declarations = true
	}
	if opts.FormatsOnly && (!opts.Formats || opts.Discovery || opts.Rules != nil || opts.Registries || opts.Projects || opts.Declarations || opts.Metrics != nil || opts.Structure != nil || len(opts.Detectors) > 0) {
		return nil, errors.New("formats-only requires formats and cannot run other profilers")
	}
	if opts.Formats && (opts.DiscoveryOnly || opts.DeclarationsOnly || opts.RegistriesOnly || opts.RulesOnly) {
		return nil, errors.New("formats cannot run in another module-only scan")
	}
	if opts.DeclarationsOnly && (!opts.Declarations || opts.Discovery || opts.Rules != nil || opts.Registries || opts.Projects || opts.Metrics != nil || opts.Structure != nil || len(opts.Detectors) > 0) {
		return nil, errors.New("declarations-only requires declarations and cannot run other profilers")
	}
	if opts.Declarations && (opts.DiscoveryOnly || opts.RegistriesOnly || opts.RulesOnly) {
		return nil, errors.New("declarations cannot run in a metadata-only or other module-only scan")
	}
	if opts.DiscoveryOnly && (opts.Registries || opts.RegistriesOnly) {
		return nil, errors.New("discovery-only cannot run registries")
	}
	if opts.RulesOnly && (opts.Registries || opts.RegistriesOnly) {
		return nil, errors.New("rules-only cannot run registries")
	}
	if opts.RegistriesOnly && (!opts.Registries || opts.Rules != nil || opts.RulesOnly || opts.DiscoveryOnly || opts.Projects || opts.Metrics != nil || opts.Structure != nil || len(opts.Detectors) > 0) {
		return nil, errors.New("registries-only requires registries and cannot run rules, discovery-only, detectors, projects, metrics, or structure")
	}
	if opts.DiscoveryOnly && (opts.Rules != nil || opts.RulesOnly) {
		return nil, errors.New("discovery-only cannot run rules")
	}
	if opts.DiscoveryOnly && (!opts.Discovery || opts.Projects || opts.Metrics != nil || opts.Structure != nil || len(opts.Detectors) > 0) {
		return nil, errors.New("discovery-only requires discovery and cannot run detectors, projects, metrics, or structure")
	}
	if opts.RulesOnly && (opts.Rules == nil || opts.DiscoveryOnly || opts.Projects || opts.Metrics != nil || opts.Structure != nil || len(opts.Detectors) > 0) {
		return nil, errors.New("rules-only requires rules and cannot run detectors, projects, metrics, structure, or discovery-only")
	}
	if opts.Structure != nil {
		if err := opts.Structure.CheckCapabilities(ctx); err != nil {
			return nil, err
		}
		opts.structureGate = make(chan struct{}, 1)
	}
	if opts.Metrics != nil {
		normalized, err := normalizeMetricsOptions(*opts.Metrics)
		if err != nil {
			return nil, err
		}
		opts.Metrics = &normalized
	}
	if opts.Workers < 0 || opts.MaxFileBytes < 0 || opts.MaxTreeSize < 0 {
		return nil, errors.New("workers, max file bytes, and max tree size must not be negative")
	}
	if opts.Workers > 1024 {
		return nil, errors.New("workers must not exceed 1024")
	}
	if opts.Workers == 0 {
		opts.Workers = min(runtime.GOMAXPROCS(0), 16)
	}
	if opts.MaxTreeSize == 0 {
		opts.MaxTreeSize = DefaultMaxTreeSize
	}
	if opts.Source == "" {
		opts.Source = "auto"
	}
	if opts.Source != "auto" && opts.Source != "git" && opts.Source != "directory" {
		return nil, fmt.Errorf("unknown source %q", opts.Source)
	}
	if opts.ErrorPolicy == "" {
		opts.ErrorPolicy = ErrorPolicyFail
	}
	if opts.ErrorPolicy != ErrorPolicyFail && opts.ErrorPolicy != ErrorPolicyContinue {
		return nil, fmt.Errorf("unknown error policy %q", opts.ErrorPolicy)
	}
	if opts.Revision != "" && opts.Tree != "" {
		return nil, errors.New("revision and tree are mutually exclusive")
	}
	if strings.Contains(opts.Revision, ":") {
		return nil, errors.New("revision must select a commit, not a rev:path expression")
	}
	if opts.Source == "directory" && (opts.Revision != "" || opts.Tree != "") {
		return nil, errors.New("revision and tree require Git source")
	}
	if opts.MaxFileBytes == int64(^uint64(0)>>1) {
		return nil, errors.New("max file bytes must be less than MaxInt64")
	}
	for _, detector := range opts.Detectors {
		if detector == nil {
			return nil, errors.New("detectors must not be nil")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("resolve scan root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("inspect scan root: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("scan root must be a directory")
	}
	snapshot, err := openGitSnapshot(ctx, abs, opts, false)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := snapshot.close(); returnErr == nil && closeErr != nil {
			out = nil
			returnErr = fmt.Errorf("close Git storage: %w", closeErr)
		}
	}()
	if snapshot != nil {
		abs = snapshot.root
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("open scan root: %w", err)
	}
	defer root.Close()
	environmentCollector := newEnvironmentAccumulator(opts)
	availabilityCollector := newAvailabilityAccumulator(opts, snapshot)
	focusCollector, err := newFocusAccumulator(opts, snapshot)
	if err != nil {
		return nil, err
	}
	ruleCollector, err := newRulesAccumulator(opts, snapshot)
	if err != nil {
		return nil, err
	}
	registryCollector, err := newRegistryAccumulator(opts, snapshot)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		exceeded, err := directoryTreeLimit(ctx, root, opts.MaxTreeSize)
		if err != nil {
			return nil, err
		}
		if exceeded {
			if opts.Focus != nil {
				return nil, errors.New("focus inventory omitted: tree size limit reached")
			}
			report := newReport(abs)
			if opts.Environments {
				report.Environments = environments.Skip("directory", "", "tree_size_limit")
				report.SchemaVersion = profile.EnvironmentSchemaVersion
			}
			if registryCollector != nil {
				if err := registryCollector.collector.Omit("tree_size_limit", 1); err != nil {
					return nil, err
				}
				report.Registries, err = registryCollector.finish(ctx)
				if err != nil {
					return nil, err
				}
			}
			if ruleCollector != nil {
				if err := ruleCollector.collector.Omit(rules.TreeSizeLimit, 1); err != nil {
					return nil, err
				}
				report.Rules, err = ruleCollector.finish(ctx, root)
				if err != nil {
					return nil, err
				}
			}
			if opts.Formats {
				report.Formats = formats.New("directory", "", opts.MaxFileBytes).Skip("tree_size_limit")
				report.SchemaVersion = profile.ContentSchemaVersion
			}
			if opts.Discovery {
				report.Discovery = discovery.New("directory", "", opts.MaxTreeSize).Skip("tree_size_limit")
			}
			if opts.Projects {
				report.Projects = projects.New("directory", "").Skip("tree_size_limit")
				report.SchemaVersion = profile.ExpandedSchemaVersion
			}
			if opts.Structure != nil {
				report.Structure = newStructureReport(opts, snapshot)
				report.Structure.Status = "skipped"
				report.Structure.Omissions["tree_size_limit"] = 1
				finishStructure(report.Structure)
				report.SchemaVersion = profile.ExpandedSchemaVersion
			}
			if opts.Metrics != nil {
				report.SchemaVersion = profile.MetricsSchemaVersion
				report.Metrics = newMetricsReport(*opts.Metrics, snapshot)
				report.Metrics.Status = "skipped"
				report.Metrics.Skipped = append(report.Metrics.Skipped, profile.MetricSkip{Reason: "tree_size_limit"})
			}
			if opts.Projects || opts.Structure != nil {
				report.SchemaVersion = profile.ExpandedSchemaVersion
			}
			if opts.Registries || opts.Rules != nil || opts.Discovery || opts.Structure != nil && opts.Structure.FunctionMetricsEnabled() {
				report.SchemaVersion = profile.EnhancedSchemaVersion
			}
			report.Warnings = append(report.Warnings, profile.Warning{Path: ".", Code: "tree_size_limit", Message: fmt.Sprintf("directory has at least %d entries; analysis omitted", opts.MaxTreeSize)})
			if opts.Declarations {
				report.Declarations = declarations.New("directory", "", opts.MaxFileBytes).Skip("tree_size_limit")
				report.SchemaVersion = profile.DeclarationsSchemaVersion
			}
			if opts.Formats || (opts.Structure != nil && opts.Structure.HotspotsEnabled()) {
				report.SchemaVersion = profile.ContentSchemaVersion
			}
			if availabilityCollector != nil {
				if err := availabilityCollector.finish(ctx, root, report); err != nil {
					return nil, err
				}
				report.SchemaVersion = profile.TargetedSchemaVersion
			}
			if opts.ExplainPath != "" {
				if err := finishLanguageExplanation(opts, snapshot, report, nil, false); err != nil {
					return nil, err
				}
			}
			return report, nil
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan job, opts.Workers*2)
	results := make(chan result, opts.Workers*2)
	failures := make(chan error, 1)
	fail := func(err error) {
		select {
		case failures <- err:
		default:
		}
		cancel()
	}
	send := func(value result) bool {
		select {
		case results <- value:
			return true
		case <-ctx.Done():
			return false
		}
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(jobs)
		if snapshot != nil {
			if err := snapshot.walk(ctx, jobs, send); err != nil {
				fail(err)
			}
			return
		}
		// Each stack element stores only one directory's local rules. Retaining a
		// flattened ancestor copy per depth would grow quadratically on deep trees.
		var stack [][]attributeRule
		attributeRuleCount := 0
		err := fs.WalkDir(root.FS(), ".", func(filename string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == ".git" {
					return fs.SkipDir
				}
				depth := 0
				if filename != "." {
					depth = strings.Count(filename, "/") + 1
				}
				stack = stack[:depth]
				var local []attributeRule
				attrsPath := path.Join(filename, ".gitattributes")
				info, attrErr := root.Lstat(attrsPath)
				if attrErr != nil && !errors.Is(attrErr, fs.ErrNotExist) {
					return fmt.Errorf("inspect %s: %w", attrsPath, attrErr)
				}
				if attrErr == nil {
					if !info.Mode().IsRegular() {
						if !send(result{warnings: []profile.Warning{{Path: attrsPath, Code: "unsupported_gitattributes", Message: "attribute file is not a regular file; rules ignored"}}}) {
							return ctx.Err()
						}
					} else if info.Size() > maxAttributesBytes {
						if !send(result{warnings: []profile.Warning{{Path: attrsPath, Code: "unsupported_gitattributes", Message: "attribute file exceeds 1 MiB; rules ignored"}}}) {
							return ctx.Err()
						}
					} else {
						content, tooLarge, err := readBounded(root, attrsPath, maxAttributesBytes)
						if err != nil {
							return err
						}
						if tooLarge {
							if !send(result{warnings: []profile.Warning{{Path: attrsPath, Code: "unsupported_gitattributes", Message: "attribute file exceeds 1 MiB; rules ignored"}}}) {
								return ctx.Err()
							}
						} else {
							rules, warnings, exceeded := parseAttributesBounded(attrsPath, content, maxAttributeRules-attributeRuleCount)
							if exceeded {
								if !send(result{warnings: []profile.Warning{{Path: attrsPath, Code: "unsupported_gitattributes", Message: fmt.Sprintf("attribute rules exceed %d rule limit; rules ignored", maxAttributeRules)}}}) {
									return ctx.Err()
								}
								rules = nil
								warnings = nil
							}
							attributeRuleCount += len(rules)
							local = rules
							if len(warnings) > 0 && !send(result{warnings: warnings}) {
								return ctx.Err()
							}
						}
					}
				}
				stack = append(stack, local)
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				if !send(result{path: filename, skipped: true, warnings: []profile.Warning{{Path: filename, Code: "non_regular_file", Message: "symlink or special file skipped"}}}) {
					return ctx.Err()
				}
				return nil
			}
			depth := strings.Count(filename, "/")
			var attrs overrides
			var traceOverrides []explain.Override
			if filename == opts.ExplainPath {
				attrs, traceOverrides, err = resolveAttributeRuleSetsTraceContext(ctx, filename, stack[:depth+1])
			} else {
				attrs, err = resolveAttributeRuleSetsContext(ctx, filename, stack[:depth+1])
			}
			if err != nil {
				return err
			}
			select {
			case jobs <- job{path: filename, size: info.Size(), attrs: attrs, traceOverrides: traceOverrides}:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		if err != nil {
			fail(fmt.Errorf("walk directory: %w", err))
		}
	}()
	for range opts.Workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				if ctx.Err() != nil {
					return
				}
				value, err := analyzeSelectedFile(ctx, root, item, opts)
				if err != nil {
					if opts.ErrorPolicy == ErrorPolicyContinue && ctx.Err() == nil && isRecoverableFileError(err) {
						value.path = item.path
						value.skipped = true
						value.omission = "file_read_error"
						value.warnings = append(value.warnings, profile.Warning{Path: item.path, Code: "file_read_error", Message: err.Error()})
						if !send(value) {
							return
						}
						continue
					}
					fail(err)
					return
				}
				if !send(value) {
					return
				}
			}
		}()
	}
	go func() { wg.Wait(); close(results) }()
	report := newReport(abs)
	var formatCollector *formats.Collector
	if opts.Formats {
		source, tree := "directory", ""
		if snapshot != nil {
			source, tree = "git", snapshot.tree.Hash.String()
		}
		formatCollector = formats.New(source, tree, opts.MaxFileBytes)
		formatCollector.SetErrorPolicy(string(opts.ErrorPolicy))
	}
	var declarationCollector *declarations.Collector
	if opts.Declarations {
		source, tree := "directory", ""
		if snapshot != nil {
			source, tree = "git", snapshot.tree.Hash.String()
		}
		declarationCollector = declarations.New(source, tree, opts.MaxFileBytes)
		declarationCollector.SetErrorPolicy(string(opts.ErrorPolicy))
		if opts.Focus != nil || opts.Environments {
			declarationCollector.EnableProjectRecords()
		}
	}
	var discoveryCollector *discovery.Collector
	if opts.Discovery {
		source, tree := "directory", ""
		if snapshot != nil {
			source, tree = "git", snapshot.tree.Hash.String()
		}
		discoveryCollector = discovery.New(source, tree, opts.MaxTreeSize)
	}
	var metrics *metricsAccumulator
	if opts.Metrics != nil && opts.Focus == nil {
		report.SchemaVersion = profile.MetricsSchemaVersion
		report.Metrics = newMetricsReport(*opts.Metrics, snapshot)
		metrics = newMetricsAccumulator(report.Metrics)
	}
	var projectCollector *projects.Collector
	if opts.Projects {
		source, tree := "directory", ""
		if snapshot != nil {
			source = "git"
			tree = snapshot.tree.Hash.String()
		}
		projectCollector = projects.New(source, tree)
		report.SchemaVersion = profile.ExpandedSchemaVersion
	}
	if opts.Structure != nil {
		report.Structure = newStructureReport(opts, snapshot)
		report.SchemaVersion = profile.ExpandedSchemaVersion
	}
	languages := make(map[string]*profile.Language)
	type findingKey struct{ kind, name, root, detector string }
	findings := make(map[findingKey]*profile.Finding)
	if opts.IncludeStrategies {
		report.Strategies = make(map[string]string)
	}
	var targetTrace *explain.LanguageTrace
	targetNonRegular := false
	for value := range results {
		if environmentCollector != nil {
			if err := environmentCollector.add(value); err != nil {
				fail(err)
				continue
			}
		}
		if opts.ExplainPath != "" && value.path == opts.ExplainPath {
			targetTrace = value.languageTrace
			targetNonRegular = value.languageTrace == nil
		}
		if availabilityCollector != nil {
			if err := availabilityCollector.add(value, root); err != nil {
				fail(err)
				continue
			}
		}
		if focusCollector != nil {
			if err := focusCollector.add(value); err != nil {
				fail(err)
				continue
			}
		}
		if formatCollector != nil {
			if value.formatFile != nil {
				formatCollector.Add(*value.formatFile)
			} else if value.path != "" {
				formatCollector.Omit(omissionReason(value, "non_regular_file"))
			}
		}
		if declarationCollector != nil {
			if value.declarationSelected {
				declarationCollector.Add(value.path, value.declarationFile)
			} else if value.path != "" {
				declarationCollector.Omit()
			}
		}
		if registryCollector != nil {
			if err := registryCollector.add(value); err != nil {
				fail(err)
				continue
			}
		}
		if ruleCollector != nil {
			if err := ruleCollector.add(value); err != nil {
				fail(fmt.Errorf("collect rules: %w", err))
				continue
			}
		}
		if discoveryCollector != nil {
			if value.discoveryFile != nil {
				discoveryCollector.Add(*value.discoveryFile)
			} else if value.path != "" {
				reason := omissionReason(value, "non_regular_file")
				if value.omission != "" {
					discoveryCollector.Partial(reason)
				} else {
					discoveryCollector.Omit(reason)
				}
			}
		}
		if projectCollector != nil {
			if value.inventoried {
				projectCollector.Add(value.path, value.inventorySize, value.role, value.projectDocument)
			} else if value.path != "" {
				projectCollector.Omit()
			}
		}
		if report.Structure != nil {
			if err := addStructure(report.Structure, value); err != nil {
				fail(err)
				continue
			}
		}
		if metrics != nil {
			metrics.add(value)
		}
		if value.strategy != "" && opts.IncludeStrategies {
			report.Strategies[value.path] = value.strategy
		}
		report.Warnings = append(report.Warnings, value.warnings...)
		if value.path == "" {
			continue
		}
		report.Summary.ScannedFiles++
		if value.skipped {
			report.Summary.SkippedFiles++
			continue
		}
		report.Summary.AnalyzedFiles++
		if value.language != "" {
			language := languages[value.language]
			if language == nil {
				language = &profile.Language{Name: value.language}
				languages[value.language] = language
			}
			if language.FirstFile == "" || value.path < language.FirstFile {
				language.FirstFile = value.path
			}
			language.Bytes += value.size
			language.FileCount++
			if opts.IncludeFiles {
				language.Files = append(language.Files, value.path)
			}
			report.Summary.LanguageBytes += value.size
		}
		for _, finding := range value.findings {
			if finding.Root == "" {
				finding.Root = "."
			}
			if err := validateFinding(finding); err != nil {
				report.Warnings = append(report.Warnings, profile.Warning{Path: value.path, Code: "invalid_finding", Message: finding.Detector + ": " + err.Error()})
				continue
			}
			key := findingKey{finding.Kind, finding.Name, finding.Root, finding.Detector}
			if prior := findings[key]; prior != nil {
				prior.Evidence = append(prior.Evidence, finding.Evidence...)
			} else {
				findings[key] = &finding
			}
		}
	}
	select {
	case err := <-failures:
		return nil, err
	default:
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Aggregation modules can report the same unreadable path as the language
	// pass and as each other. Index existing warnings once so mass failures do
	// not turn per-module warning deduplication into quadratic work.
	fileReadErrorWarnings := make(map[string]struct{})
	for _, warning := range report.Warnings {
		if warning.Code == "file_read_error" {
			fileReadErrorWarnings[warning.Path] = struct{}{}
		}
	}
	if ruleCollector != nil {
		for _, warning := range report.Warnings {
			if warning.Code == "tree_size_limit" {
				if err := ruleCollector.collector.Omit(rules.TreeSizeLimit, 1); err != nil {
					return nil, err
				}
			}
		}
		report.Rules, err = ruleCollector.finish(ctx, root)
		if err != nil {
			return nil, err
		}
	}
	if registryCollector != nil {
		for _, warning := range report.Warnings {
			if warning.Code == "tree_size_limit" {
				if err := registryCollector.collector.Omit("tree_size_limit", 1); err != nil {
					return nil, err
				}
			}
		}
		report.Registries, err = registryCollector.finish(ctx)
		if err != nil {
			return nil, err
		}
		for _, path := range registryCollector.collector.ReadErrors() {
			addFileReadErrorWarning(report, fileReadErrorWarnings, path, "selected registry configuration could not be read")
		}
		report.SchemaVersion = profile.EnhancedSchemaVersion
	}
	if metrics != nil {
		metrics.finish()
	}
	if projectCollector != nil {
		report.Projects = projectCollector.Finish()
		for _, w := range report.Warnings {
			if w.Code == "tree_size_limit" {
				report.Projects = projectCollector.Skip("tree_size_limit")
			}
		}
	}
	if report.Structure != nil {
		finishStructure(report.Structure)
		if opts.Structure.FunctionMetricsEnabled() {
			report.SchemaVersion = profile.EnhancedSchemaVersion
		}
	}
	if discoveryCollector != nil {
		report.Discovery = discoveryCollector.Finish()
		for _, warning := range report.Warnings {
			switch warning.Code {
			case "tree_size_limit":
				report.Discovery = discoveryCollector.Skip("tree_size_limit")
			case "unsupported_gitattributes":
				discoveryCollector.Partial("unsupported_gitattributes")
			}
		}
		report.SchemaVersion = profile.EnhancedSchemaVersion
	}
	if ruleCollector != nil {
		report.SchemaVersion = profile.EnhancedSchemaVersion
	}
	if declarationCollector != nil {
		report.Declarations, err = declarationCollector.Finish(ctx)
		if err != nil {
			return nil, err
		}
		for _, warning := range report.Warnings {
			if warning.Code == "tree_size_limit" {
				report.Declarations = declarationCollector.Skip("tree_size_limit")
			}
		}
		for _, path := range declarationCollector.ReadErrors() {
			addFileReadErrorWarning(report, fileReadErrorWarnings, path, "selected declaration manifest could not be read")
		}
		report.SchemaVersion = profile.DeclarationsSchemaVersion
	}
	if formatCollector != nil {
		report.Formats, err = formatCollector.Finish(ctx)
		if err != nil {
			return nil, err
		}
		for _, warning := range report.Warnings {
			if warning.Code == "tree_size_limit" {
				report.Formats = formatCollector.Skip("tree_size_limit")
			}
		}
		for _, path := range formatCollector.ReadErrors() {
			addFileReadErrorWarning(report, fileReadErrorWarnings, path, "selected format candidate could not be read")
		}
		report.SchemaVersion = profile.ContentSchemaVersion
	}
	if report.Structure != nil && report.Structure.Hotspots != nil {
		report.SchemaVersion = profile.ContentSchemaVersion
	}
	if availabilityCollector != nil {
		if err := availabilityCollector.finish(ctx, root, report); err != nil {
			return nil, err
		}
		report.SchemaVersion = profile.TargetedSchemaVersion
	}
	if opts.ExplainPath != "" {
		if err := finishLanguageExplanation(opts, snapshot, report, targetTrace, targetNonRegular); err != nil {
			return nil, err
		}
	}
	if focusCollector != nil {
		if err := focusCollector.finish(ctx, root, declarationCollector, report); err != nil {
			return nil, err
		}
		report.SchemaVersion = profile.TargetedSchemaVersion
	}
	if environmentCollector != nil {
		if err := environmentCollector.finish(ctx, root, declarationCollector, report); err != nil {
			return nil, err
		}
		if report.Environments != nil {
			for _, d := range report.Environments.Diagnostics {
				if d.Code == "file-read-error" {
					addFileReadErrorWarning(report, fileReadErrorWarnings, d.Path, "selected global.json could not be read")
				}
			}
		}
		report.SchemaVersion = profile.EnvironmentSchemaVersion
	}
	for _, language := range languages {
		if report.Summary.LanguageBytes > 0 {
			language.Percentage = 100 * float64(language.Bytes) / float64(report.Summary.LanguageBytes)
		}
		slices.Sort(language.Files)
		report.Languages = append(report.Languages, *language)
	}
	slices.SortFunc(report.Languages, func(a, b profile.Language) int {
		if a.Bytes > b.Bytes {
			return -1
		}
		if a.Bytes < b.Bytes {
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	for _, finding := range findings {
		if finding.Evidence == nil {
			finding.Evidence = []string{}
		}
		slices.Sort(finding.Evidence)
		finding.Evidence = slices.Compact(finding.Evidence)
		switch finding.Kind {
		case "ecosystem":
			report.Ecosystems = append(report.Ecosystems, *finding)
		case "framework":
			report.Frameworks = append(report.Frameworks, *finding)
		case "layout":
			report.Layouts = append(report.Layouts, *finding)
		default:
			report.Warnings = append(report.Warnings, profile.Warning{Path: finding.Root, Code: "unknown_finding_kind", Message: "detector " + finding.Detector + " returned unsupported kind " + finding.Kind})
		}
	}
	for _, group := range [][]profile.Finding{report.Ecosystems, report.Frameworks, report.Layouts} {
		slices.SortFunc(group, func(a, b profile.Finding) int {
			if n := strings.Compare(a.Root, b.Root); n != 0 {
				return n
			}
			if n := strings.Compare(a.Name, b.Name); n != 0 {
				return n
			}
			return strings.Compare(a.Detector, b.Detector)
		})
	}
	slices.SortFunc(report.Warnings, func(a, b profile.Warning) int {
		if n := strings.Compare(a.Path, b.Path); n != 0 {
			return n
		}
		if n := strings.Compare(a.Code, b.Code); n != 0 {
			return n
		}
		return strings.Compare(a.Message, b.Message)
	})
	return report, nil
}

func omissionReason(value result, fallback string) string {
	if value.omission != "" {
		return value.omission
	}
	return fallback
}

// addFileReadErrorWarning appends a per-path file_read_error warning without
// duplicating one the language pass already emitted for the same file: the
// aggregation modules commonly reach the same unreadable file after the
// scanner-level per-file recovery has already surfaced it, and the caller
// gains nothing from parallel warnings that name the same path.
func addFileReadErrorWarning(report *profile.Report, seen map[string]struct{}, path, message string) {
	if _, exists := seen[path]; exists {
		return
	}
	seen[path] = struct{}{}
	report.Warnings = append(report.Warnings, profile.Warning{Path: path, Code: "file_read_error", Message: message})
}

func analyzeFileBase(ctx context.Context, root *os.Root, item job, opts Options) (result, error) {
	trace := opts.languageTrace
	if trace != nil {
		addLanguageTraceStep(trace, "selected-file", "scanner", "regular-file", fmt.Sprintf("%d bytes in selected source", item.size))
	}
	value := result{path: item.path}
	if opts.Structure != nil {
		value.size = item.size
	}
	if opts.Projects {
		value.inventoried = true
		value.inventorySize = item.size
		value.role = "unknown"
	}
	if opts.Metrics != nil {
		value.metrics = &profile.FileMetrics{Path: item.path, Status: "skipped", Reason: "outside_scope"}
	}
	enryVendored := enry.IsVendor(item.path)
	vendored := overrideBool(item.attrs.vendored, enryVendored)
	if trace != nil {
		vendorProvider := "go-enry path rules"
		if item.attrs.vendored != nil {
			vendorProvider = languageTraceOverrideProvider(trace, "linguist-vendored")
		}
		vendorOutcome := strconv.FormatBool(vendored)
		if vendored && item.attrs.vendored == nil && isCIHookPath(item.path) {
			vendorOutcome = "continue-for-ci-hook-evidence"
		}
		addLanguageTraceStep(trace, "vendored", vendorProvider, vendorOutcome, fmt.Sprintf("fallback=%t", enryVendored))
	}
	if opts.Projects && vendored {
		value.role = "vendored"
	}
	if vendored && !(item.attrs.vendored == nil && isCIHookPath(item.path)) {
		value.skipped = true
		if opts.Metrics == nil || opts.Metrics.Scope != "text" {
			if trace != nil {
				setLanguageTraceDecision(trace, "excluded", "vendored", "", "", "")
			}
			return value, nil
		}
	}
	if opts.MaxFileBytes > 0 && item.size > opts.MaxFileBytes {
		value.skipped = true
		if value.metrics != nil {
			value.metrics.Reason = "file_too_large"
		}
		value.warnings = []profile.Warning{{Path: item.path, Code: "file_too_large", Message: fmt.Sprintf("file exceeds %d byte limit", opts.MaxFileBytes)}}
		if trace != nil {
			addLanguageTraceStep(trace, "file-read-limit", "scanner", "excluded", fmt.Sprintf("%d bytes exceeds %d-byte limit", item.size, opts.MaxFileBytes))
			setLanguageTraceDecision(trace, "excluded", "file_too_large", "", "", "")
		}
		return value, nil
	}
	limit := ClassificationBytes
	if len(opts.Detectors) > 0 {
		limit = maxAttributesBytes
	}
	if opts.Projects && projects.IsManifest(item.path) {
		limit = max(limit, projects.MaxManifestBytes)
	}
	if opts.MaxFileBytes > 0 {
		limit = min(limit, opts.MaxFileBytes)
	}
	var content []byte
	var tooLarge bool
	var err error
	actualSize := item.size
	if item.read != nil {
		content, actualSize, err = item.read(limit)
		tooLarge = opts.MaxFileBytes > 0 && actualSize > opts.MaxFileBytes
	} else {
		content, tooLarge, actualSize, err = readBoundedSize(root, item.path, limit)
		tooLarge = opts.MaxFileBytes > 0 && (actualSize > opts.MaxFileBytes || (limit == opts.MaxFileBytes && tooLarge))
		if int64(len(content)) > limit {
			content = content[:limit]
		}
	}
	if err != nil {
		if trace != nil {
			addLanguageTraceStep(trace, "content-read", "selected source", "unavailable", "read failed")
			setLanguageTraceDecision(trace, "unavailable", "read_error", "", "", "")
		}
		return value, recoverable(err)
	}
	if trace != nil {
		trace.Extent.FileBytes = actualSize
		trace.Extent.ReadBytes = int64(len(content))
		trace.Extent.ContentComplete = int64(len(content)) == actualSize
	}
	if trace != nil {
		addLanguageTraceStep(trace, "content-read", "selected source", "bounded", fmt.Sprintf("read %d of %d bytes", len(content), actualSize))
	}
	if opts.Projects {
		value.inventorySize = actualSize
	}
	if tooLarge {
		value.skipped = true
		if value.metrics != nil {
			value.metrics.Reason = "file_too_large"
		}
		value.warnings = []profile.Warning{{Path: item.path, Code: "file_too_large", Message: fmt.Sprintf("file exceeds %d byte limit", opts.MaxFileBytes)}}
		if trace != nil {
			addLanguageTraceStep(trace, "file-read-limit", "scanner", "excluded", fmt.Sprintf("selected source reports %d bytes above %d-byte limit", actualSize, opts.MaxFileBytes))
			setLanguageTraceDecision(trace, "excluded", "file_too_large", "", "", "")
		}
		return value, nil
	}
	if err := ctx.Err(); err != nil {
		if trace != nil {
			addLanguageTraceStep(trace, "cancellation", "context", "unavailable", err.Error())
			setLanguageTraceDecision(trace, "unavailable", "cancelled", "", "", "")
		}
		return result{}, err
	}
	classContent := content[:min(int64(len(content)), ClassificationBytes)]
	if trace != nil {
		trace.Extent.ClassifiedBytes = int64(len(classContent))
	}
	// LazyBlob's explicit language override bypasses Linguist.detect's binary
	// and empty-file checks. Preserve that behavior for attributed content.
	binary := isBinaryContent(classContent)
	if opts.Projects && binary {
		value.role = "binary"
	}
	if binary && (!item.attrs.languageSet || item.attrs.language == "") {
		if value.metrics != nil {
			value.metrics.Reason = "binary"
		}
		value.skipped = true
		if trace != nil {
			addLanguageTraceStep(trace, "binary-preflight", "dircue Linguist-compatible binary preflight", "excluded", fmt.Sprintf("NUL/signature check over %d bytes", len(classContent)))
			setLanguageTraceDecision(trace, "excluded", "binary", "", "", "")
		}
		return value, nil
	}
	if trace != nil {
		if binary {
			addLanguageTraceStep(trace, "binary-preflight", "dircue Linguist-compatible binary preflight", "bypassed-by-language-override", fmt.Sprintf("NUL/signature check over %d bytes", len(classContent)))
		} else {
			addLanguageTraceStep(trace, "binary-preflight", "dircue Linguist-compatible binary preflight", "text", fmt.Sprintf("checked %d bytes", len(classContent)))
		}
	}
	detectedLanguage, strategy := DetectLanguage(path.Base(item.path), classContent)
	language := detectedLanguage
	language, value.strategy = applyLanguageOverride(language, strategy, item.attrs)
	if trace != nil {
		detectionOutcome := detectedLanguage
		if detectedLanguage == "" {
			detectionOutcome = "unknown"
		}
		addLanguageTraceStep(trace, "language-detection", "go-enry default strategies", detectionOutcome, strategy)
		if item.attrs.languageSet {
			overrideOutcome := language
			if overrideOutcome == "" {
				overrideOutcome = "unknown"
			}
			addLanguageTraceStep(trace, "language-override", languageTraceOverrideProvider(trace, "linguist-language"), overrideOutcome, value.strategy)
		}
	}
	if language == enry.OtherLanguage {
		language = ""
	}
	typ := enry.GetLanguageType(language)
	enryGenerated := enry.IsGenerated(item.path, classContent)
	generated := overrideBool(item.attrs.generated, enryGenerated)
	if trace != nil {
		generatedProvider := "go-enry generated rules"
		if item.attrs.generated != nil {
			generatedProvider = languageTraceOverrideProvider(trace, "linguist-generated")
		}
		addLanguageTraceStep(trace, "generated", generatedProvider, strconv.FormatBool(generated), fmt.Sprintf("fallback=%t", enryGenerated))
	}
	enryDocumentation := enry.IsDocumentation(item.path)
	documentation := overrideBool(item.attrs.documentation, enryDocumentation)
	if trace != nil {
		documentationProvider := "go-enry documentation rules"
		if item.attrs.documentation != nil {
			documentationProvider = languageTraceOverrideProvider(trace, "linguist-documentation")
		}
		addLanguageTraceStep(trace, "documentation", documentationProvider, strconv.FormatBool(documentation), fmt.Sprintf("fallback=%t", enryDocumentation))
	}
	lfsPointer := item.attrs.lfsTracked && isLFSPointer(classContent)
	if trace != nil && item.attrs.lfsTracked {
		addLanguageTraceStep(trace, "git-lfs-pointer", languageTraceOverrideProvider(trace, "filter")+" and pointer syntax", strconv.FormatBool(lfsPointer), "filter=lfs")
	}
	typeDetectable := typ == enry.Programming || typ == enry.Markup
	attributeDetectable := overrideBool(item.attrs.detectable, typeDetectable)
	detectable := !vendored && !generated && !documentation && !lfsPointer && attributeDetectable
	if trace != nil {
		detectableProvider := "Linguist language type"
		if item.attrs.detectable != nil {
			detectableProvider = languageTraceOverrideProvider(trace, "linguist-detectable")
		}
		evidence := fmt.Sprintf("type_detectable=%t", typeDetectable)
		if vendored || generated || documentation || lfsPointer {
			evidence += fmt.Sprintf(" vendored=%t generated=%t documentation=%t lfs_pointer=%t", vendored, generated, documentation, lfsPointer)
		}
		addLanguageTraceStep(trace, "detectable", detectableProvider, strconv.FormatBool(detectable), evidence)
	}
	value.size = actualSize
	if opts.Projects {
		value.role = contentRole(item.path, language, vendored, generated, documentation)
	}
	if detectable {
		value.language = enry.GetLanguageGroup(language)
		if value.language == "" {
			value.language = language
		}
	}
	if trace != nil {
		reportedLanguage := value.language
		reason := "included"
		status := "included"
		switch {
		case vendored:
			status, reason = "excluded", "vendored"
		case generated:
			status, reason = "excluded", "generated"
		case documentation:
			status, reason = "excluded", "documentation"
		case lfsPointer:
			status, reason = "excluded", "lfs_pointer"
		case !attributeDetectable:
			status, reason = "excluded", "not_detectable"
		case language == "":
			status, reason = "excluded", "language_unknown"
		}
		setLanguageTraceDecision(trace, status, reason, detectedLanguage, reportedLanguage, value.strategy)
	}
	if opts.Metrics != nil {
		if err := countFileMetrics(ctx, root, item, opts, &value, language, content, actualSize, detectable && language != ""); err != nil {
			return result{}, err
		}
	}
	if opts.Structure != nil {
		if err := countStructure(ctx, root, item, opts, &value, language, content, actualSize, detectable && language != ""); err != nil {
			return result{}, err
		}
	}
	if value.skipped {
		return value, nil
	}
	file := profile.File{Path: item.path, Size: value.size, Content: content, Language: language, Included: detectable && language != ""}
	for _, detector := range opts.Detectors {
		if err := ctx.Err(); err != nil {
			return result{}, err
		}
		findings, err := detector.Detect(ctx, file)
		for _, finding := range findings {
			finding.Detector = detector.Name()
			finding.Evidence = slices.Clone(finding.Evidence)
			value.findings = append(value.findings, finding)
		}
		if err != nil {
			value.warnings = append(value.warnings, profile.Warning{Path: item.path, Code: "detector_error", Message: detector.Name() + ": " + err.Error()})
		}
	}
	return value, nil
}

// readBounded scopes access to the root, rejects non-regular files, and reads at
// most limit+1 bytes so a growing file cannot force an unbounded allocation.
func readBounded(root *os.Root, filename string, limit int64) ([]byte, bool, error) {
	content, tooLarge, _, err := readBoundedSize(root, filename, limit)
	return content, tooLarge, err
}

func readBoundedSize(root *os.Root, filename string, limit int64) ([]byte, bool, int64, error) {
	info, err := root.Lstat(filename)
	if err != nil {
		return nil, false, 0, recoverable(fmt.Errorf("inspect %s: %w", filename, err))
	}
	if !info.Mode().IsRegular() {
		return nil, false, 0, recoverable(fmt.Errorf("read %s: not a regular file", filename))
	}
	file, err := openRegular(root, filename)
	if err != nil {
		return nil, false, 0, recoverable(fmt.Errorf("open %s: %w", filename, err))
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return nil, false, 0, recoverable(fmt.Errorf("stat %s: %w", filename, err))
	}
	if !info.Mode().IsRegular() {
		return nil, false, 0, recoverable(fmt.Errorf("read %s: file changed to a non-regular file", filename))
	}
	content, err := readAllBounded(file, limit+1, info.Size())
	if err != nil {
		return nil, false, 0, recoverable(fmt.Errorf("read %s: %w", filename, err))
	}
	return content, int64(len(content)) > limit, info.Size(), nil
}

func validateFinding(finding profile.Finding) error {
	if finding.Kind != "ecosystem" && finding.Kind != "framework" && finding.Kind != "layout" {
		return fmt.Errorf("unsupported finding kind %q", finding.Kind)
	}
	if strings.TrimSpace(finding.Name) == "" || strings.TrimSpace(finding.Detector) == "" {
		return errors.New("finding name and detector must not be empty")
	}
	validPath := func(value string) bool {
		return fs.ValidPath(value)
	}
	if !validPath(finding.Root) {
		return errors.New("finding root must be a normalized relative path")
	}
	if len(finding.Evidence) == 0 {
		return errors.New("finding must contain file evidence")
	}
	for _, evidence := range finding.Evidence {
		if evidence == "." || !validPath(evidence) {
			return errors.New("finding evidence must contain normalized relative file paths")
		}
	}
	return nil
}

// Enry marks CI configuration as vendored for language statistics. It is still
// useful hook evidence, unless its enclosing project is itself vendored.
func isCIHookPath(filename string) bool {
	directory := path.Dir(filename)
	if path.Base(filename) == "Jenkinsfile" {
		return !enry.IsVendor(path.Join(directory, "probe.go"))
	}
	if path.Base(directory) == "workflows" && path.Base(path.Dir(directory)) == ".github" && (path.Ext(filename) == ".yml" || path.Ext(filename) == ".yaml") {
		return !enry.IsVendor(path.Join(path.Dir(path.Dir(directory)), "probe.go"))
	}
	return false
}

func newReport(root string) *profile.Report {
	return &profile.Report{SchemaVersion: profile.SchemaVersion, Root: root, Languages: []profile.Language{}, Ecosystems: []profile.Finding{}, Frameworks: []profile.Finding{}, Layouts: []profile.Finding{}, Warnings: []profile.Warning{}}
}

// Preflight avoids reporting partial statistics when a filesystem tree reaches
// the same file-count limit as a Git tree. No file content is opened here.
func directoryTreeLimit(ctx context.Context, root *os.Root, limit int) (bool, error) {
	count := 0
	stop := errors.New("tree size reached")
	err := fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		count++
		if count >= limit {
			return stop
		}
		return nil
	})
	if errors.Is(err, stop) {
		return true, nil
	}
	return false, err
}
