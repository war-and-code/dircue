package providerjoin

import (
	"fmt"
	"strings"

	"dircue/pkg/mapdoc"
)

type syftReport struct {
	Descriptor struct {
		Name, Version string
	} `json:"descriptor"`
	Source struct {
		Type string `json:"type"`
	} `json:"source"`
	Artifacts []struct {
		ID, Name, Version, Type, PURL string
		Locations                     []struct{ Path, AccessPath string } `json:"locations"`
	} `json:"artifacts"`
	ArtifactRelationships []struct{ Parent, Child, Type string } `json:"artifactRelationships"`
}

func ingestSyft(data []byte, in Input, limit int, key string) (Result, error) {
	var doc syftReport
	if err := decodeOne(data, &doc); err != nil {
		return Result{}, err
	}
	// Ingest the first N records in document order; keep what fits within the limit.
	limitReached := len(doc.Artifacts)+len(doc.ArtifactRelationships) > limit
	if limitReached {
		keepArtifacts := limit
		if keepArtifacts > len(doc.Artifacts) {
			keepArtifacts = len(doc.Artifacts)
		}
		doc.Artifacts = doc.Artifacts[:keepArtifacts]
		keepRelationships := limit - keepArtifacts
		if keepRelationships < 0 {
			keepRelationships = 0
		}
		if keepRelationships > len(doc.ArtifactRelationships) {
			keepRelationships = len(doc.ArtifactRelationships)
		}
		doc.ArtifactRelationships = doc.ArtifactRelationships[:keepRelationships]
	}
	provider := doc.Descriptor.Name
	if provider == "" {
		provider = "syft"
	}
	version := fallbackVersion(doc.Descriptor.Version)
	b, reason := binding(in.Snapshot, reportIdentity{})
	tool := toolNode(provider, version, key, b, reason, nil)
	out := Result{Nodes: []mapdoc.Node{tool}}
	byArtifact := map[string]string{}
	covered := []string{}
	observations := []syftPackageObservation{}
	for index, a := range doc.Artifacts {
		paths := []string{"."}
		for _, loc := range a.Locations {
			p := loc.Path
			if p == "" {
				p = loc.AccessPath
			}
			if clean, ok := cleanSyftPath(p, doc.Source.Type); ok {
				paths = append(paths, clean)
				covered = append(covered, clean)
			}
		}
		paths = compact(paths)
		disc := a.PURL
		if disc == "" {
			disc = a.Type + ":" + a.Name + ":" + a.Version
		}
		// A provider report can contain separate occurrences with the same
		// coordinates and no usable root-relative location. Retain the
		// provider artifact identity so those occurrences cannot collapse
		// into a duplicate map node.
		artifactIdentity := a.ID
		if artifactIdentity == "" {
			artifactIdentity = fmt.Sprintf("ordinal:%d", index)
		}
		disc += "\x00artifact:" + artifactIdentity
		n := mapdoc.NewNode(mapdoc.NodePackage, paths, disc)
		n.Name = a.Name
		n.Properties = map[string]string{"package_type": a.Type}
		if a.Version != "" {
			n.Properties["version"] = a.Version
		}
		if a.PURL != "" {
			n.Properties["purl"] = a.PURL
		}
		n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
		n.Evidence = []mapdoc.Evidence{evidence(provider, version, paths[0], 0)}
		out.Nodes = append(out.Nodes, n)
		nodeIndex := len(out.Nodes) - 1
		byArtifact[a.ID] = n.ID
		e := mapdoc.NewEdge(mapdoc.EdgeAnalyzedBy, n.ID, tool.ID, "")
		e.Coverage, e.Evidence = n.Coverage, n.Evidence
		out.Edges = append(out.Edges, e)
		owner := nearestOwner(in.Nodes, paths)
		observations = append(observations, syftPackageObservation{nodeIndex: nodeIndex, key: syftPackageKey(a.Type, a.PURL, a.Name), owner: owner})
		if owner != "" {
			associated := mapdoc.NewEdge(mapdoc.EdgePackagedIn, n.ID, owner, "provider_location")
			associated.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
			associated.Evidence = n.Evidence
			out.Edges = append(out.Edges, associated)
		}
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
	reconcileSyftRequirements(&out, in, b, covered, observations)
	ledgerReason := reason
	if limitReached {
		ledgerReason = "attachment_record_limit_reached"
	}
	out.Ledger = []CoverageEntry{{Tool: provider, ReportKind: "syft-json", Scope: ".", Binding: b, Ran: true, CoveredFiles: covered, State: coverageState(covered), Reason: ledgerReason}}
	return out, nil
}

func cleanSyftPath(value, sourceType string) (string, bool) {
	v := strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	if strings.Contains(v, "://") || (len(v) >= 2 && v[1] == ':') {
		return "", false
	}
	if strings.HasPrefix(v, "/") {
		if !strings.EqualFold(sourceType, "directory") {
			return "", false
		}
		v = strings.TrimPrefix(v, "/")
	}
	return cleanReportPath(v)
}

func nearestOwner(nodes []mapdoc.Node, paths []string) string {
	best, bestLength := "", -1
	for _, p := range paths {
		if p == "." {
			continue
		}
		for _, n := range nodes {
			if n.Kind != mapdoc.NodeComponent {
				continue
			}
			for _, root := range n.Paths {
				if root == "." || p == root || strings.HasPrefix(p, root+"/") {
					if len(root) > bestLength {
						best, bestLength = n.ID, len(root)
					}
				}
			}
		}
	}
	return best
}

func coverageState(paths []string) string {
	if len(paths) == 0 {
		return "unknown"
	}
	return "covered_files_reported"
}
