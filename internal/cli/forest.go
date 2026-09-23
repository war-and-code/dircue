package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"dircue/pkg/deployables"
	"dircue/pkg/detectors"
	"dircue/pkg/forest"
	"dircue/pkg/intentmap"
	"dircue/pkg/mapbuild"
	"dircue/pkg/mapdoc"
	"dircue/pkg/profile"
	"dircue/pkg/scanner"
	"github.com/spf13/cobra"
)

// ForestDocument is the top-level forest output document.
type ForestDocument struct {
	SchemaVersion    string                    `json:"schema_version"`
	Kind             string                    `json:"kind"`
	Status           mapdoc.CoverageStatus     `json:"status"`
	Source           ForestSource              `json:"source"`
	Coverage         []mapdoc.QuestionCoverage `json:"coverage"`
	Roots            []ForestRoot              `json:"roots"`
	EnvironmentTrees []ForestEnvTree           `json:"environment_trees,omitempty"`
	Residual         *mapdoc.Document          `json:"residual"`
	// ResidualTotals counts every regular file in the residual scan exactly once,
	// sourced directly from the scanner's inventory summary (not from map nodes).
	ResidualTotals *ResidualTotals `json:"residual_totals,omitempty"`
}

// ResidualTotals holds a canonical file and byte count for the residual scan.
// Each regular file is counted exactly once regardless of its content role.
type ResidualTotals struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}

// ForestSource describes the input to a forest scan.
type ForestSource struct {
	Mode string `json:"mode"`
	Path string `json:"path"`
}

// ForestRoot holds the identity and map for one discovered root.
type ForestRoot struct {
	Path           string           `json:"path"`
	Kind           string           `json:"kind"`
	HEAD           string           `json:"head"`
	Commit         string           `json:"commit,omitempty"`
	Tree           string           `json:"tree,omitempty"`
	CommitterTime  int64            `json:"committer_time,omitempty"`
	Remotes        []forest.Remote  `json:"remotes"`
	SubmoduleOf    string           `json:"submodule_of,omitempty"`
	IdentityStatus string           `json:"identity_status"`
	IdentityReason string           `json:"identity_reason,omitempty"`
	Map            *mapdoc.Document `json:"map,omitempty"`
}

