package intentmap

import (
	"container/heap"
	"context"
	"go/parser"
	"go/token"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"

	"dircue/pkg/profile"
)

type Detector struct {
	mu           sync.Mutex
	maxBytes     int64
	maxObs       int
	observations observationHeap
	heapReady    bool
	selected     int
	inspected    int
	omissions    map[string]int
	projects     map[string]string
}

var _ profile.Detector = (*Detector)(nil)

func New(options Options) *Detector {
	maxBytes := options.MaxFileBytes
	if maxBytes <= 0 || maxBytes > DefaultMaxFileBytes {
		maxBytes = DefaultMaxFileBytes
	}
	maxObs := options.MaxObservations
	if maxObs <= 0 || maxObs > DefaultMaxObservations {
		maxObs = DefaultMaxObservations
	}
	return &Detector{maxBytes: maxBytes, maxObs: maxObs, omissions: map[string]int{}, projects: map[string]string{}}
}

func (d *Detector) Name() string { return DetectorName }

// Detect receives only scanner-selected bytes, retains bounded observations,
// and deliberately returns no generic scanner findings.
func (d *Detector) Detect(ctx context.Context, file profile.File) ([]profile.Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	filename := strings.ToLower(file.Path)
	proto := strings.HasSuffix(filename, ".proto")
	goFile := strings.HasSuffix(filename, ".go")
	config := configCandidate(file.Path)
	if !proto && !goFile && !config {
		return nil, nil
	}
	if strings.HasPrefix(filename, "docs/") || strings.HasPrefix(filename, "doc/") || strings.HasPrefix(filename, "examples/") || strings.HasPrefix(filename, "samples/") {
		return nil, nil
	}
	d.mu.Lock()
	d.selected++
	d.mu.Unlock()
	if file.Size != int64(len(file.Content)) || file.Size > d.maxBytes {
		d.omit("file_bytes")
		return nil, nil
	}
	var observations []Observation
	switch {
	case proto:
		observations = parseProto(file.Path, file.Content)
	case goFile:
		observations = parseGoImports(file.Path, file.Content)
	case config:
		if strings.HasSuffix(filename, ".json") {
			var depthLimited bool
			observations, depthLimited = parseJSONConfigBounded(file.Path, file.Content)
			if depthLimited {
				d.omit("config_depth_limit")
			}
		} else {
			observations = parseConfig(file.Path, file.Content)
		}
	}
	d.mu.Lock()
	d.inspected++
	for _, observation := range observations {
		d.addLocked(observation)
	}
	d.mu.Unlock()
	return nil, nil
}

func (d *Detector) Finish(ctx context.Context) (*Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out := slices.Clone([]Observation(d.observations))
	for i := range out {
		out[i].Properties = cloneMap(out[i].Properties)
		if out[i].ProjectID == "" {
			out[i].ProjectID = owningProject(out[i].Path, d.projects)
		}
	}
	slices.SortFunc(out, compareObservation)
	out = slices.CompactFunc(out, equalObservation)
	omissions := cloneIntMap(d.omissions)
	status := "complete"
	if len(omissions) != 0 {
		status = "partial"
	}
	return &Report{Provider: DetectorName, ProviderVersion: DetectorVersion, Coverage: Coverage{Status: status, SelectedFiles: d.selected, InspectedFiles: d.inspected, OmittedFiles: sum(omissions), RetainedObservations: len(out), Omissions: omissions}, Observations: out}, nil
}

func (d *Detector) addLocked(v Observation) {
	if v.Name == "" || v.Path == "" {
		return
	}
	v.Path = strings.TrimPrefix(path.Clean(strings.ReplaceAll(v.Path, "\\", "/")), "./")
	if len(d.observations) < d.maxObs {
		d.observations = append(d.observations, v)
		return
	}
	d.omissions["observation_limit"]++
	if !d.heapReady {
		heap.Init(&d.observations)
		d.heapReady = true
	}
	if compareObservation(v, d.observations[0]) < 0 {
		d.observations[0] = v
		heap.Fix(&d.observations, 0)
	}
}

// The largest retained observation is at index zero, so the same lexical
// subset survives regardless of scanner worker completion order.
type observationHeap []Observation

func (h observationHeap) Len() int           { return len(h) }
func (h observationHeap) Less(i, j int) bool { return compareObservation(h[i], h[j]) > 0 }
func (h observationHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *observationHeap) Push(v any)        { *h = append(*h, v.(Observation)) }
func (h *observationHeap) Pop() any {
	old := *h
	v := old[len(old)-1]
	*h = old[:len(old)-1]
	return v
}

func (d *Detector) omit(reason string) { d.mu.Lock(); d.omissions[reason]++; d.mu.Unlock() }

func parseGoImports(name string, content []byte) []Observation {
	f, err := parser.ParseFile(token.NewFileSet(), name, content, parser.ImportsOnly)
	if err != nil {
		return nil
	}
	var out []Observation
	for _, spec := range f.Imports {
		value, err := strconv.Unquote(spec.Path.Value)
		if err != nil || value == "" {
			continue
		}
		line := 0
		if spec.Pos().IsValid() {
			line = fsetLine(content, int(spec.Pos()-f.Pos()))
		}
		out = append(out, Observation{Kind: KindImport, Name: value, State: "observed", Basis: "code_syntax", Path: name, StartLine: line, EndLine: line})
		for _, capability := range capabilitiesFor(value) {
			out = append(out, Observation{Kind: KindCapability, Name: capability, State: "observed", Basis: "imported", Path: name, StartLine: line, EndLine: line, Properties: map[string]string{"import": value}})
		}
	}
	return out
}

func fsetLine(content []byte, offset int) int {
	if offset < 0 {
		return 0
	}
	if offset > len(content) {
		offset = len(content)
	}
	return 1 + strings.Count(string(content[:offset]), "\n")
}

func compareObservation(a, b Observation) int {
	keysA := []string{string(a.Kind), a.Name, a.ProjectID, a.State, a.Basis, a.Path, strconv.Itoa(a.StartLine), propertiesKey(a.Properties)}
	keysB := []string{string(b.Kind), b.Name, b.ProjectID, b.State, b.Basis, b.Path, strconv.Itoa(b.StartLine), propertiesKey(b.Properties)}
	return strings.Compare(strings.Join(keysA, "\x00"), strings.Join(keysB, "\x00"))
}
func equalObservation(a, b Observation) bool { return compareObservation(a, b) == 0 }
func propertiesKey(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(m[k])
		b.WriteByte(0)
	}
	return b.String()
}
func cloneMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
func cloneIntMap(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
func compact(m map[string]string) map[string]string {
	for k, v := range m {
		if v == "" {
			delete(m, k)
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}
func sum(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}
func declarationState(state string) string {
	if state == "declared" || state == "conditional" {
		return state
	}
	return "partial"
}

func owningProject(filename string, projects map[string]string) string {
	bestID := ""
	bestLength, ambiguous := -1, false
	for id, root := range projects {
		root = strings.Trim(strings.TrimSpace(strings.ReplaceAll(root, "\\", "/")), "/")
		if root == "." {
			root = ""
		}
		if root != "" && filename != root && !strings.HasPrefix(filename, root+"/") {
			continue
		}
		if len(root) > bestLength {
			bestID = id
			bestLength, ambiguous = len(root), false
		} else if len(root) == bestLength && id != bestID {
			ambiguous = true
		}
	}
	if ambiguous {
		return ""
	}
	return bestID
}
