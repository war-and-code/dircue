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

	"github.com/war-and-code/dircue/pkg/profile"
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

// javaCandidate returns true for Java/Kotlin source files and Maven/Gradle
// build files that may contain Spring Boot entry-point declarations.
func javaCandidate(name string) bool {
	lower := strings.ToLower(name)
	base := path.Base(lower)
	if strings.HasSuffix(lower, ".java") || strings.HasSuffix(lower, ".kt") {
		return true
	}
	if base == "pom.xml" {
		return true
	}
	if base == "build.gradle" || base == "build.gradle.kts" {
		return true
	}
	return false
}

// Detect receives only scanner-selected bytes, retains bounded observations,
// and deliberately returns no generic scanner findings.
func (d *Detector) Detect(ctx context.Context, file profile.File) ([]profile.Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	filename := strings.ToLower(file.Path)
	proto := strings.HasSuffix(filename, ".proto")
	goFile := strings.HasSuffix(filename, ".go")
	pythonFile := strings.HasSuffix(filename, ".py")
	config := configCandidate(file.Path)
	dockerfile := isDockerfile(file.Path)
	java := javaCandidate(file.Path)
	compose := isComposeFile(file.Path)
	k8s := !compose && isK8sManifest(file.Path)
	contract := contractCandidate(file.Path)
	if !proto && !goFile && !pythonFile && !config && !dockerfile && !java && !compose && !k8s && !contract {
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
	if contract {
		observations = parseContract(file.Path, file.Content)
	}
	switch {
	case observations != nil:
		// A declared API contract; no other reader applies.
	case proto:
		observations = parseProto(file.Path, file.Content)
	case goFile:
		observations = parseGoImports(file.Path, file.Content)
	case pythonFile:
		observations = parsePythonImports(file.Path, file.Content)
	case compose:
		observations = parseComposeYAMLPorts(file.Path, file.Content)
	case k8s:
		observations = parseK8sContainerPorts(file.Path, file.Content)
	case dockerfile:
		observations = parseDockerfileExpose(file.Path, file.Content)
	case java:
		base := path.Base(strings.ToLower(file.Path))
		switch {
		case strings.HasSuffix(base, ".java") || strings.HasSuffix(base, ".kt"):
			observations = parseJavaSpringBoot(file.Path, file.Content)
		case base == "pom.xml":
			observations = parseMavenMainClass(file.Path, file.Content)
		case base == "build.gradle" || base == "build.gradle.kts":
			observations = parseGradleMainClass(file.Path, file.Content)
		}
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
// It resolves ${VAR} references from ARG/ENV defaults in the same file.
func parseDockerfileExpose(name string, content []byte) []Observation {
	// First pass: collect ARG/ENV default values for variable resolution.
	// Handles single-line (ARG K=v, ENV K=v, ENV K v) and multi-line
	// ENV (ENV K1=v1 \ \n    K2=v2 \) forms.
	varDefaults := map[string]string{}
	allLines := strings.Split(string(content), "\n")
	inEnvContinuation := false
	for _, rawLine := range allLines {
		trimmed := strings.TrimSpace(rawLine)
		upper := strings.ToUpper(trimmed)
		// If we're inside a multi-line ENV block, parse KEY=value pairs.
		if inEnvContinuation {
			// Continuation ends when the line does NOT end with backslash.
			if !strings.HasSuffix(trimmed, "\\") {
				inEnvContinuation = false
			}
			// Strip trailing backslash and parse any KEY=value on this line.
			pair := strings.TrimSuffix(trimmed, "\\")
			pair = strings.TrimSpace(pair)
			if pair != "" {
				if idx := strings.IndexByte(pair, '='); idx >= 0 {
					k := strings.TrimSpace(pair[:idx])
					v := strings.TrimSpace(pair[idx+1:])
					if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
						v = v[1 : len(v)-1]
					}
					varDefaults[k] = v
				}
			}
			continue
		}
		if strings.HasPrefix(upper, "ARG ") || strings.HasPrefix(upper, "ENV ") {
			rest := strings.TrimSpace(trimmed[4:])
			isContinuation := strings.HasSuffix(rest, "\\")
			if isContinuation {
				rest = strings.TrimSpace(strings.TrimSuffix(rest, "\\"))
				inEnvContinuation = true
			}
			// ARG VAR=default  or  ENV VAR=value  or  ENV VAR value
			if idx := strings.IndexByte(rest, '='); idx >= 0 {
				k := strings.TrimSpace(rest[:idx])
				v := strings.TrimSpace(rest[idx+1:])
				// Strip optional surrounding quotes.
				if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
					v = v[1 : len(v)-1]
				}
				varDefaults[k] = v
			} else if strings.HasPrefix(upper, "ENV ") && rest != "" && !isContinuation {
				// ENV VAR value (space-separated, no equals)
				parts := strings.SplitN(rest, " ", 2)
				if len(parts) == 2 {
					varDefaults[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
				}
			}
		}
	}
	inEnvContinuation = false // reset for second pass scanner

	// Second pass: parse EXPOSE instructions.
	var out []Observation
	scanner2 := bufio.NewScanner(bytes.NewReader(content))
	lineNum := 0
	for scanner2.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner2.Text())
		if !strings.HasPrefix(strings.ToUpper(line), "EXPOSE") {
			continue
		}
		rest := strings.TrimSpace(line[6:])
		for _, token := range strings.Fields(rest) {
			port := strings.SplitN(token, "/", 2)[0]
			// Resolve ${VAR} or $VAR references.
			if strings.Contains(port, "$") {
				port = resolveDockerVar(port, varDefaults)
			}
			if port == "" || !isNumericPort(port) {
				// Unresolvable variable — record omission, emit nothing.
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

// resolveDockerVar expands ${VAR} or $VAR in s using the provided defaults map.
// Returns the resolved value, or the original string if unresolvable.
func resolveDockerVar(s string, vars map[string]string) string {
	result := s
	for k, v := range vars {
		result = strings.ReplaceAll(result, "${"+k+"}", v)
		result = strings.ReplaceAll(result, "$"+k, v)
	}
	return result
}

// isComposeFile reports whether the path looks like a Docker Compose file.
func isComposeFile(name string) bool {
	base := strings.ToLower(path.Base(name))
	return base == "docker-compose.yml" || base == "docker-compose.yaml" ||
		base == "compose.yml" || base == "compose.yaml"
}

// isK8sManifest reports whether the path looks like a Kubernetes manifest.
func isK8sManifest(name string) bool {
	lower := strings.ToLower(name)
	base := path.Base(lower)
	if !strings.HasSuffix(base, ".yml") && !strings.HasSuffix(base, ".yaml") {
		return false
	}
	// Check for k8s-related directory names.
	dir := path.Dir(lower)
	for _, seg := range strings.Split(dir, "/") {
		switch seg {
		case "k8s", "kubernetes", "kube", "manifests", "deploy", "deployment", "helm", "charts", "chart", "ksonnet", "jsonnet":
			return true
		}
	}
	// Check for k8s-related base name patterns.
	for _, kw := range []string{"deployment", "statefulset", "daemonset", "pod", "replicaset", "cronjob"} {
		if strings.Contains(base, kw) {
			return true
		}
	}
	return false
}

// parseComposeYAMLPorts extracts declared ports from a Docker Compose YAML file.
// It handles ports: (short and long syntax) and expose: blocks.
func parseComposeYAMLPorts(name string, content []byte) []Observation {
	var out []Observation
	scanner := bufio.NewScanner(bytes.NewReader(content))
	lineNum := 0

	// State machine: track whether we are inside a ports: or expose: block.
	const (
		stateNone   = 0
		statePorts  = 1
		stateExpose = 2
		stateLong   = 3 // inside a long-form port map entry
	)
	state := stateNone
	blockIndent := -1   // indent of the block header
	itemIndent := -1    // indent of the first list item
	longTarget := ""    // accumulated target: value for long-form entries
	longTargetLine := 0 // line number of the target: key
	longPublishedSet := false

	emitPort := func(port string, line int) {
		if port == "" || !isNumericPort(port) {
			return
		}
		out = append(out, Observation{
			Kind:      KindInterface,
			Name:      "port:" + port,
			State:     "declared",
			Basis:     "declared_config",
			Path:      name,
			StartLine: line,
			EndLine:   line,
			Properties: map[string]string{
				"interface_kind": "declared_port",
				"port":           port,
			},
		})
	}

	flushLong := func() {
		if longTarget != "" {
			emitPort(longTarget, longTargetLine)
		}
		longTarget = ""
		longTargetLine = 0
		longPublishedSet = false
	}

	for scanner.Scan() {
		lineNum++
		raw := scanner.Text()
		trimmed := strings.TrimSpace(raw)

		// Calculate indentation depth.
		indent := 0
		for _, c := range raw {
			if c == ' ' {
				indent++
			} else if c == '\t' {
				indent += 2
			} else {
				break
			}
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Detect transition out of current block when indentation drops.
		if state != stateNone && blockIndent >= 0 && indent <= blockIndent {
			if state == stateLong {
				flushLong()
			}
			state = stateNone
			blockIndent = -1
			itemIndent = -1
		}

		// Detect ports: or expose: block headers (at any indent level, but must
		// be an exact key match followed by optional colon and whitespace).
		if state == stateNone {
			if trimmed == "ports:" {
				state = statePorts
				blockIndent = indent
				itemIndent = -1
				continue
			}
			if trimmed == "expose:" {
				state = stateExpose
				blockIndent = indent
				itemIndent = -1
				continue
			}
			continue
		}

		// We are inside a ports: or expose: block.
		// A new key at block indent (no leading dash) ends the block.
		if indent == blockIndent && !strings.HasPrefix(trimmed, "-") {
			if state == stateLong {
				flushLong()
			}
			state = stateNone
			blockIndent = -1
			itemIndent = -1
			// Reprocess this line as a potential block header.
			if trimmed == "ports:" {
				state = statePorts
				blockIndent = indent
				itemIndent = -1
			} else if trimmed == "expose:" {
				state = stateExpose
				blockIndent = indent
				itemIndent = -1
			}
			continue
		}

		if state == stateExpose {
			// expose: only has simple numeric string or integer values.
			item := trimmed
			if strings.HasPrefix(item, "- ") {
				item = strings.TrimSpace(item[2:])
			} else if item == "-" {
				continue
			}
			item = stripYAMLComment(item)
			item = strings.Trim(item, "\"'")
			emitPort(item, lineNum)
			continue
		}

		if state == statePorts || state == stateLong {
			if strings.HasPrefix(trimmed, "-") {
				// New list item — flush any pending long-form entry.
				if state == stateLong {
					flushLong()
				}
				state = statePorts
				if itemIndent < 0 {
					itemIndent = indent
				}
				item := strings.TrimSpace(trimmed[1:])
				if item == "" {
					// Long-form: "- " with nothing after → next lines are key: value
					state = stateLong
					continue
				}
				// Short-form string or integer. Strip inline YAML comments first,
				// then strip surrounding quotes, then extract container port.
				item = stripYAMLComment(item)
				item = strings.Trim(item, "\"'")
				port := extractContainerPort(item)
				emitPort(port, lineNum)
			} else if state == stateLong {
				// We're inside a long-form entry.
				key, val, _ := strings.Cut(trimmed, ":")
				key = strings.TrimSpace(key)
				val = strings.TrimSpace(val)
				val = strings.Trim(val, "\"'")
				switch key {
				case "target":
					longTarget = val
					longTargetLine = lineNum
					_ = longPublishedSet // suppress unused warning
				case "published":
					longPublishedSet = true
				}
			} else if !strings.HasPrefix(trimmed, "-") && strings.Contains(trimmed, ":") {
				// Inline long-form as "target: N" outside a dash — treat as long entry.
				key, val, _ := strings.Cut(trimmed, ":")
				key = strings.TrimSpace(key)
				val = strings.TrimSpace(val)
				val = strings.Trim(val, "\"'")
				if key == "target" {
					longTarget = val
					longTargetLine = lineNum
					state = stateLong
				}
			}
		}
	}
	if state == stateLong {
		flushLong()
	}
	_ = longPublishedSet
	return out
}

// stripYAMLComment removes a trailing inline YAML comment from a value
// string. It handles both unquoted values ("8080 # comment" → "8080") and
// quoted values ("value" # comment → "value"). It is deliberately simple
// and does not implement full YAML lexing.
func stripYAMLComment(s string) string {
	s = strings.TrimSpace(s)
	// If the value starts with a quote, find the matching closing quote and
	// truncate there.
	if len(s) > 0 && (s[0] == '"' || s[0] == '\'') {
		q := s[0]
		end := strings.IndexByte(s[1:], q)
		if end >= 0 {
			return s[:end+2] // include both quotes
		}
		return s // no closing quote found — return as-is
	}
	// Unquoted: strip everything from " #" or "\t#" onward.
	for _, sep := range []string{" #", "\t#"} {
		if idx := strings.Index(s, sep); idx >= 0 {
			s = strings.TrimSpace(s[:idx])
		}
	}
	return s
}

// extractContainerPort extracts the container (right-side) port from a
// Docker Compose short-form port entry such as "8080:80", "127.0.0.1:3000:3000",
// or plain "3000".
func extractContainerPort(entry string) string {
	// Strip protocol suffix (e.g. /tcp, /udp).
	entry = strings.SplitN(entry, "/", 2)[0]
	parts := strings.Split(entry, ":")
	// The container port is always the last component.
	return strings.TrimSpace(parts[len(parts)-1])
}

// parseK8sContainerPorts extracts containerPort declarations from a Kubernetes
// manifest, producing one interface observation per port. When a name: field
// appears adjacent to the containerPort: field (before or after), it is used
// as the port name in the observation name.
func parseK8sContainerPorts(name string, content []byte) []Observation {
	var out []Observation
	lines := strings.Split(string(content), "\n")
	for i, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		// containerPort: may appear as "containerPort: 8080" or "- containerPort: 8080"
		checkLine := trimmed
		if strings.HasPrefix(checkLine, "- ") {
			checkLine = strings.TrimSpace(checkLine[2:])
		}
		if !strings.HasPrefix(checkLine, "containerPort:") {
			continue
		}
		_, val, _ := strings.Cut(checkLine, ":")
		portStr := strings.TrimSpace(val)
		portStr = strings.Trim(portStr, "\"'")
		if !isNumericPort(portStr) {
			continue
		}
		// Look for a name: field adjacent to the containerPort line.
		// Two common k8s port forms:
		//   (a)  - containerPort: 8080        (b)  - name: http
		//          name: http                       containerPort: 8080
		portName := ""
		// Strategy: check forward lines first (common form a), then check the
		// immediately preceding list-marker line for an inline name (form b).
		for j := i + 1; j <= i+3 && j < len(lines); j++ {
			adj := strings.TrimSpace(lines[j])
			if adj == "" || strings.HasPrefix(adj, "#") {
				continue
			}
			adjCheck := adj
			if strings.HasPrefix(adjCheck, "- ") {
				adjCheck = strings.TrimSpace(adjCheck[2:])
			}
			if strings.HasPrefix(adjCheck, "containerPort:") {
				break // another port entry
			}
			// A new list item that isn't a sibling key → we left the entry.
			if strings.HasPrefix(adj, "- ") && !strings.HasPrefix(strings.TrimSpace(adj[2:]), "name:") {
				break
			}
			if strings.HasPrefix(adj, "name:") || strings.HasPrefix(strings.TrimSpace(adj), "name:") {
				rawAdj := adj
				if strings.HasPrefix(rawAdj, "- ") {
					rawAdj = strings.TrimSpace(rawAdj[2:])
				}
				_, nval, _ := strings.Cut(rawAdj, ":")
				portName = strings.TrimSpace(nval)
				portName = strings.Trim(portName, "\"'")
				break
			}
		}
		// Form (b): check the preceding line(s) for "- name: <portname>".
		if portName == "" {
			for j := i - 1; j >= i-3 && j >= 0; j-- {
				adj := strings.TrimSpace(lines[j])
				if adj == "" || strings.HasPrefix(adj, "#") {
					continue
				}
				// Only accept a "- name:" list marker (unambiguously the same entry).
				if strings.HasPrefix(adj, "- ") {
					inner := strings.TrimSpace(adj[2:])
					if strings.HasPrefix(inner, "name:") {
						_, nval, _ := strings.Cut(inner, ":")
						portName = strings.TrimSpace(nval)
						portName = strings.Trim(portName, "\"'")
					}
					break // stop regardless
				}
				// Non-list-marker lines are sibling keys; skip upward.
			}
		}
		// Use port name as interface name when available; otherwise use port number.
		obsName := "port:" + portStr
		if portName != "" {
			obsName = portName
		}
		obs := Observation{
			Kind:      KindInterface,
			Name:      obsName,
			State:     "declared",
			Basis:     "declared_config",
			Path:      name,
			StartLine: i + 1,
			EndLine:   i + 1,
			Properties: map[string]string{
				"interface_kind": "declared_port",
				"port":           portStr,
			},
		}
		if portName != "" {
			obs.Properties["port_name"] = portName
		}
		out = append(out, obs)
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
		for _, capability := range capabilitiesFor("go-import", value) {
			out = append(out, Observation{Kind: KindCapability, Name: capability, State: "observed", Basis: "imported", Path: name, StartLine: line, EndLine: line, Properties: map[string]string{"import": value}})
		}
	}
	return out
}

// parsePythonImports scans Python source files for import statements and infers
// capabilities from the imported top-level package names. It uses a simple
// line-oriented scanner rather than a full AST, which is sufficient for the
// first-level package name and avoids executing any inspected content. Only
// `import X` and `from X import ...` forms are recognised; relative imports
// (from .sibling) and comments are skipped. A capability observation is emitted
// for every catalog match.
func parsePythonImports(name string, content []byte) []Observation {
	var out []Observation
	line := 0
	for _, raw := range strings.Split(string(content), "\n") {
		line++
		// Skip indented lines — only top-level (column-0) imports are parsed.
		// Indented imports are conditional, function-scoped, or try/except guards
		// that are not reliable indicators of a module's direct dependencies.
		if len(raw) > 0 && (raw[0] == ' ' || raw[0] == '\t') {
			continue
		}
		s := strings.TrimSpace(raw)
		// Skip comments and blank lines.
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		var pkg string
		if strings.HasPrefix(s, "import ") {
			// `import foo` or `import foo.bar`
			rest := strings.TrimSpace(s[len("import "):])
			// Handle `import foo, bar` by taking only the first token.
			if i := strings.IndexAny(rest, ", \t"); i > 0 {
				rest = rest[:i]
			}
			pkg = rest
		} else if strings.HasPrefix(s, "from ") {
			// `from foo import bar` or `from foo.bar import baz`
			rest := strings.TrimSpace(s[len("from "):])
			// Relative imports (from .sibling) are not top-level packages.
			if strings.HasPrefix(rest, ".") {
				continue
			}
			if i := strings.Index(rest, " import"); i > 0 {
				pkg = rest[:i]
			} else {
				pkg = rest
			}
		}
		if pkg == "" {
			continue
		}
		// Use only the top-level package name for catalog matching.
		if i := strings.IndexByte(pkg, '.'); i > 0 {
			pkg = pkg[:i]
		}
		for _, capability := range capabilitiesFor("python-import", pkg) {
			out = append(out, Observation{
				Kind:       KindCapability,
				Name:       capability,
				State:      "observed",
				Basis:      "imported",
				Path:       name,
				StartLine:  line,
				EndLine:    line,
				Properties: map[string]string{"import": pkg},
			})
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
