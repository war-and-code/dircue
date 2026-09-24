package intentmap

import (
	"bufio"
	"bytes"
	"container/heap"
	"context"
	"go/ast"
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
	projects     map[string]string // projectID → root dir
	projectNames map[string]string // projectID → module/package name base (for binary naming)
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
	return &Detector{maxBytes: maxBytes, maxObs: maxObs, omissions: map[string]int{}, projects: map[string]string{}, projectNames: map[string]string{}}
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
	dockerfile := isDockerfile(file.Path)
	if !proto && !goFile && !config && !dockerfile {
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
	case dockerfile:
		observations = parseDockerfileExpose(file.Path, file.Content)
	case config:
		if strings.HasSuffix(filename, ".json") {
			var depthLimited bool
			observations, depthLimited = parseJSONConfigBounded(file.Path, file.Content)
			if depthLimited {
				d.omit("config_depth_limit")
			}
		} else if strings.HasSuffix(filename, ".yml") || strings.HasSuffix(filename, ".yaml") {
			observations = parseYAMLConfig(file.Path, file.Content)
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
			if id := owningProject(out[i].Path, d.projects); id != "" {
				out[i].ProjectID = id
				out[i].ProjectAttribution = "directory_containment"
			}
		}
		// For Go binaries whose main.go sits at the project root (as opposed
		// to under cmd/<name>/), goBinaryName() returns the directory name,
		// which is often "." → falls back to "main". Replace with the module
		// path base so the name is stable across different checkout paths.
		if out[i].Kind == KindInterface && out[i].Properties["interface_kind"] == "binary" {
			if name, ok := d.projectNames[out[i].ProjectID]; ok && name != "" && name != "." {
				// Only rename when the binary's source file sits at the project root.
				obsDir := strings.Trim(path.Dir(out[i].Path), "./")
				projRoot := strings.Trim(d.projects[out[i].ProjectID], "./")
				if obsDir == projRoot {
					out[i].Name = name
				}
			}
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

// kindPriority returns a sort key that ensures interfaces are retained over
// capabilities, and capabilities over config entries and imports, when the
// observation heap is at capacity. Lower string = higher priority.
func kindPriority(kind Kind) string {
	switch kind {
	case KindInterface:
		return "1"
	case KindCapability:
		return "2"
	case KindConfig:
		return "3"
	case KindImport:
		return "4"
	default:
		return "9"
	}
}

// goBinaryName returns a user-meaningful name for a Go CLI binary from the
// source file path. It uses the immediate parent directory of the file; for
// root-level files (parent ".") it falls back to the file stem.
func goBinaryName(filePath string) string {
	dir := path.Dir(filePath)
	base := path.Base(dir)
	if base == "" || base == "." {
		// root-level binary (e.g. main.go at repo root)
		base = strings.TrimSuffix(path.Base(filePath), ".go")
	}
	return base
}

// isDockerfile reports whether the path looks like a Dockerfile.
func isDockerfile(name string) bool {
	base := strings.ToLower(path.Base(name))
	return base == "dockerfile" || strings.HasPrefix(base, "dockerfile.")
}

// parseDockerfileExpose extracts EXPOSE port declarations from a Dockerfile,
// producing one interface observation per exposed port.
func parseDockerfileExpose(name string, content []byte) []Observation {
	var out []Observation
	scanner := bufio.NewScanner(bytes.NewReader(content))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(strings.ToUpper(line), "EXPOSE") {
			continue
		}
		rest := strings.TrimSpace(line[6:])
		for _, token := range strings.Fields(rest) {
			port := strings.SplitN(token, "/", 2)[0]
			if port == "" || !isNumericPort(port) {
				continue
			}
			out = append(out, Observation{
				Kind:      KindInterface,
				Name:      "port:" + port,
				State:     "declared",
				Basis:     "declared_config",
				Path:      name,
				StartLine: lineNum,
				EndLine:   lineNum,
				Properties: map[string]string{
					"interface_kind": "declared_port",
					"port":           port,
				},
			})
		}
	}
	return out
}

func isNumericPort(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

func parseGoImports(name string, content []byte) []Observation {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, content, 0)
	if err != nil {
		return nil
	}
	var out []Observation
	if f.Name != nil && f.Name.Name == "main" {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != "main" {
				continue
			}
			line := fset.Position(fn.Pos()).Line
			binName := goBinaryName(name)
			out = append(out, Observation{Kind: KindInterface, Name: binName, State: "declared", Basis: "code_syntax", Path: name, StartLine: line, EndLine: line, Properties: map[string]string{"interface_kind": "binary", "package": "main"}})
			break
		}
	}
	for _, spec := range f.Imports {
		value, err := strconv.Unquote(spec.Path.Value)
		if err != nil || value == "" {
			continue
		}
		line := 0
		if spec.Pos().IsValid() {
			line = fset.Position(spec.Pos()).Line
		}
		out = append(out, Observation{Kind: KindImport, Name: value, State: "observed", Basis: "code_syntax", Path: name, StartLine: line, EndLine: line})
		for _, capability := range capabilitiesFor(value) {
			out = append(out, Observation{Kind: KindCapability, Name: capability, State: "observed", Basis: "imported", Path: name, StartLine: line, EndLine: line, Properties: map[string]string{"import": value}})
		}
	}
	return out
}

func compareObservation(a, b Observation) int {
	// kindPriority is the primary key so high-value observation kinds
	// (interfaces first, then capabilities) are retained when the heap fills.
	keysA := []string{kindPriority(a.Kind), string(a.Kind), a.Name, a.ProjectID, a.State, a.Basis, a.Path, strconv.Itoa(a.StartLine), propertiesKey(a.Properties)}
	keysB := []string{kindPriority(b.Kind), string(b.Kind), b.Name, b.ProjectID, b.State, b.Basis, b.Path, strconv.Itoa(b.StartLine), propertiesKey(b.Properties)}
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
