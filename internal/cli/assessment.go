package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/war-and-code/dircue/pkg/assessment"
)

func writeAssessment(out io.Writer, report *assessment.Report) error {
	if report == nil {
		return nil
	}
	inventory := report.Inventory
	if _, err := fmt.Fprintf(out, "Repository measurements; source: %s\nSelected regular files: %d (%s); logical bytes: %d (%s); Linguist-vendored: %d files, %d bytes\nDeclared projects: %d (%s)%s; distinct project roots: %d (%s)%s\nWorkspace memberships: %d (%s); local dependency references: %d (%s)\n", report.Source.Mode,
		inventory.Files.Count, inventory.Files.Completeness, inventory.Bytes.Count, inventory.Bytes.Completeness, inventory.VendoredFiles.Count, inventory.VendoredBytes.Count,
		report.Projects.Count, report.Projects.Completeness, roleSummary(report.ProjectsByRole), report.ProjectRoots.Count, report.ProjectRoots.Completeness, roleSummary(report.ProjectRootsByRole),
		report.WorkspaceMembership.Count, report.WorkspaceMembership.Completeness, report.LocalDependencies.Count, report.LocalDependencies.Completeness); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Manifest filename candidates: %d (%s); unparsed documents: %d (%s)\n", report.ManifestCandidatePopulation.Count, report.ManifestCandidatePopulation.Completeness, report.UnparsedManifestCandidates.Count, report.UnparsedManifestCandidates.Completeness); err != nil {
		return err
	}
	for _, group := range report.ManifestCandidates {
		if _, err := fmt.Fprintf(out, "  %s [%s %s]: %d files; %d logical bytes\n", group.Filename, group.Ecosystem, group.Kind, group.Files, group.Bytes); err != nil {
			return err
		}
	}
	if structure := report.Structure; structure != nil {
		if _, err := fmt.Fprintf(out, "Declared groups: %d workspace/module groups; %d solution groups\nObserved dependencies: %d definite local edges (%s); %d connected project groups (%s); %d qualified references\nGroups with qualified links: %d (%s)\nStatic entry-point observations: %d declarations; %d rows; %d project associations; structural scope and uncertainty are in --json\n", structure.WorkspaceGroupCount, structure.SolutionGroupCount,
			structure.Dependencies.DefiniteEdges.Count, structure.Dependencies.DefiniteEdges.Completeness, structure.Dependencies.ConnectedGroups.Count, structure.Dependencies.ConnectedGroups.Completeness, structure.Dependencies.QualifiedReferenceCount, structure.Dependencies.ConnectedGroupsWithQualified.Count, structure.Dependencies.ConnectedGroupsWithQualified.Completeness, structure.EntryPointCount, structure.EntryPointRowCount, structure.EntryPointAssociationCount); err != nil {
			return err
		}
	}
	for _, group := range report.Lockfiles {
		if _, err := fmt.Fprintf(out, "%s lockfile associations: %d projects; %d eligible; %d covered; %d missing; %d not applicable; %d unsupported; %d unknown (%s)\n", group.Ecosystem, group.Projects.Count, group.Eligible.Count, group.Covered.Count, group.Missing.Count, group.NotApplicable.Count, group.Unsupported.Count, group.Unknown.Count, group.Projects.Completeness); err != nil {
			return err
		}
		for _, reason := range group.OutcomeReasons {
			if _, err := fmt.Fprintf(out, "  %s: %s %d\n", strings.ReplaceAll(reason.State, "_", " "), reason.Reason, reason.Count); err != nil {
				return err
			}
		}
		if c := group.Checks; c != nil && group.Covered.Count > 0 {
			if _, err := fmt.Fprintf(out, "  covered checks: %d match; %d different; %d indeterminate; %d not applicable\n", c.Match, c.Different, c.Indeterminate, c.NotApplicable); err != nil {
				return err
			}
			for _, reason := range c.IndeterminateReasons {
				if _, err := fmt.Fprintf(out, "    indeterminate: %s %d\n", reason.Reason, reason.Count); err != nil {
					return err
				}
			}
		}
		if p := group.NuGetPresence; p != nil {
			if _, err := fmt.Fprintf(out, "  lockfile presence: %d observed; %d not observed; %d unknown\n", p.Observed, p.NotObserved, p.Unknown); err != nil {
				return err
			}
		}
		for i, cause := range group.Causes {
			if i == assessmentTextCauses {
				if _, err := fmt.Fprintf(out, "  %d more cause(s) in --json\n", int64(len(group.Causes)-i)+group.OmittedCauses); err != nil {
					return err
				}
				break
			}
			if _, err := fmt.Fprintf(out, "  cause: %s %s (%d projects)\n", cause.Reason, cause.Path, cause.Count); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintln(out, "Counts describe selected evidence only. Use --json for scopes, definitions, roles, evidence, and omissions.")
	return err
}

// assessmentTextCauses bounds the causes printed per lockfile row.
const assessmentTextCauses = 5

// roleSummary renders a role partition, or nothing when every count is
// primary.
func roleSummary(roles []assessment.RoleCount) string {
	if len(roles) == 0 || len(roles) == 1 && roles[0].Role == "primary" {
		return ""
	}
	parts := make([]string, 0, len(roles))
	for _, role := range roles {
		parts = append(parts, fmt.Sprintf("%s %d", role.Role, role.Count))
	}
	return " [" + strings.Join(parts, ", ") + "]"
}
