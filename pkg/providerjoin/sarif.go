package providerjoin

import (
	"fmt"

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
		Properties map[string]any `json:"properties"`
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
	if len(doc.Runs) > limit {
		return Result{}, fmt.Errorf("report exceeds %d-record limit", limit)
	}
	var out Result
	for i, run := range doc.Runs {
		if len(run.Artifacts)+len(run.Invocations) > limit {
			return Result{}, fmt.Errorf("SARIF run exceeds %d-record limit", limit)
		}
		tool, version := run.Tool.Driver.Name, fallbackVersion(run.Tool.Driver.Version)
		if tool == "" {
			tool = "sarif"
		}
		id := identityFromMaps(run.Properties)
		b, reason := binding(in.Snapshot, id)
		covered := []string{}
		for _, a := range run.Artifacts {
			if p, ok := cleanReportPath(a.Location.URI); ok {
				covered = append(covered, p)
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
