package providerjoin

import (
	"fmt"
	"strings"

	"dircue/pkg/mapdoc"
)

type noirReport struct {
	Version    string         `json:"version"`
	Properties map[string]any `json:"properties"`
	Endpoints  []noirEndpoint `json:"endpoints"`
}
type noirEndpoint struct {
	Method, Path string
	Params       []struct{ Name, Type string } `json:"params"`
	File         string                        `json:"file"`
	Line         int                           `json:"line"`
	Source       *struct {
		Path string `json:"path"`
		Line int    `json:"line"`
	} `json:"source"`
}

func ingestNoir(data []byte, in Input, limit int) (Result, error) {
	var doc noirReport
	if err := decodeOne(data, &doc); err != nil { // Noir also emits a bare endpoint array.
		var endpoints []noirEndpoint
		if arrErr := decodeOne(data, &endpoints); arrErr != nil {
			return Result{}, err
		}
		doc.Endpoints = endpoints
	}
	if len(doc.Endpoints) > limit {
		return Result{}, fmt.Errorf("report exceeds %d-record limit", limit)
	}
	version := fallbackVersion(doc.Version)
	b, reason := binding(in.Snapshot, identityFromMaps(doc.Properties))
	key := reportKey(data)
	tool := toolNode("noir", version, key, b, reason, nil)
	out := Result{Nodes: []mapdoc.Node{tool}}
	covered := []string{}
	for _, endpoint := range doc.Endpoints {
		method := strings.ToUpper(strings.TrimSpace(endpoint.Method))
		if method == "" {
			method = "ANY"
		}
		route := strings.TrimSpace(endpoint.Path)
		if route == "" {
			continue
		}
		file, line := endpoint.File, endpoint.Line
		if endpoint.Source != nil {
			if file == "" {
				file = endpoint.Source.Path
			}
			if line == 0 {
				line = endpoint.Source.Line
			}
		}
		clean, ok := cleanReportPath(file)
		if !ok {
			clean = "."
		} else {
			covered = append(covered, clean)
		}
		n := mapdoc.NewNode(mapdoc.NodeInterface, []string{clean}, "noir:"+method+":"+route)
		n.Name = method + " " + route
		n.Properties = map[string]string{"kind": "http", "method": method, "route": route}
		n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
		n.Evidence = []mapdoc.Evidence{evidence("noir", version, clean, line)}
		for _, p := range endpoint.Params {
			n.Facts = append(n.Facts, mapdoc.Fact{Kind: "parameter", Name: p.Name, Value: p.Type, Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}, Evidence: n.Evidence})
		}
		out.Nodes = append(out.Nodes, n)
		e := mapdoc.NewEdge(mapdoc.EdgeAnalyzedBy, n.ID, tool.ID, "")
		e.Coverage = n.Coverage
		e.Evidence = n.Evidence
		out.Edges = append(out.Edges, e)
		for _, owner := range ownersForPaths(in.Nodes, []string{clean}) {
			x := mapdoc.NewEdge(mapdoc.EdgeExposes, owner, n.ID, "provider:noir")
			x.Coverage = n.Coverage
			x.Evidence = n.Evidence
			out.Edges = append(out.Edges, x)
		}
	}
	covered = compact(covered)
	out.Ledger = []CoverageEntry{{Tool: "noir", Scope: ".", Binding: b, Ran: true, CoveredFiles: covered, State: coverageState(covered), Reason: reason}}
	return out, nil
}
