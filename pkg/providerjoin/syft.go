package providerjoin

import (
	"fmt"
	"strings"

	"dircue/pkg/mapdoc"
)

type syftReport struct {
	Descriptor struct {
		Name, Version string
		Configuration map[string]any
	} `json:"descriptor"`
	Source struct {
		Metadata map[string]any `json:"metadata"`
	} `json:"source"`
	Artifacts []struct {
		ID, Name, Version, Type, PURL string
		Locations                     []struct{ Path, AccessPath string } `json:"locations"`
	} `json:"artifacts"`
	ArtifactRelationships []struct{ Parent, Child, Type string } `json:"artifactRelationships"`
}

func ingestSyft(data []byte, in Input, limit int) (Result, error) {
	var doc syftReport
	if err := decodeOne(data, &doc); err != nil {
		return Result{}, err
	}
	if len(doc.Artifacts)+len(doc.ArtifactRelationships) > limit {
		return Result{}, fmt.Errorf("report exceeds %d-record limit", limit)
	}
	provider := doc.Descriptor.Name
	if provider == "" {
		provider = "syft"
	}
	version := fallbackVersion(doc.Descriptor.Version)
	id := identityFromMaps(doc.Source.Metadata, doc.Descriptor.Configuration)
	b, reason := binding(in.Snapshot, id)
	key := reportKey(data)
	tool := toolNode(provider, version, key, b, reason, nil)
	out := Result{Nodes: []mapdoc.Node{tool}}
	byArtifact := map[string]string{}
	covered := []string{}
	for _, a := range doc.Artifacts {
		paths := []string{"."}
		for _, loc := range a.Locations {
			p := loc.Path
			if p == "" {
				p = loc.AccessPath
			}
			if clean, ok := cleanReportPath(p); ok {
				paths = append(paths, clean)
				covered = append(covered, clean)
			}
		}
		paths = compact(paths)
		disc := a.PURL
		if disc == "" {
			disc = a.Type + ":" + a.Name + ":" + a.Version
		}
		n := mapdoc.NewNode(mapdoc.NodePackage, paths, disc)
		n.Name = a.Name
		n.Properties = map[string]string{"provider_artifact_id": a.ID, "package_type": a.Type}
		if a.Version != "" {
			n.Properties["version"] = a.Version
		}
		if a.PURL != "" {
			n.Properties["purl"] = a.PURL
		}
		n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
		n.Evidence = []mapdoc.Evidence{evidence(provider, version, paths[0], 0)}
		out.Nodes = append(out.Nodes, n)
		byArtifact[a.ID] = n.ID
		e := mapdoc.NewEdge(mapdoc.EdgeAnalyzedBy, n.ID, tool.ID, "")
		e.Coverage, e.Evidence = n.Coverage, n.Evidence
		out.Edges = append(out.Edges, e)
	}
	for _, rel := range doc.ArtifactRelationships {
		parent, pok := byArtifact[rel.Parent]
		child, cok := byArtifact[rel.Child]
		if !pok || !cok {
			continue
		}
		var typ mapdoc.EdgeType
		switch strings.ToLower(rel.Type) {
		case "contains":
			typ = mapdoc.EdgePackagedIn
		case "dependency-of":
			typ = mapdoc.EdgeDependsOnLocal
		default:
			continue
		}
		e := mapdoc.NewEdge(typ, child, parent, "syft:"+rel.Type)
		e.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
		e.Evidence = []mapdoc.Evidence{evidence(provider, version, ".", 0)}
		out.Edges = append(out.Edges, e)
	}
	covered = compact(covered)
	out.Ledger = []CoverageEntry{{Tool: provider, Scope: ".", Binding: b, Ran: true, CoveredFiles: covered, State: coverageState(covered), Reason: reason}}
	return out, nil
}

func identityFromMaps(values ...map[string]any) reportIdentity {
	var id reportIdentity
	for _, m := range values {
		if m == nil {
			continue
		}
		if v, ok := m["dircue_snapshot_tree"].(string); ok {
			id.Tree = v
		}
		if v, ok := m["dircueSnapshotTree"].(string); ok {
			id.Tree = v
		}
		if v, ok := m["dircue_snapshot_digest"].(string); ok {
			id.Digest = v
		}
		if v, ok := m["dircueSnapshotDigest"].(string); ok {
			id.Digest = v
		}
		if v, ok := m["dircue_snapshot_algorithm"].(string); ok {
			id.Algorithm = v
		}
		if v, ok := m["dircue_snapshot_scope"].(string); ok {
			id.Scope = v
		}
	}
	if id.Digest != "" && id.Algorithm == "" {
		id.Algorithm = "sha256"
	}
	if id.Digest != "" && id.Scope == "" {
		id.Scope = "full_selected_tree"
	}
	return id
}

func coverageState(paths []string) string {
	if len(paths) == 0 {
		return "unknown"
	}
	return "covered_files_reported"
}
