package providerjoin

import (
	"fmt"
	"net/url"
	"strings"

	"dircue/pkg/mapdoc"
)

type sarifDocument struct {
	Version string `json:"version"`
	Runs    []struct {
		Tool struct {
			Driver struct{ Name, Version string } `json:"driver"`
		} `json:"tool"`
		Artifacts []struct {
			Location struct {
				URI string `json:"uri"`
			} `json:"location"`
		} `json:"artifacts"`
		Invocations []struct {
			ExecutionSuccessful        *bool `json:"executionSuccessful"`
			ToolExecutionNotifications []any `json:"toolExecutionNotifications"`
		} `json:"invocations"`
		VersionControlProvenance []struct {
			RevisionID    string `json:"revisionId"`
			RepositoryURI string `json:"repositoryUri"`
		} `json:"versionControlProvenance"`
		// Results are intentionally absent: findings are outside dircue's contract.
	} `json:"runs"`
}

func ingestSARIF(data []byte, in Input, limit int, key string) (Result, error) {
	var doc sarifDocument
	if err := decodeOne(data, &doc); err != nil {
		return Result{}, err
	}
	if doc.Version != "2.1.0" {
		return Result{}, fmt.Errorf("unsupported SARIF version %q", doc.Version)
	}
	// Ingest the first N runs in document order; truncate to keep what fits.
	docLimitReached := len(doc.Runs) > limit
	if docLimitReached {
		doc.Runs = doc.Runs[:limit]
	}
	var out Result
	for i, run := range doc.Runs {
		tool, version := run.Tool.Driver.Name, fallbackVersion(run.Tool.Driver.Version)
		if tool == "" {
			tool = "sarif"
		}
		// Ingest the first N artifacts/invocations within this run in document order.
		runLimitReached := len(run.Artifacts)+len(run.Invocations) > limit
		if runLimitReached {
			keepArtifacts := limit
			if keepArtifacts > len(run.Artifacts) {
				keepArtifacts = len(run.Artifacts)
			}
			doc.Runs[i].Artifacts = doc.Runs[i].Artifacts[:keepArtifacts]
			keepInvocations := limit - keepArtifacts
			if keepInvocations < 0 {
				keepInvocations = 0
			}
			if keepInvocations > len(run.Invocations) {
				keepInvocations = len(run.Invocations)
			}
			doc.Runs[i].Invocations = doc.Runs[i].Invocations[:keepInvocations]
			run = doc.Runs[i]
		}
		b, reason := bindingSARIF(in.Snapshot, run.VersionControlProvenance)
		if runLimitReached {
			reason = "attachment_record_limit_reached"
		}
		covered := []string{}
		for _, a := range run.Artifacts {
			decoded, decodeErr := url.PathUnescape(a.Location.URI)
			if decodeErr == nil {
				if p, ok := cleanReportPath(decoded); ok {
					covered = append(covered, p)
				}
			}
		}
		covered = compact(covered)
		state := coverageState(covered)
		failed, notifications := false, 0
		for _, inv := range run.Invocations {
			if inv.ExecutionSuccessful != nil && !*inv.ExecutionSuccessful {
				failed = true
			}
			notifications += len(inv.ToolExecutionNotifications)
		}
		if failed {
			state = "tool_error"
			if reason == "" {
				reason = "tool_execution_failed"
			}
		}
		facts := []mapdoc.Fact{{Kind: "run_metadata", State: state, Properties: map[string]string{"artifacts": fmt.Sprint(len(covered)), "notifications": fmt.Sprint(notifications)}, Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}, Evidence: []mapdoc.Evidence{evidence(tool, version, ".", 0)}}}
		n := toolNode(tool, version, fmt.Sprintf("%s:run:%d", key, i), b, reason, facts)
		out.Nodes = append(out.Nodes, n)
		for _, owner := range ownersForPaths(in.Nodes, covered) {
			e := mapdoc.NewEdge(mapdoc.EdgeAnalyzedBy, owner, n.ID, "")
			e.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
			e.Evidence = []mapdoc.Evidence{evidence(tool, version, ".", 0)}
			out.Edges = append(out.Edges, e)
		}
		out.Ledger = append(out.Ledger, CoverageEntry{Tool: tool, ReportKind: "sarif", Scope: ".", Binding: b, Ran: true, CoveredFiles: covered, State: state, Reason: reason})
	}
	return out, nil
}

func bindingSARIF(snapshot Snapshot, provenance []struct {
	RevisionID    string `json:"revisionId"`
	RepositoryURI string `json:"repositoryUri"`
}) (Binding, string) {
	// Collect non-empty revision IDs so we can fall back when the snapshot has no commit.
	var firstRevision string
	for _, source := range provenance {
		if rev := strings.TrimSpace(source.RevisionID); rev != "" {
			firstRevision = rev
			break
		}
	}
	if firstRevision == "" {
		return binding(snapshot, reportIdentity{})
	}
	if snapshot.Commit == "" {
		return binding(snapshot, reportIdentity{Commit: firstRevision})
	}

	// Per-repository semantics: entries for other repositories are ignored.
	// "Same repository" means identical normalized repositoryUri, or both empty.
	normalizeURI := func(uri string) string {
		return strings.TrimRight(strings.ToLower(strings.TrimSpace(uri)), "/")
	}

	// Find the focal repository: the normalized URI that has at least one entry
	// whose revisionId matches the snapshot commit.
	focalURI := ""
	focalFound := false
	for _, source := range provenance {
		if strings.EqualFold(strings.TrimSpace(source.RevisionID), snapshot.Commit) {
			focalURI = normalizeURI(source.RepositoryURI)
			focalFound = true
			break
		}
	}

	if !focalFound {
		// No entry in any repository matches the snapshot commit.
		return BindingMismatch, "report_commit_mismatch"
	}

	// Check for conflicts: another entry in the same (focal) repository with a
	// different revisionId. If any conflict exists the binding cannot be verified.
	for _, source := range provenance {
		if normalizeURI(source.RepositoryURI) == focalURI {
			if !strings.EqualFold(strings.TrimSpace(source.RevisionID), snapshot.Commit) {
				return BindingUnknown, "vcp_same_repository_revision_conflict"
			}
		}
	}

	return BindingVerified, ""
}

func ownersForPaths(nodes []mapdoc.Node, paths []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, p := range paths {
		for _, n := range nodes {
			if n.Kind != mapdoc.NodeComponent && n.Kind != mapdoc.NodeDeployable {
				continue
			}
			for _, root := range n.Paths {
				if root == "." || p == root || len(p) > len(root) && p[:len(root)+1] == root+"/" {
					if !seen[n.ID] {
						seen[n.ID] = true
						out = append(out, n.ID)
					}
					break
				}
			}
		}
	}
	return out
}
