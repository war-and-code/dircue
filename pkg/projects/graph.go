package projects

import (
	"path"
	"slices"
	"strings"
)

// GraphReport describes a static declaration graph, not evaluated build membership.
// Only included edges contribute to degrees, weak components, and cycles.
type GraphReport struct {
	Provider        string           `json:"provider"`
	ProviderVersion string           `json:"provider_version"`
	Kind            string           `json:"kind"`
	Status          string           `json:"status"`
	Reason          string           `json:"reason,omitempty"`
	Source          string           `json:"source"`
	Tree            string           `json:"tree,omitempty"`
	InputStatus     string           `json:"input_status"`
	EdgeRule        string           `json:"edge_rule"`
	Coverage        GraphCoverage    `json:"coverage"`
	Nodes           []GraphNode      `json:"nodes"`
	Edges           []GraphEdge      `json:"edges"`
	Components      []GraphComponent `json:"components"`
	Cycles          []GraphCycle     `json:"cycles"`
	Diagnostics     []Diagnostic     `json:"diagnostics"`
}

type GraphNode struct {
	ID        string   `json:"id"`
	Root      string   `json:"root"`
	Evidence  []string `json:"evidence"`
	Ambiguous bool     `json:"ambiguous"`
	FanIn     int      `json:"fan_in"`
	FanOut    int      `json:"fan_out"`
	Component string   `json:"component"`
	Cyclic    bool     `json:"cyclic"`
}

// Certainty and Resolution are independent: a conditional reference can point
// to a missing target. Included is true only for an unconditional in-scope edge.
type GraphEdge struct {
	Source       string `json:"source"`
	Target       string `json:"target,omitempty"`
	Value        string `json:"value"`
	Evidence     string `json:"evidence"`
	Condition    string `json:"condition,omitempty"`
	State        string `json:"state"`
	TargetStatus string `json:"target_status"`
	Certainty    string `json:"certainty"`
	Resolution   string `json:"resolution"`
	Included     bool   `json:"included"`
}

// Components are weakly connected components, including isolated vertices.
// IDs are the lexically first manifest ID in each sorted member list.
type GraphComponent struct {
	ID    string   `json:"id"`
	Nodes []string `json:"nodes"`
	Edges int      `json:"edges"`
}

// A cycle is a strongly connected component with multiple nodes or a self edge.
// It describes declared references; conditions and unresolved edges are excluded.
type GraphCycle struct {
	ID    string   `json:"id"`
	Nodes []string `json:"nodes"`
}

// Observation counts apply after identical edge observations are deduplicated.
// UniqueEdges counts distinct included source/target pairs. Other edge counts
// describe independent axes and must not be added to obtain a total. Target
// counts count reference observations, not distinct target paths.
type GraphCoverage struct {
	Projects               int            `json:"projects"`
	ReferenceObservations  int            `json:"reference_observations"`
	DuplicateObservations  int            `json:"duplicate_observations"`
	IncludedObservations   int            `json:"included_observations"`
	UniqueEdges            int            `json:"unique_edges"`
	ConditionalEdges       int            `json:"conditional_edges"`
	UnresolvedEdges        int            `json:"unresolved_edges"`
	MissingTargets         int            `json:"missing_targets"`
	ExternalTargets        int            `json:"external_targets"`
	UnrecognizedTargets    int            `json:"unrecognized_targets"`
	AmbiguousTargets       int            `json:"ambiguous_targets"`
	InputOmittedFiles      int64          `json:"input_omitted_files"`
	ExcludedProjectKinds   map[string]int `json:"excluded_project_kinds"`
	ExcludedReferenceKinds map[string]int `json:"excluded_reference_kinds"`
}