// ForestEnvTree summarizes a recognized environment or build-output tree.
type ForestEnvTree struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Ecosystem  string `json:"ecosystem"`
	Marker     string `json:"marker"`
	Basis      string `json:"basis"`
	Entries    int64  `json:"entries"`
	Bytes      int64  `json:"bytes"`
	Bounded    bool   `json:"bounded"`
	LowerBound bool   `json:"lower_bound,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// runForest implements `dircue map --forest PATH`.
func runForest(ctx context.Context, cmd *cobra.Command, inputPath string, summary bool, opts *options, settings resolvedMapSettings) error {
	abs, err := filepath.Abs(inputPath)
	if err != nil {
		return fmt.Errorf("forest: resolve path: %w", err)
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return fmt.Errorf("--forest requires a directory")
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return fmt.Errorf("forest: open root: %w", err)
	}
	defer root.Close()

	// Phase 1: discover roots and env trees.
	discoverOpts := forest.DiscoverOptions{
		RootCap:        1000,
		IncludeRemotes: true,
		SummarizeTrees: true,
		EnvTreeCap:     1_000_000,
	}
	discResult := forest.DiscoverRoots(root, discoverOpts)

	// Build a set of excluded paths (roots and env tree roots) for residual scan.
	excludedPaths := map[string]bool{}
	for _, r := range discResult.Roots {
		excludedPaths[r.Path] = true
	}
	for _, et := range discResult.EnvTrees {
		excludedPaths[et.Path] = true
	}

	// Phase 2: build per-root maps.
	forestRoots := make([]ForestRoot, 0, len(discResult.Roots))
	for _, r := range discResult.Roots {
		fr := ForestRoot{
			Path:           r.Path,
			Kind:           string(r.Kind),
			HEAD:           r.HEAD,
			Commit:         r.Commit,
			Tree:           r.Tree,
			CommitterTime:  r.CommitterTime,
			Remotes:        r.Remotes,
			SubmoduleOf:    r.SubmoduleOf,
			IdentityStatus: string(r.IdentityStatus),
			IdentityReason: r.IdentityReason,
		}
		// Build a map for this root in Git mode (committed HEAD tree).
		if r.IdentityStatus == forest.IdentityResolved && r.Kind != forest.RootGitBare {
			rootAbs := filepath.Join(abs, r.Path)
			m, mapErr := buildRootMap(ctx, rootAbs, settings)
			if mapErr == nil {
				fr.Map = m
			}
		}
		forestRoots = append(forestRoots, fr)
	}

	// Phase 3: residual map (directory mode, excluding roots and env trees).
	residualReport, err := scanner.Scan(ctx, abs, scanner.Options{
		Source:              "directory",
		ErrorPolicy:         scanner.ErrorPolicy(opts.onError),
		Workers:             settings.Workers,
		MaxTreeSize:         settings.MaxFiles,
		MaxFileBytes:        settings.MaxFileBytes,
		GitObjectCacheBytes: settings.GitCacheBytes,
		SummarizeTrees:      false, // env trees already discovered in phase 1
		ExcludePaths:        excludedPaths,
		Detectors:           []profile.Detector{},
		Discovery:           true,
		Declarations:        true,
		Formats:             true,
	})
	if err != nil {
		return fmt.Errorf("forest residual scan: %w", err)
	}
	// Add env tree summaries to residual report for coverage accounting.
	for _, et := range discResult.EnvTrees {
		residualReport.SummarizedTrees = append(residualReport.SummarizedTrees, profile.SummarizedTree{
			Path: et.Path, Kind: string(et.Kind), Ecosystem: et.Ecosystem,
			Marker: et.Marker, Basis: et.Basis,
			Entries: et.Entries, Bytes: et.Bytes, Bounded: et.Bounded,
			LowerBound: et.LowerBound, Reason: et.Reason,
		})
	}

	// Compute residual totals from the scanner's inventory — each regular file
	// is counted exactly once here, unlike the overlapping map node properties.
	var residualTotals *ResidualTotals
	if residualReport.Discovery != nil {
		inv := residualReport.Discovery.Inventory
		residualTotals = &ResidualTotals{
			Files: inv.Files,
			Bytes: inv.Bytes,
		}
	} else {
		// Fallback: sum language bytes when discovery report is absent.
		var totalFiles, totalBytes int64
		for _, lang := range residualReport.Languages {
			totalFiles += lang.FileCount
			totalBytes += lang.Bytes
		}
		residualTotals = &ResidualTotals{Files: totalFiles, Bytes: totalBytes}
	}

	deployObserver := deployables.NewCollector(deployables.Options{})
	intentObserver := intentmap.New(intentmap.Options{})
	residualIntent, _ := intentObserver.Finish(ctx)
	residualDoc, err := mapbuild.Build(residualReport, mapbuild.Options{
		Deployables: deployObserver.Finish(),
		Intent:      residualIntent,
	})
	if err != nil {
		return fmt.Errorf("forest residual map: %w", err)
	}

	// Build env tree list for output.
	envTrees := make([]ForestEnvTree, 0, len(discResult.EnvTrees))
	for _, et := range discResult.EnvTrees {
		envTrees = append(envTrees, ForestEnvTree{
			Path:       et.Path,
			Kind:       string(et.Kind),
			Ecosystem:  et.Ecosystem,
			Marker:     et.Marker,
			Basis:      et.Basis,
			Entries:    et.Entries,
			Bytes:      et.Bytes,
			Bounded:    et.Bounded,
			LowerBound: et.LowerBound,
			Reason:     et.Reason,
		})
	}

	// Assemble forest document.
	forestStatus := mapdoc.CoverageComplete
	var coverageReasons []string
	if discResult.Partial {
		forestStatus = mapdoc.CoveragePartial
		coverageReasons = append(coverageReasons, discResult.PartialReason)
	}
	for _, r := range forestRoots {
		if r.IdentityStatus == string(forest.IdentityUnknown) {
			forestStatus = mapdoc.CoveragePartial
		}
	}

	rootsStatus := mapdoc.CoverageComplete
	if discResult.Partial {
		rootsStatus = mapdoc.CoveragePartial
		_ = coverageReasons
	}

	doc := ForestDocument{
		SchemaVersion: "1.0.0",
		Kind:          "forest",
		Status:        forestStatus,
		Source:        ForestSource{Mode: "directory", Path: abs},
		Coverage: []mapdoc.QuestionCoverage{
			{Question: "roots", Scope: ".", Coverage: mapdoc.Coverage{
				Status: rootsStatus,
				Reasons: func() []string {
					if discResult.Partial {
						return []string{discResult.PartialReason}
					}
					return []string{}
				}(),
			}},
			{Question: "residual", Scope: ".", Coverage: mapdoc.Coverage{Status: residualDoc.Status, Reasons: []string{}}},
			{Question: "environment_trees", Scope: ".", Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete, Reasons: []string{}}},
		},
		Roots:            forestRoots,
		EnvironmentTrees: envTrees,
		Residual:         &residualDoc,
		ResidualTotals:   residualTotals,
	}

	if summary || !opts.json && isTerminalWriter(cmd.OutOrStdout()) {
		return writeForestSummary(cmd.OutOrStdout(), doc)
	}

	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal forest: %w", err)
	}
	data = append(data, '\n')
	_, err = cmd.OutOrStdout().Write(data)
	return err
}

// buildRootMap runs a Git-mode map on a single discovered root.
func buildRootMap(ctx context.Context, rootPath string, settings resolvedMapSettings) (*mapdoc.Document, error) {
	deployObserver := deployables.NewCollector(deployables.Options{})
	intentObserver := intentmap.New(intentmap.Options{})
	hooks := append(detectors.Default(), deployObserver, intentObserver)
	report, err := scanner.Scan(ctx, rootPath, scanner.Options{
		Source:              "auto",
		ErrorPolicy:         scanner.ErrorPolicyContinue,
		Workers:             settings.Workers,
		MaxTreeSize:         settings.MaxFiles,
		MaxFileBytes:        settings.MaxFileBytes,
		GitObjectCacheBytes: settings.GitCacheBytes,
		Detectors:           hooks,
		Discovery:           true,
		Declarations:        true,
		Formats:             true,
		Availability:        true,
		Environments:        true,
		Registries:          true,
	})
	if err != nil {
		return nil, err
	}
	if report.Declarations != nil {
		intentObserver.AddDeclarations(report.Declarations.Projects)
	}
	intentReport, err := intentObserver.Finish(ctx)
	if err != nil {
		return nil, err
	}
	doc, err := mapbuild.Build(report, mapbuild.Options{
		Deployables: deployObserver.Finish(),
		Intent:      intentReport,
	})
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

// writeForestSummary writes a one-screen human-readable summary of a forest.
func writeForestSummary(out io.Writer, doc ForestDocument) error {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	line("Forest: %s (%d roots, %d environment trees)", doc.Status, len(doc.Roots), len(doc.EnvironmentTrees))
	line("")

	if len(doc.Roots) > 0 {
		line("Roots:")
		line("  %-30s %-14s %-9s %-9s %-12s %-5s %-5s %s",
			"Path", "Kind", "Branch", "Commit", "Date", "Comp", "Lang", "Top languages / Remote host")
		for _, r := range doc.Roots {
			branch := r.HEAD
			if strings.HasPrefix(branch, "refs/heads/") {
				branch = strings.TrimPrefix(branch, "refs/heads/")
			}
			if len(branch) > 9 {
				branch = branch[:8] + "…"
			}
			shortCommit := r.Commit
			if len(shortCommit) > 8 {
				shortCommit = shortCommit[:8]
			}
			dateStr := ""
			if r.CommitterTime > 0 {
				dateStr = time.Unix(r.CommitterTime, 0).UTC().Format("2006-01-02")
			}
			remoteHost := ""
			for _, rem := range r.Remotes {
				if rem.Name == "origin" || remoteHost == "" {
					remoteHost = extractHost(rem.URL)
				}
			}

			// Extract top 3 languages and component count from the root map.
			compCount := 0
			var topLangs []string
			if r.Map != nil {
				type langEntry struct {
					name  string
					bytes int64
				}
				var langs []langEntry
				for _, n := range r.Map.Nodes {
					if n.Kind == mapdoc.NodeContent && n.Properties["role"] == "language_population" {
						if langName := n.Properties["language"]; langName != "" {
							b, _ := strconv.ParseInt(n.Properties["bytes"], 10, 64)
							langs = append(langs, langEntry{langName, b})
						}
					}
					if n.Kind == mapdoc.NodeComponent {
						compCount++
					}
				}
				sort.Slice(langs, func(i, j int) bool { return langs[i].bytes > langs[j].bytes })
				for i, l := range langs {
					if i >= 3 {
						break
					}
					topLangs = append(topLangs, l.name)
				}
			}
			langStr := strings.Join(topLangs, ", ")
			if langStr == "" {
				langStr = remoteHost
			} else if remoteHost != "" {
				langStr = langStr + " · " + remoteHost
			}

			line("  %-30s %-14s %-9s %-9s %-12s %-5d %-5d %s",
				truncate(r.Path, 30), r.Kind, branch, shortCommit, dateStr,
				compCount, len(topLangs), langStr)
		}
		line("")
	}

	if len(doc.EnvironmentTrees) > 0 {
		// Group by kind+ecosystem.
		type ecosystemKey struct{ kind, ecosystem string }
		type ecosystemStats struct {
			count   int
			entries int64
			bytes   int64
		}
		grouped := map[ecosystemKey]*ecosystemStats{}
		var order []ecosystemKey
		for _, et := range doc.EnvironmentTrees {
			k := ecosystemKey{et.Kind, et.Ecosystem}
			if _, ok := grouped[k]; !ok {
				order = append(order, k)
				grouped[k] = &ecosystemStats{}
			}
			s := grouped[k]
			s.count++
			s.entries += et.Entries
			s.bytes += et.Bytes
		}
		sort.Slice(order, func(i, j int) bool {
			if order[i].kind != order[j].kind {
				return order[i].kind < order[j].kind
			}
			return order[i].ecosystem < order[j].ecosystem
		})
		line("Environment trees:")
		for _, k := range order {
			s := grouped[k]
			line("  %-20s %-20s  %d tree(s), %s entries, %s",
				k.kind, k.ecosystem, s.count,
				formatCount(s.entries), formatBytes(s.bytes))
		}
		line("")
	}

	// Residual totals from the canonical inventory (each file counted once).
	if doc.ResidualTotals != nil {
		line("Residual (unrooted): %s files, %s",
			formatCount(doc.ResidualTotals.Files), formatBytes(doc.ResidualTotals.Bytes))
		line("")
	} else if doc.Residual != nil {
		// Legacy fallback: first language_population node avoids double-counting.
		var residualFiles, residualBytes int64
		for _, n := range doc.Residual.Nodes {
			if n.Kind == mapdoc.NodeContent && n.Properties["scope"] == "inventory_population" {
				if f, err := strconv.ParseInt(n.Properties["files"], 10, 64); err == nil {
					residualFiles += f
				}
				if bv, err := strconv.ParseInt(n.Properties["bytes"], 10, 64); err == nil {
					residualBytes += bv
				}
			}
		}
		line("Residual (unrooted): %s files, %s", formatCount(residualFiles), formatBytes(residualBytes))
		line("")
	}

	// Unknowns.
	var unknowns []string
	for _, r := range doc.Roots {
		if r.IdentityStatus == string(forest.IdentityUnknown) {
			unknowns = append(unknowns, r.Path+" ("+r.IdentityReason+")")
		}
	}
	if len(unknowns) > 0 {
		line("Unknowns:")
		for _, u := range unknowns {
			line("  %s", u)
		}
	}

	_, err := io.WriteString(out, b.String())
	return err
}

func extractHost(u string) string {
	// For scp-like: host:path
	if !strings.Contains(u, "://") {
		if colon := strings.IndexByte(u, ':'); colon >= 0 {
			return u[:colon]
		}
		return u
	}
	// For URL scheme://host/...
	if idx := strings.Index(u, "://"); idx >= 0 {
		rest := u[idx+3:]
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			return rest[:slash]
		}
		return rest
	}
	return u
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func formatCount(n int64) string {
	if n < 1000 {
		return strconv.FormatInt(n, 10)
	}
	if n < 1_000_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
}

func formatBytes(b int64) string {
	switch {
	case b < 1024:
		return strconv.FormatInt(b, 10) + " B"
	case b < 1024*1024:
		return fmt.Sprintf("%.1f KiB", float64(b)/1024)
	case b < 1024*1024*1024:
		return fmt.Sprintf("%.1f MiB", float64(b)/1024/1024)
	default:
		return fmt.Sprintf("%.1f GiB", float64(b)/1024/1024/1024)
	}
}

func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
