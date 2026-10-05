package cli

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/spf13/cobra"
	"github.com/war-and-code/dircue/pkg/packageevidence"
	"github.com/war-and-code/dircue/pkg/projects"
)

func addPackageFlags(cmd *cobra.Command, opts *options) {
	cmd.Flags().StringVar(&opts.syftReport, "syft-report", "", "Import a previously generated Syft JSON report; does not execute Syft")
	cmd.Flags().StringVar(&opts.syftRoot, "syft-root", "", "Explicit report coordinate root mapped to this inventory (commonly /)")
	cmd.Flags().Int64Var(&opts.syftMaxBytes, "syft-report-max-bytes", 16<<20, "Maximum imported report bytes (at most 134217728)")
	cmd.Flags().StringVar(&opts.syftReportSHA256, "syft-report-sha256", "", "Report digest for a caller-supplied source binding; requires --syft-source-tree")
	cmd.Flags().StringVar(&opts.syftSourceTree, "syft-source-tree", "", "Git tree the caller attests was cataloged; requires --syft-report-sha256")
}

func loadPackageEvidence(cmd *cobra.Command, opts *options, mode string) (*packageevidence.Report, error) {
	if mode != "packages" && mode != "all" && mode != "assessment" {
		return nil, nil
	}
	if opts.syftReport == "" {
		if mode == "packages" {
			return nil, fmt.Errorf("packages analysis requires --syft-report")
		}
		for _, name := range []string{"syft-report", "syft-root", "syft-report-max-bytes", "syft-report-sha256", "syft-source-tree"} {
			if cmd.Flags().Changed(name) {
				return nil, fmt.Errorf("--%s requires a nonempty --syft-report", name)
			}
		}
		return nil, nil
	}
	if opts.syftMaxBytes < 1 || opts.syftMaxBytes > 128<<20 {
		return nil, fmt.Errorf("--syft-report-max-bytes must be between 1 and 134217728")
	}
	if cmd.Flags().Changed("syft-root") {
		mapping := packageevidence.Mapping{ReportRoot: opts.syftRoot, InventoryRoot: "."}
		if err := mapping.Validate(); err != nil {
			return nil, fmt.Errorf("--syft-root must be a nonempty slash-separated coordinate root without parent traversal, control characters, query, or fragment")
		}
	}
	if (opts.syftReportSHA256 == "") != (opts.syftSourceTree == "") {
		return nil, fmt.Errorf("--syft-report-sha256 and --syft-source-tree must be supplied together")
	}
	if opts.syftSourceTree != "" {
		if !validHexDigest(opts.syftReportSHA256, 64) || !validHexDigest(opts.syftSourceTree, 40) {
			return nil, fmt.Errorf("source binding requires a 64-digit SHA-256 report digest and a 40-digit Git tree ID")
		}
		if opts.source == "directory" {
			return nil, fmt.Errorf("--syft-source-tree requires a Git content source")
		}
	}
	file, err := openInputFile(opts.syftReport, "Syft report")
	if err != nil {
		return nil, fmt.Errorf("open Syft report: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect Syft report: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("Syft report must be a regular file")
	}
	if info.Size() > opts.syftMaxBytes {
		return nil, fmt.Errorf("--syft-report exceeds --syft-report-max-bytes (%d): %w", opts.syftMaxBytes, packageevidence.ErrLimit)
	}
	report, err := packageevidence.Import(cmd.Context(), file, packageevidence.Options{Limits: packageevidence.Limits{Bytes: opts.syftMaxBytes}})
	if errors.Is(err, packageevidence.ErrUnsupported) {
		return nil, fmt.Errorf("--syft-report requires native Syft JSON schema %s: %w", packageevidence.SupportedSchema, err)
	}
	if err != nil {
		return nil, fmt.Errorf("import --syft-report: %w", err)
	}
	return report, nil
}

func validHexDigest(value string, size int) bool {
	if len(value) != size {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func associatePackages(input *packageevidence.Report, inventory *projects.Report, opts *options) (*packageevidence.Report, error) {
	ctx := packageevidence.Context{}
	if inventory != nil {
		ctx.ProjectStatus = inventory.Status
		for _, p := range inventory.Projects {
			ctx.Projects = append(ctx.Projects, packageevidence.Project{ID: p.ID, Root: p.Root})
		}
		if inventory.Source == "git" && inventory.Tree != "" {
			ctx.SourceIdentity = packageevidence.Identity{Kind: "git-tree", Algorithm: "git-sha1", Digest: inventory.Tree}
		}
	}
	if opts.syftRoot != "" {
		ctx.Mapping = &packageevidence.Mapping{ReportRoot: opts.syftRoot, InventoryRoot: "."}
	}
	if opts.syftSourceTree != "" {
		if ctx.SourceIdentity.Digest == "" {
			return nil, fmt.Errorf("--syft-source-tree requires an available selected Git tree")
		}
		ctx.Binding = &packageevidence.Binding{ReportSHA256: opts.syftReportSHA256, SourceIdentity: packageevidence.Identity{Kind: "git-tree", Algorithm: "git-sha1", Digest: opts.syftSourceTree}}
	}
	return packageevidence.Associate(input, ctx)
}

func writePackageEvidence(out io.Writer, r *packageevidence.Report) error {
	if r == nil {
		return nil
	}
	if _, err := fmt.Fprintf(out, "Package evidence: %s %s; status: %s; source match: %s\n%d packages; %d relationships; report SHA-256: %s\n", r.Provider.Name, r.Provider.Version, r.Status, r.Source.Match, len(r.Packages), len(r.Relationships), r.ReportSHA256); err != nil {
		return err
	}
	for _, p := range r.Packages {
		if _, err := fmt.Fprintf(out, "%q %q (%s; %s)\n", p.Name, p.Version, p.Type, p.Basis); err != nil {
			return err
		}
	}
	counts := map[string]int{}
	for _, p := range r.Packages {
		for _, loc := range p.Locations {
			counts[loc.Association]++
		}
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if _, err := fmt.Fprintf(out, "Package locations, %s: %d\n", key, counts[key]); err != nil {
			return err
		}
	}
	for _, d := range r.Diagnostics {
		if _, err := fmt.Fprintf(out, "%s: %d\n", d.Code, d.Count); err != nil {
			return err
		}
	}
	return nil
}

// Check the open handle as well as its name: a path can change before opening.
func requireRegularInput(filename, label string) error {
	info, err := os.Lstat(filename)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s must be a regular file", label)
	}
	return nil
}