// AnalyzeGraph reuses a completed project inventory without opening files or
// evaluating repository code. Traversals are iterative and linear after sorting.
// Input slices are never modified. Missing input and an empty supported scope
// have distinct statuses: skipped and not_applicable, respectively.
func AnalyzeGraph(input *Report) *GraphReport {
	r := &GraphReport{Provider: "dircue", ProviderVersion: "1.0.0", Kind: "dotnet-project-reference", Status: "complete", Source: "unavailable", InputStatus: "unavailable", EdgeRule: "Direct, unconditional project-manifest ProjectReference declarations to present parsed .NET projects; not evaluated build membership.", Coverage: GraphCoverage{ExcludedProjectKinds: map[string]int{}, ExcludedReferenceKinds: map[string]int{}}, Nodes: []GraphNode{}, Edges: []GraphEdge{}, Components: []GraphComponent{}, Cycles: []GraphCycle{}, Diagnostics: []Diagnostic{}}
	if input == nil {
		r.Status = "skipped"
		r.Reason = "project_inventory_unavailable"
		return r
	}
	r.Source = input.Source
	r.Tree = input.Tree
	r.InputStatus = input.Status
	r.Coverage.InputOmittedFiles = input.OmittedFiles
	r.Diagnostics = append(r.Diagnostics, input.Diagnostics...)
	if input.Status == "skipped" {
		r.Status = "skipped"
		r.Reason = "project_inventory_skipped"
		return r
	}
	if input.Status != "complete" || input.OmittedFiles > 0 || len(input.Diagnostics) > 0 {
		r.Status = "partial"
		r.Reason = "incomplete_project_inventory"
	}
	// Manifest IDs, rather than containing directories, identify graph vertices.
	projects := make([]Project, 0, len(input.Projects))
	for _, p := range input.Projects {
		if p.Kind != "dotnet" {
			r.Coverage.ExcludedProjectKinds[p.Kind]++
			continue
		}
		if !graphLocalPath(p.ID) {
			r.Coverage.ExcludedProjectKinds["invalid_manifest_path"]++
			r.Status = "partial"
			if r.Reason == "" {
				r.Reason = "invalid_project_identity"
			}
			r.Diagnostics = append(r.Diagnostics, Diagnostic{Path: ".", Code: "invalid-graph-project-id", Message: "A project ID is not a normalized path within the inventory."})
			continue
		}
		projects = append(projects, p)
	}
	slices.SortFunc(projects, func(a, b Project) int {
		if c := strings.Compare(a.ID, b.ID); c != 0 {
			return c
		}
		return strings.Compare(a.Root, b.Root)
	})
	indices := map[string]int{}
	for _, p := range projects {
		if i, ok := indices[p.ID]; ok {
			r.Nodes[i].Ambiguous = true
			r.Nodes[i].Evidence = append(r.Nodes[i].Evidence, p.Evidence...)
			r.Status = "partial"
			if r.Reason == "" {
				r.Reason = "ambiguous_project_identity"
			}
			continue
		}
		indices[p.ID] = len(r.Nodes)
		r.Nodes = append(r.Nodes, GraphNode{ID: p.ID, Root: p.Root, Evidence: append([]string{}, p.Evidence...)})
	}
	for i := range r.Nodes {
		n := &r.Nodes[i]
		slices.Sort(n.Evidence)
		n.Evidence = slices.Compact(n.Evidence)
		if n.Ambiguous {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{Path: n.ID, Code: "ambiguous-graph-project-id", Message: "Duplicate project IDs prevent definite reference attribution."})
		}
	}
	r.Coverage.Projects = len(r.Nodes)
	if len(r.Nodes) == 0 && r.Status == "complete" {
		r.Status = "not_applicable"
		r.Reason = "no_dotnet_projects"
	}
	// Preserve conditional and omitted observations without admitting them to the
	// adjacency graph. Identical records are collapsed; distinct evidence remains.
	seen := map[GraphEdge]bool{}
	for _, p := range projects {
		for _, ref := range p.References {
			if ref.Kind != "project-reference" {
				r.Coverage.ExcludedReferenceKinds[ref.Kind]++
				continue
			}
			edge := classifyGraphEdge(p.ID, ref, indices, r.Nodes)
			if seen[edge] {
				r.Coverage.DuplicateObservations++
				continue
			}
			seen[edge] = true
			r.Edges = append(r.Edges, edge)
		}
	}
	for _, cfg := range input.Configurations {
		excluded := false
		for _, ref := range cfg.References {
			r.Coverage.ExcludedReferenceKinds["configuration:"+ref.Kind]++
			if ref.Kind == "project-reference" {
				excluded = true
			}
		}
		if excluded && len(r.Nodes) > 0 {
			r.Status = "partial"
			if r.Reason == "" {
				r.Reason = "unattributed_configuration_references"
			}
			r.Diagnostics = append(r.Diagnostics, Diagnostic{Path: cfg.Path, Code: "unattributed-project-references", Message: "Shared configuration declares ProjectReferences whose source projects require import evaluation."})
		}
	}
	slices.SortFunc(r.Edges, compareGraphEdges)
	r.Coverage.ReferenceObservations = len(r.Edges)
	adj := make([][]int, len(r.Nodes))
	reverse := make([][]int, len(r.Nodes))
	pairs := map[[2]int]bool{}
	for _, e := range r.Edges {
		switch e.Certainty {
		case "conditional":
			r.Coverage.ConditionalEdges++
		case "unresolved":
			r.Coverage.UnresolvedEdges++
		}
		switch e.Resolution {
		case "missing":
			r.Coverage.MissingTargets++
		case "external":
			r.Coverage.ExternalTargets++
		case "unrecognized_target":
			r.Coverage.UnrecognizedTargets++
		case "ambiguous_target":
			r.Coverage.AmbiguousTargets++
		}
		if !e.Included {
			r.Status = "partial"
			if r.Reason == "" {
				r.Reason = "excluded_reference_observations"
			}
			continue
		}
		r.Coverage.IncludedObservations++
		a, b := indices[e.Source], indices[e.Target]
		pair := [2]int{a, b}
		if pairs[pair] {
			continue
		}
		pairs[pair] = true
		adj[a] = append(adj[a], b)
		reverse[b] = append(reverse[b], a)
	}
	r.Coverage.UniqueEdges = len(pairs)
	for i := range adj {
		slices.Sort(adj[i])
		slices.Sort(reverse[i])
		r.Nodes[i].FanOut = len(adj[i])
		r.Nodes[i].FanIn = len(reverse[i])
	}
	graphComponents(r, adj, reverse)
	slices.SortFunc(r.Diagnostics, func(a, b Diagnostic) int {
		return compareFields([]string{a.Path, a.Code, a.Message}, []string{b.Path, b.Code, b.Message})
	})
	return r
}

