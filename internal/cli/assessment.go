package cli

import (
	"fmt"
	"io"

	"github.com/war-and-code/dircue/pkg/assessment"
)

func writeAssessment(out io.Writer, report *assessment.Report) error {
	if report == nil {
		return nil
	}
	if _, err := fmt.Fprintf(out, "Repository measurements; source: %s\nSelected regular files: %d (%s); logical bytes: %d (%s)\nDeclared projects: %d (%s); distinct project roots: %d (%s)\nWorkspace memberships: %d (%s); local dependency references: %d (%s)\n", report.Source.Mode,
		report.Inventory.Files.Count, report.Inventory.Files.Completeness, report.Inventory.Bytes.Count, report.Inventory.Bytes.Completeness,
		report.Projects.Count, report.Projects.Completeness, report.ProjectRoots.Count, report.ProjectRoots.Completeness,
		report.WorkspaceMembership.Count, report.WorkspaceMembership.Completeness, report.LocalDependencies.Count, report.LocalDependencies.Completeness); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Manifest filename candidates: %d (%s); unparsed documents: %d (%s)\n", report.ManifestCandidatePopulation.Count, report.ManifestCandidatePopulation.Completeness, report.UnparsedManifestCandidates.Count, report.UnparsedManifestCandidates.Completeness); err != nil {
		return err
	}
	for _, group := range report.ManifestCandidates {
		if _, err := fmt.Fprintf(out, "  %s [%s]: %d files; %d logical bytes\n", group.Filename, group.Ecosystem, group.Files, group.Bytes); err != nil {
			return err
		}
	}
	for _, group := range report.Lockfiles {
		if _, err := fmt.Fprintf(out, "%s lockfile associations: %d projects; %d eligible; %d covered; %d missing; %d not applicable; %d unsupported; %d unknown (%s)\n", group.Ecosystem, group.Projects.Count, group.Eligible.Count, group.Covered.Count, group.Missing.Count, group.NotApplicable.Count, group.Unsupported.Count, group.Unknown.Count, group.Projects.Completeness); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(out, "Counts describe selected evidence, not repository size policy, package resolution, or successful builds. Use --json for scopes, definitions, evidence, and omissions.")
	return err
}