func graphLocalPath(value string) bool {
	return value != "" && value != "." && path.Clean(value) == value && !strings.HasPrefix(value, "/") && value != ".." && !strings.HasPrefix(value, "../") && !strings.ContainsAny(value, "\\:\x00\r\n")
}

func graphExternal(source, value string) bool {
	normalized := strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
	if dotnetDynamic(normalized) || strings.ContainsAny(normalized, "*?%[]\x00\r\n") {
		return false
	}
	if strings.HasPrefix(normalized, "/") || strings.Contains(normalized, ":") {
		return true
	}
	target := path.Clean(path.Join(path.Dir(source), normalized))
	return target == ".." || strings.HasPrefix(target, "../")
}

func graphExternalTarget(target string) bool {
	// Reference.Target is already inventory-root-relative, unlike its Value.
	if target == "" || dotnetDynamic(target) {
		return false
	}
	target = path.Clean(strings.ReplaceAll(target, `\`, "/"))
	return strings.HasPrefix(target, "/") || strings.Contains(target, ":") || target == ".." || strings.HasPrefix(target, "../")
}

func classifyGraphEdge(source string, ref Reference, indices map[string]int, nodes []GraphNode) GraphEdge {
	e := GraphEdge{Source: source, Value: ref.Value, Evidence: ref.Evidence, Condition: ref.Condition, State: ref.State, TargetStatus: ref.TargetStatus, Certainty: "unresolved", Resolution: "unresolved"}
	if ref.Condition != "" || ref.State == "conditional" {
		e.Certainty = "conditional"
	} else if ref.State == "declared" || ref.State == "resolved" || ref.State == "missing" {
		e.Certainty = "unconditional"
	}
	if ref.State == "missing" && ref.TargetStatus == "present" {
		e.Certainty = "unresolved"
	}
	if ref.State == "unresolved" || dotnetDynamic(ref.Value) {
		e.Certainty = "unresolved"
	}
	if nodes[indices[source]].Ambiguous {
		e.Certainty = "unresolved"
	}
	if graphExternal(source, ref.Value) || graphExternalTarget(ref.Target) {
		e.Resolution = "external"
		return e
	}
	if ref.Target == "" || !graphLocalPath(ref.Target) {
		return e
	}
	e.Target = ref.Target
	switch ref.TargetStatus {
	case "missing":
		e.Resolution = "missing"
	case "present":
		if index, ok := indices[ref.Target]; ok {
			e.Resolution = "in_scope"
			if nodes[index].Ambiguous {
				e.Resolution = "ambiguous_target"
			}
		} else {
			e.Resolution = "unrecognized_target"
		}
	}
	e.Included = e.Certainty == "unconditional" && e.Resolution == "in_scope"
	return e
}

func compareGraphEdges(a, b GraphEdge) int {
	return compareFields([]string{a.Source, a.Target, a.Evidence, a.Value, a.Condition, a.State, a.TargetStatus, a.Certainty, a.Resolution}, []string{b.Source, b.Target, b.Evidence, b.Value, b.Condition, b.State, b.TargetStatus, b.Certainty, b.Resolution})
}

func graphComponents(r *GraphReport, adj, reverse [][]int) {
	n := len(adj)
	visited := make([]bool, n)
	// Weak components include isolated projects and count unique directed edges.
	for start := 0; start < n; start++ {
		if visited[start] {
			continue
		}
		visited[start] = true
		queue := []int{start}
		members := []int{}
		for cursor := 0; cursor < len(queue); cursor++ {
			v := queue[cursor]
			members = append(members, v)
			for _, neighbors := range [][]int{adj[v], reverse[v]} {
				for _, next := range neighbors {
					if !visited[next] {
						visited[next] = true
						queue = append(queue, next)
					}
				}
			}
		}
		slices.Sort(members)
		component := GraphComponent{ID: r.Nodes[members[0]].ID, Nodes: []string{}}
		for _, v := range members {
			component.Nodes = append(component.Nodes, r.Nodes[v].ID)
			component.Edges += len(adj[v])
			r.Nodes[v].Component = component.ID
		}
		r.Components = append(r.Components, component)
	}
	// Kosaraju's two passes use explicit stacks, so deeply nested project chains
	// do not consume one call-stack frame per vertex.
	clear(visited)
	order := make([]int, 0, n)
	type frame struct{ node, next int }
	for start := 0; start < n; start++ {
		if visited[start] {
			continue
		}
		visited[start] = true
		stack := []frame{{node: start}}
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.next < len(adj[top.node]) {
				next := adj[top.node][top.next]
				top.next++
				if !visited[next] {
					visited[next] = true
					stack = append(stack, frame{node: next})
				}
				continue
			}
			order = append(order, top.node)
			stack = stack[:len(stack)-1]
		}
	}
	clear(visited)
	for i := len(order) - 1; i >= 0; i-- {
		start := order[i]
		if visited[start] {
			continue
		}
		visited[start] = true
		stack := []int{start}
		members := []int{}
		for len(stack) > 0 {
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			members = append(members, v)
			for _, next := range reverse[v] {
				if !visited[next] {
					visited[next] = true
					stack = append(stack, next)
				}
			}
		}
		if len(members) == 1 && !slices.Contains(adj[members[0]], members[0]) {
			continue
		}
		slices.Sort(members)
		cycle := GraphCycle{ID: r.Nodes[members[0]].ID, Nodes: []string{}}
		for _, v := range members {
			r.Nodes[v].Cyclic = true
			cycle.Nodes = append(cycle.Nodes, r.Nodes[v].ID)
		}
		r.Cycles = append(r.Cycles, cycle)
	}
	slices.SortFunc(r.Cycles, func(a, b GraphCycle) int { return strings.Compare(a.ID, b.ID) })
}
