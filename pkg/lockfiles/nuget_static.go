package lockfiles

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/war-and-code/dircue/internal/xmlencoding"
)

// The NuGet static model walks one MSBuild project's selected inputs in
// MSBuild evaluation order and records two facts: the values
// NuGetLockFilePath can hold when restore reads it, and the PackageReference
// identities the evaluation keeps. It never runs MSBuild, expands arbitrary
// properties, or evaluates conditions. Inputs outside that subset are named as
// causes instead of being guessed.
//
// The order follows the SDK's Microsoft.Common.props, NuGet.props,
// Microsoft.Common.CurrentVersion.targets and Microsoft.Common.targets, and was
// checked with SDK 10.0.401 GenerateRestoreGraphFile: Directory.Build.props,
// CustomAfterDirectoryBuildProps, the Microsoft.Common.props custom hooks,
// Directory.Packages.props, the project body, the project's .user file, the
// Microsoft.Common.targets custom hooks, then Directory.Build.targets. Project
// extension files under obj/ are not imported while MSBuild restores.

const (
	nugetCandidatePathLimit = 16
	nugetMaxCauses          = 8
	nugetMaxImportDepth     = 32
	nugetMaxDetailBytes     = 200
)

type nugetMSNode struct {
	name      string
	namespace string
	attrs     map[string]string
	text      strings.Builder
	children  []*nugetMSNode
}

// parseNuGetMSBuildFile reads only XML structure and literal values. It does
// not expand properties, evaluate conditions, or execute imports/targets.
func parseNuGetMSBuildFile(ctx context.Context, data []byte, maxBytes int64) (*nugetMSNode, bool) {
	if int64(len(data)) > maxBytes {
		return nil, false
	}
	decoded, err := xmlencoding.Decode(data)
	if err != nil {
		return nil, false
	}
	d := xml.NewDecoder(strings.NewReader(string(decoded)))
	d.Strict = true
	var stack []*nugetMSNode
	var root *nugetMSNode
	tokens := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, false
		}
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return root, root != nil && len(stack) == 0 && validNuGetMSBuildNamespaceTree(root)
		}
		if err != nil {
			return nil, false
		}
		tokens++
		if tokens > 100_000 || len(stack) > 128 {
			return nil, false
		}
		switch v := tok.(type) {
		case xml.StartElement:
			n := &nugetMSNode{name: v.Name.Local, namespace: v.Name.Space, attrs: map[string]string{}}
			for _, a := range v.Attr {
				if a.Name.Space == "" {
					n.attrs[strings.ToLower(a.Name.Local)] = a.Value
				}
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, false
				}
				root = n
			} else {
				p := stack[len(stack)-1]
				p.children = append(p.children, n)
			}
			stack = append(stack, n)
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text.Write([]byte(v))
			} else if strings.TrimSpace(string(v)) != "" {
				// encoding/xml permits character data outside the document
				// element as tokens; XML does not. Treat trailing junk (or
				// non-whitespace before the root) as an incomplete project.
				return nil, false
			}
		case xml.EndElement:
			if len(stack) == 0 || !strings.EqualFold(stack[len(stack)-1].name, v.Name.Local) {
				return nil, false
			}
			stack = stack[:len(stack)-1]
		}
	}
}

func validNuGetMSBuildNamespaceTree(root *nugetMSNode) bool {
	const msbuildNamespace = "http://schemas.microsoft.com/developer/msbuild/2003"
	if root == nil || root.name != "Project" || (root.namespace != "" && root.namespace != msbuildNamespace) {
		return false
	}
	var visit func(*nugetMSNode) bool
	visit = func(node *nugetMSNode) bool {
		if node.namespace != root.namespace {
			return false
		}
		for _, child := range node.children {
			if !visit(child) {
				return false
			}
		}
		return true
	}
	return visit(root)
}

// MSBuild element names are case-sensitive: a structural element in another
// case is rejected with MSB4067, so the project cannot be evaluated.
func nugetMSBuildStructuralCaseMismatch(name string) bool {
	for _, structural := range []string{"Project", "PropertyGroup", "ItemGroup", "Import", "ImportGroup", "Target", "UsingTask", "Choose", "When", "Otherwise", "ItemDefinitionGroup", "ProjectExtensions", "Sdk"} {
		if strings.EqualFold(name, structural) && name != structural {
			return true
		}
	}
	return false
}

// isNuGetModeledSDK lists project SDKs bundled with the .NET SDK whose props
// and targets import Microsoft.Common.props and Microsoft.Common.targets and
// never assign NuGetLockFilePath (SDK 10.0.401 Sdks directory). Other SDKs
// resolve from NuGet feeds or global.json and are outside the static model.
func isNuGetModeledSDK(sdk string) bool {
	sdk = strings.TrimSpace(sdk)
	name, version, versioned := strings.Cut(sdk, "/")
	if versioned || version != "" {
		return false
	}
	switch strings.ToLower(name) {
	case "microsoft.net.sdk", "microsoft.net.sdk.web", "microsoft.net.sdk.razor", "microsoft.net.sdk.worker",
		"microsoft.net.sdk.blazorwebassembly", "microsoft.net.sdk.windowsdesktop":
		return true
	default:
		return false
	}
}

// nugetGlobalSDKOverride reads only the nearest selected global.json ancestor.
// Its sdk.version field selects the .NET SDK and does not override project
// SDKs; only a matching msbuild-sdks entry changes project-SDK resolution.
func (w *nugetWalker) nugetGlobalSDKOverride(e *nugetEval, manifest string, sdkNames []string) error {
	if len(sdkNames) == 0 {
		return nil
	}
	if w.globalSDKs == nil {
		w.globalSDKs = map[string]nugetGlobalSDKParse{}
	}
	for _, name := range w.ancestorGlobalJSONs(manifest) {
		parsed, exists := w.globalSDKs[name]
		if !exists {
			data, reason, err := readNuGetSelected(w.ctx, w.in, w.index.files, name, w.limits, w.inputBytes, w.reads)
			if err != nil {
				return err
			}
			if reason != "" {
				parsed = nugetGlobalSDKParse{detail: "global.json could not be read within bounded selected inputs (" + reason + ")"}
			} else {
				overrides, ok := parseNuGetMSBuildSDKOverrides(data)
				parsed = nugetGlobalSDKParse{overrides: overrides, valid: ok}
				if !ok {
					parsed.detail = "global.json is not a fully understood JSON SDK configuration"
				}
			}
			w.globalSDKs[name] = parsed
		}
		if !parsed.valid {
			e.addUnmodeled(name, parsed.detail)
			continue
		}
		for _, sdk := range sdkNames {
			if !isNuGetModeledSDK(sdk) {
				continue
			}
			if version, exists := parsed.overrides[strings.ToLower(sdk)]; exists {
				e.addUnmodeled(name, "global.json selects project SDK "+sdk+" version "+version)
			}
		}
	}
	return nil
}

func (w *nugetWalker) ancestorGlobalJSONs(manifest string) []string {
	var files []string
	dir := path.Dir(manifest)
	for {
		name := path.Join(dir, "global.json")
		if _, exists := w.index.files[name]; exists {
			files = append(files, name)
		}
		if dir == "." {
			return files
		}
		dir = path.Dir(dir)
	}
}

func parseNuGetMSBuildSDKOverrides(data []byte) (map[string]string, bool) {
	clean, ok := stripNuGetJSONComments(data)
	if !ok {
		return nil, false
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(clean, &root); err != nil || root == nil {
		return nil, false
	}
	raw, exists := root["msbuild-sdks"]
	if !exists {
		return map[string]string{}, true
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return nil, false
	}
	out := make(map[string]string, len(values))
	for name, encoded := range values {
		var version string
		if err := json.Unmarshal(encoded, &version); err != nil || strings.TrimSpace(name) == "" || strings.TrimSpace(version) == "" {
			return nil, false
		}
		key := strings.ToLower(name)
		if _, duplicate := out[key]; duplicate {
			return nil, false
		}
		out[key] = version
	}
	return out, true
}

// global.json permits JavaScript/C-style comments. Replace comment bytes with
// spaces while preserving line breaks so encoding/json can validate the rest.
func stripNuGetJSONComments(data []byte) ([]byte, bool) {
	out := append([]byte(nil), data...)
	inString, escaped := false, false
	for i := 0; i < len(out); i++ {
		if inString {
			if escaped {
				escaped = false
			} else if out[i] == '\\' {
				escaped = true
			} else if out[i] == '"' {
				inString = false
			}
			continue
		}
		if out[i] == '"' {
			inString = true
			continue
		}
		if out[i] != '/' || i+1 >= len(out) {
			continue
		}
		switch out[i+1] {
		case '/':
			out[i], out[i+1] = ' ', ' '
			i += 2
			for i < len(out) && out[i] != '\n' && out[i] != '\r' {
				out[i] = ' '
				i++
			}
			i--
		case '*':
			out[i], out[i+1] = ' ', ' '
			i += 2
			closed := false
			for i < len(out) {
				if out[i] == '*' && i+1 < len(out) && out[i+1] == '/' {
					out[i], out[i+1] = ' ', ' '
					i++
					closed = true
					break
				}
				if out[i] != '\n' && out[i] != '\r' {
					out[i] = ' '
				}
				i++
			}
			if !closed {
				return nil, false
			}
		}
	}
	return out, true
}

type selectedFileRead struct {
	data   []byte
	reason string
}

func readNuGetSelected(ctx context.Context, in Input, selected map[string]File, name string, limits Limits, inputBytes *int64, cache map[string]selectedFileRead) ([]byte, string, error) {
	if got, ok := cache[name]; ok {
		return got.data, got.reason, nil
	}
	f, ok := selected[name]
	if !ok || f.NonRegular || f.Size < 0 || f.Size > limits.FileBytes || f.Size > limits.InputBytes-*inputBytes || in.ReadSelected == nil {
		cache[name] = selectedFileRead{reason: "nuget-selected-input-unavailable"}
		return nil, "nuget-selected-input-unavailable", nil
	}
	data, size, err := in.ReadSelected(ctx, name, limits.FileBytes+1)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		if in.ErrorPolicy != "continue" {
			return nil, "", fmt.Errorf("read selected NuGet input %q: %w", name, err)
		}
		cache[name] = selectedFileRead{reason: "file-read-error"}
		return nil, "file-read-error", nil
	}
	if size != int64(len(data)) || size != f.Size || size > limits.FileBytes || size > limits.InputBytes-*inputBytes {
		cache[name] = selectedFileRead{reason: "incomplete-selected-input-read"}
		return nil, "incomplete-selected-input-read", nil
	}
	*inputBytes += size
	cache[name] = selectedFileRead{data: data}
	return data, "", nil
}

// nugetFileIndex answers the walker's path questions without rescanning the
// inventory: exact lookups, case-variant lookups, directory listings, and
// prefix ranges for lock path patterns.
type nugetFileIndex struct {
	files       map[string]File
	byLower     map[string][]string
	byDir       map[string][]string
	lowerSorted []string
	complete    bool
}

func newNuGetFileIndex(files map[string]File, complete bool) *nugetFileIndex {
	x := &nugetFileIndex{files: files, byLower: make(map[string][]string, len(files)), byDir: map[string][]string{}, complete: complete}
	sorted := make([]string, 0, len(files))
	for p := range files {
		sorted = append(sorted, p)
	}
	slices.Sort(sorted)
	for _, p := range sorted {
		lower := strings.ToLower(p)
		if len(x.byLower[lower]) == 0 {
			x.lowerSorted = append(x.lowerSorted, lower)
		}
		x.byLower[lower] = append(x.byLower[lower], p)
		x.byDir[path.Dir(p)] = append(x.byDir[path.Dir(p)], p)
	}
	slices.Sort(x.lowerSorted)
	return x
}

func (x *nugetFileIndex) regular(p string) bool {
	f, ok := x.files[p]
	return ok && !f.NonRegular
}

// caseVariants returns regular paths that differ from p only by case.
func (x *nugetFileIndex) caseVariants(p string) []string {
	var out []string
	for _, candidate := range x.byLower[strings.ToLower(p)] {
		if candidate != p && !x.files[candidate].NonRegular {
			out = append(out, candidate)
		}
	}
	return out
}

// matching returns regular paths matching a case-insensitive glob whose only
// metacharacter is '*', which may span directory separators.
func (x *nugetFileIndex) matching(pattern string) []string {
	lowerPattern := strings.ToLower(pattern)
	prefix := lowerPattern
	if i := strings.IndexByte(prefix, '*'); i >= 0 {
		prefix = prefix[:i]
	}
	var out []string
	for i := sort.SearchStrings(x.lowerSorted, prefix); i < len(x.lowerSorted) && strings.HasPrefix(x.lowerSorted[i], prefix); i++ {
		if !nugetGlobMatch(lowerPattern, x.lowerSorted[i]) {
			continue
		}
		for _, p := range x.byLower[x.lowerSorted[i]] {
			if !x.files[p].NonRegular {
				out = append(out, p)
			}
		}
	}
	return out
}

func nugetGlobMatch(pattern, value string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == value
	}
	if !strings.HasPrefix(value, parts[0]) {
		return false
	}
	value = value[len(parts[0]):]
	for _, part := range parts[1 : len(parts)-1] {
		i := strings.Index(value, part)
		if i < 0 {
			return false
		}
		value = value[i+len(part):]
	}
	return strings.HasSuffix(value, parts[len(parts)-1])
}

type nugetLockKind uint8

const (
	nugetLockDefault   nugetLockKind = iota // NuGet's conventional name in the project directory
	nugetLockLiteral                        // one snapshot-relative path
	nugetLockPattern                        // a snapshot-relative glob from an unexpanded expression
	nugetLockOutside                        // a value that resolves outside the snapshot
	nugetLockOpen                           // any path: an unexpanded leading expression or a task output
	nugetLockUnmodeled                      // an SDK or import outside the static model
)

type nugetLockValue struct {
	kind  nugetLockKind
	value string
	cause NuGetCause
}

func (v nugetLockValue) key() string { return fmt.Sprintf("%d\x00%s", v.kind, v.value) }

type nugetIDState uint8

const (
	nugetIDPresent nugetIDState = iota + 1
	nugetIDMaybe
)

type nugetPropValue struct {
	raw, file string
}

// nugetProp tracks a property that changes which files MSBuild imports.
type nugetProp struct {
	values    []nugetPropValue
	unset     bool // a possible assignment left the unassigned state reachable
	dynamic   bool
	dynamicIn string // the file whose assignment made the value dynamic
}

// nugetEval is the static evaluation of one MSBuild project.
type nugetEval struct {
	manifest     string
	lock         []nugetLockValue
	deferredLock []nugetLockValue
	ids          map[string]nugetIDState
	idCause      map[string]NuGetCause
	idsDynamic   []NuGetCause
	unmodeled    []NuGetCause
	invalid      []NuGetCause
	uninspected  bool // the project file could not be read within the bounded inputs
	// projectUnreadable is true when the project file itself could not be read
	// or is not a single well-formed MSBuild document.
	projectUnreadable bool
	props             map[string]*nugetProp
	imported          map[string]bool
	possibleWhy       map[string]string // why a file is only possibly imported
	propsDone         bool
	targetsDone       bool
	depth             int
}

func newNuGetEval(manifest string) *nugetEval {
	return &nugetEval{manifest: manifest, lock: []nugetLockValue{{kind: nugetLockDefault}}, ids: map[string]nugetIDState{}, idCause: map[string]NuGetCause{}, props: map[string]*nugetProp{}, imported: map[string]bool{}, possibleWhy: map[string]string{}}
}

// possibleDetail explains a possible assignment, preferring the reason its
// whole file is only possibly imported.
func (e *nugetEval) possibleDetail(file, fallback string) string {
	if why, ok := e.possibleWhy[file]; ok {
		return nugetDetail(why)
	}
	return nugetDetail(fallback)
}

func nugetDetail(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > nugetMaxDetailBytes {
		cut := nugetMaxDetailBytes
		for cut > 0 && s[cut]&0xC0 == 0x80 {
			cut--
		}
		s = s[:cut] + "..."
	}
	return s
}

func (e *nugetEval) addUnmodeled(file, detail string) {
	e.unmodeled = append(e.unmodeled, NuGetCause{Reason: "nuget-msbuild-input-unmodeled", Path: file, Detail: nugetDetail(detail)})
}

func (e *nugetEval) addInvalid(file, detail string) {
	e.invalid = append(e.invalid, NuGetCause{Reason: "nuget-project-config-unresolved", Path: file, Detail: nugetDetail(detail)})
}

func (e *nugetEval) hasLockKind(kind nugetLockKind) bool {
	return slices.ContainsFunc(e.lock, func(v nugetLockValue) bool { return v.kind == kind })
}

func (e *nugetEval) assignLock(values []nugetLockValue, certain bool) {
	if certain {
		e.lock = slices.Clone(values)
		return
	}
	for _, v := range values {
		if !slices.ContainsFunc(e.lock, func(old nugetLockValue) bool { return old.key() == v.key() }) {
			e.lock = append(e.lock, v)
		}
	}
}

// finalLock returns the possible values in a stable order. An unmodeled input
// may assign NuGetLockFilePath at any later point, so it stays possible.
func (e *nugetEval) finalLock() []nugetLockValue {
	out := slices.Clone(e.lock)
	out = append(out, e.deferredLock...)
	if len(e.unmodeled) > 0 {
		out = append(out, nugetLockValue{kind: nugetLockUnmodeled})
	}
	slices.SortFunc(out, func(a, b nugetLockValue) int { return strings.Compare(a.key(), b.key()) })
	return slices.CompactFunc(out, func(a, b nugetLockValue) bool { return a.key() == b.key() })
}

type nugetWalker struct {
	ctx        context.Context
	in         Input
	index      *nugetFileIndex
	limits     Limits
	inputBytes *int64
	reads      map[string]selectedFileRead
	parsed     map[string]*nugetParsed
	globalSDKs map[string]nugetGlobalSDKParse
}

type nugetGlobalSDKParse struct {
	overrides map[string]string
	detail    string
	valid     bool
}

type nugetParsed struct {
	root   *nugetMSNode
	reason string // read failure; empty with a nil root means invalid MSBuild XML
}

func (w *nugetWalker) parse(name string) (*nugetParsed, error) {
	if got, ok := w.parsed[name]; ok {
		return got, nil
	}
	data, reason, err := readNuGetSelected(w.ctx, w.in, w.index.files, name, w.limits, w.inputBytes, w.reads)
	if err != nil {
		return nil, err
	}
	out := &nugetParsed{reason: reason}
	if reason == "" {
		root, ok := parseNuGetMSBuildFile(w.ctx, data, w.limits.FileBytes)
		if err := w.ctx.Err(); err != nil {
			return nil, err
		}
		if ok {
			out.root = root
		}
	}
	w.parsed[name] = out
	return out, nil
}

var nugetRootAttributes = map[string]bool{"toolsversion": true, "defaulttargets": true, "initialtargets": true, "treataslocalproperty": true, "xmlns": true, "label": true}

// evaluate walks one MSBuild project. uninspected means the project file
// itself could not be read within the bounded inputs.
func (w *nugetWalker) evaluate(manifest string) (*nugetEval, error) {
	e := newNuGetEval(manifest)
	parsed, err := w.parse(manifest)
	if err != nil {
		return e, err
	}
	if parsed.root == nil {
		e.projectUnreadable = true
		detail := "the project XML is not a single well-formed MSBuild Project document"
		if parsed.reason != "" {
			e.uninspected = true
			detail = "the project file could not be read within the bounded inputs (" + parsed.reason + ")"
		}
		e.addInvalid(manifest, detail)
		return e, nil
	}
	root := parsed.root
	e.imported[manifest] = true
	var sdks []string
	for key, value := range root.attrs {
		switch {
		case key == "sdk":
			for _, sdk := range strings.Split(value, ";") {
				if sdk = strings.TrimSpace(sdk); sdk != "" {
					sdks = append(sdks, sdk)
				}
			}
		case !nugetRootAttributes[key]:
			e.addInvalid(manifest, "unrecognized Project attribute "+key)
		}
	}
	for _, child := range root.children {
		if child.name == "Sdk" {
			name := child.attrs["name"]
			if version := child.attrs["version"]; version != "" {
				name += "/" + version
			}
			sdks = append(sdks, name)
		}
	}
	slices.Sort(sdks)
	var modeledSDKs []string
	for _, sdk := range sdks {
		if isNuGetModeledSDK(sdk) {
			modeledSDKs = append(modeledSDKs, sdk)
		} else {
			e.addUnmodeled(manifest, "project SDK "+sdk)
		}
	}
	if err := w.nugetGlobalSDKOverride(e, manifest, modeledSDKs); err != nil {
		return e, err
	}
	if len(sdks) > 0 {
		if err := w.propsPhase(e); err != nil {
			return e, err
		}
	}
	if err := w.topLevel(e, manifest, root.children, false, true); err != nil {
		return e, err
	}
	if len(sdks) > 0 {
		if err := w.targetsPhase(e); err != nil {
			return e, err
		}
	}
	return e, nil
}

func (w *nugetWalker) propsPhase(e *nugetEval) error {
	if e.propsDone {
		return nil
	}
	e.propsDone = true
	if err := w.directoryImport(e, "importdirectorybuildprops", "directorybuildpropspath", "Directory.Build.props", "custombeforedirectorybuildprops", "customafterdirectorybuildprops"); err != nil {
		return err
	}
	for _, hook := range []string{"custombeforemicrosoftcommonprops", "customaftermicrosoftcommonprops"} {
		if err := w.hookImport(e, hook, false); err != nil {
			return err
		}
	}
	return w.directoryImport(e, "importdirectorypackagesprops", "directorypackagespropspath", "Directory.Packages.props", "", "")
}

func (w *nugetWalker) targetsPhase(e *nugetEval) error {
	if e.targetsDone {
		return nil
	}
	// Microsoft.Common.targets imports Microsoft.Common.props when the project
	// did not import it itself.
	if err := w.propsPhase(e); err != nil {
		return err
	}
	e.targetsDone = true
	// Language-target hooks wrap Microsoft.Common.targets; their order relative
	// to the hooks below is not modeled, so their files stay possible.
	for _, hook := range []string{"custombeforemicrosoftcsharptargets", "custombeforemicrosoftvisualbasictargets", "custombeforemicrosoftfsharptargets"} {
		if err := w.hookImport(e, hook, true); err != nil {
			return err
		}
	}
	if user := e.manifest + ".user"; w.index.regular(user) {
		if err := w.walkFile(e, user, false); err != nil {
			return err
		}
	}
	for _, hook := range []string{"custombeforemicrosoftcommontargets", "customaftermicrosoftcommontargets"} {
		if err := w.hookImport(e, hook, false); err != nil {
			return err
		}
	}
	if err := w.directoryImport(e, "importdirectorybuildtargets", "directorybuildtargetspath", "Directory.Build.targets", "custombeforedirectorybuildtargets", "customafterdirectorybuildtargets"); err != nil {
		return err
	}
	for _, hook := range []string{"customaftermicrosoftcsharptargets", "customaftermicrosoftvisualbasictargets", "customaftermicrosoftfsharptargets"} {
		if err := w.hookImport(e, hook, true); err != nil {
			return err
		}
	}
	return nil
}

// importEnabled reports whether an Import* control is definitely and possibly
// true. The SDK defaults an unset control to true and compares it
// case-insensitively with 'true'.
func (e *nugetEval) importEnabled(control string) (definite, possible bool) {
	p := e.props[control]
	if p == nil {
		return true, true
	}
	if p.dynamic {
		return false, true
	}
	definite, possible = !p.unset, p.unset
	for _, v := range p.values {
		if value := strings.TrimSpace(v.raw); value == "" || strings.EqualFold(value, "true") {
			possible = true
		} else {
			definite = false
		}
	}
	return definite && possible, possible
}

func (w *nugetWalker) directoryImport(e *nugetEval, control, pathControl, name, customBefore, customAfter string) error {
	if customBefore != "" {
		if err := w.hookImport(e, customBefore, false); err != nil {
			return err
		}
	}
	if definite, possible := e.importEnabled(control); possible {
		targets, uncertain := w.directoryTargets(e, pathControl, name)
		for _, target := range targets {
			if err := w.walkFile(e, target, !definite || uncertain); err != nil {
				return err
			}
		}
	}
	if customAfter != "" {
		return w.hookImport(e, customAfter, false)
	}
	return nil
}

// directoryTargets resolves Directory.Build.props/targets or
// Directory.Packages.props. MSBuild searches upward from the project
// directory with File.Exists, so a case variant is file-system dependent. The
// snapshot root is the upper limit of the search.
func (w *nugetWalker) directoryTargets(e *nugetEval, pathControl, name string) ([]string, bool) {
	p := e.props[pathControl]
	if p == nil {
		return w.nearest(e, name)
	}
	if p.dynamic {
		e.addUnmodeled(p.dynamicIn, pathControl+" is assigned an unexpanded value")
		return nil, true
	}
	var out []string
	for _, v := range p.values {
		target, kind := w.resolveImportPath(v.raw, v.file, e.manifest, true)
		switch kind {
		case nugetPathResolved:
			out = append(out, target)
		case nugetPathAbsent:
		default:
			e.addUnmodeled(v.file, pathControl+" "+v.raw)
		}
	}
	if p.unset {
		nearest, _ := w.nearest(e, name)
		out = append(out, nearest...)
	}
	return out, p.unset || len(p.values) > 1
}

// nearest finds the Directory file MSBuild's upward search selects. A level
// with only a case variant is file-system dependent: a case-insensitive
// search stops there, a case-sensitive one continues upward. Both results stay
// possible, and possible reports that.
func (w *nugetWalker) nearest(e *nugetEval, name string) (out []string, possible bool) {
	for dir := path.Dir(e.manifest); ; dir = path.Dir(dir) {
		candidate := name
		if dir != "." {
			candidate = dir + "/" + name
		}
		if w.index.regular(candidate) {
			return append(out, candidate), possible
		}
		for _, variant := range w.index.caseVariants(candidate) {
			if w.index.regular(variant) {
				e.possibleWhy[variant] = "only the case variant " + path.Base(variant) + " of " + name + " exists here; MSBuild imports it only on case-insensitive file systems"
				out, possible = append(out, variant), true
			}
		}
		if dir == "." {
			return out, possible
		}
	}
}

// hookImport imports the file named by a Custom* hook property. Its value is
// imported from an SDK file, so only anchored values name a snapshot file;
// the Microsoft.Common hooks are guarded by Exists.
func (w *nugetWalker) hookImport(e *nugetEval, property string, possible bool) error {
	p := e.props[property]
	if p == nil {
		return nil
	}
	if p.dynamic {
		e.addUnmodeled(p.dynamicIn, property+" is assigned an unexpanded value")
		return nil
	}
	uncertain := possible || p.unset || len(p.values) > 1
	for _, v := range p.values {
		if v.raw == "" {
			continue
		}
		target, kind := w.resolveImportPath(v.raw, v.file, e.manifest, true)
		switch kind {
		case nugetPathResolved:
			if err := w.walkFile(e, target, uncertain); err != nil {
				return err
			}
		case nugetPathAbsent:
		default:
			e.addUnmodeled(v.file, property+" "+v.raw)
		}
	}
	return nil
}

func (w *nugetWalker) walkFile(e *nugetEval, name string, possible bool) error {
	// MSBuild skips a file already imported into the same evaluation (MSB4011).
	if e.imported[name] {
		return nil
	}
	e.imported[name] = true
	if e.depth >= nugetMaxImportDepth {
		e.addUnmodeled(name, "import depth limit reached")
		return nil
	}
	parsed, err := w.parse(name)
	if err != nil {
		return err
	}
	if parsed.root == nil {
		if parsed.reason != "" {
			e.addUnmodeled(name, "the imported file could not be read within the bounded inputs ("+parsed.reason+")")
		} else {
			e.addInvalid(name, "the imported file is not a single well-formed MSBuild Project document")
		}
		return nil
	}
	for key, value := range parsed.root.attrs {
		switch {
		case key == "sdk":
			e.addUnmodeled(name, "imported file declares SDK "+value)
		case !nugetRootAttributes[key]:
			e.addInvalid(name, "unrecognized Project attribute "+key)
		}
	}
	e.depth++
	defer func() { e.depth-- }()
	return w.topLevel(e, name, parsed.root.children, possible, false)
}

func (w *nugetWalker) topLevel(e *nugetEval, file string, nodes []*nugetMSNode, possible, isProject bool) error {
	for _, child := range nodes {
		if err := w.ctx.Err(); err != nil {
			return err
		}
		if nugetMSBuildStructuralCaseMismatch(child.name) {
			e.addInvalid(file, "element <"+child.name+"> is not a recognized MSBuild element name")
			continue
		}
		conditional := possible || child.attrs["condition"] != ""
		switch child.name {
		case "PropertyGroup":
			w.properties(e, file, child.children, conditional, false)
		case "ItemGroup":
			w.items(e, file, child.children, conditional)
		case "Choose":
			w.choose(e, file, child)
		case "Target":
			w.target(e, file, child)
		case "Import":
			if err := w.importElement(e, file, child, possible, isProject); err != nil {
				return err
			}
		case "ImportGroup":
			for _, imp := range child.children {
				if imp.name != "Import" {
					e.addInvalid(file, "ImportGroup contains <"+imp.name+">")
					continue
				}
				if err := w.importElement(e, file, imp, conditional, isProject); err != nil {
					return err
				}
			}
		case "Sdk":
			if !isProject {
				e.addUnmodeled(file, "imported file declares SDK "+child.attrs["name"])
			}
		case "ItemDefinitionGroup", "UsingTask", "ProjectExtensions":
		default:
			e.addInvalid(file, "element <"+child.name+"> is not valid beneath <Project>")
		}
	}
	return nil
}

// choose records both branches as possible: exactly one When or Otherwise is
// taken, and conditions are not evaluated.
func (w *nugetWalker) choose(e *nugetEval, file string, node *nugetMSNode) {
	for _, branch := range node.children {
		if branch.name != "When" && branch.name != "Otherwise" {
			e.addInvalid(file, "Choose contains <"+branch.name+">")
			continue
		}
		for _, child := range branch.children {
			switch child.name {
			case "PropertyGroup":
				w.properties(e, file, child.children, true, false)
			case "ItemGroup":
				w.items(e, file, child.children, true)
			case "Choose":
				w.choose(e, file, child)
			default:
				e.addInvalid(file, "<"+branch.name+"> contains <"+child.name+">")
			}
		}
	}
}

// nugetTrackedProperty reports whether a property changes which files MSBuild
// imports, and whether that change is outside the static model.
func nugetTrackedProperty(name string) (tracked, unmodeled bool) {
	switch {
	case name == "importdirectorybuildprops", name == "directorybuildpropspath",
		name == "importdirectorybuildtargets", name == "directorybuildtargetspath",
		name == "importdirectorypackagesprops", name == "directorypackagespropspath":
		return true, false
	case strings.HasPrefix(name, "custombefore"), strings.HasPrefix(name, "customafter"):
		return true, false
	case name == "languagetargets", name == "nugetrestoretargets", name == "nugetpropsfile", name == "nugettargets",
		name == "alternatecommonprops", name == "commontargetspath", name == "msbuildextensionspath",
		name == "msbuildextensionspath32", name == "msbuildextensionspath64", name == "msbuilduserextensionspath":
		return true, true
	default:
		return false, false
	}
}

// nugetUnsetCondition recognizes the condition that assigns a property only
// while it is unset:
//
//	Condition="'$(Name)' == ''"
func nugetUnsetCondition(condition, property string) bool {
	compact := strings.ToLower(strings.Join(strings.Fields(condition), ""))
	return compact == "'$("+strings.ToLower(property)+")'==''"
}

func (w *nugetWalker) properties(e *nugetEval, file string, props []*nugetMSNode, possible, deferred bool) {
	for _, prop := range props {
		name := strings.ToLower(prop.name)
		condition := prop.attrs["condition"]
		certain := !possible && condition == ""
		raw := prop.text.String()
		if name == "nugetlockfilepath" {
			values := nugetLockValues(raw, e.manifest, file)
			if len(prop.children) != 0 {
				values = []nugetLockValue{{kind: nugetLockOpen, cause: NuGetCause{Reason: "nuget-custom-lock-path-unresolved", Path: file, Detail: "NuGetLockFilePath has child elements"}}}
			}
			if !possible && nugetUnsetCondition(condition, prop.name) {
				switch {
				case len(e.lock) == 1 && e.lock[0].kind == nugetLockDefault:
					certain = true
				case !e.hasLockKind(nugetLockDefault):
					continue
				}
			}
			if deferred {
				for i := range values {
					if values[i].cause.Reason == "" {
						values[i].cause = NuGetCause{Reason: "nuget-lock-path-conditional", Path: file, Detail: e.possibleDetail(file, "deferred NuGetLockFilePath assignment in Target")}
					}
				}
				e.deferredLock = append(e.deferredLock, values...)
				continue
			}
			if !certain {
				for i := range values {
					if values[i].cause.Reason == "" {
						values[i].cause = NuGetCause{Reason: "nuget-lock-path-conditional", Path: file, Detail: e.possibleDetail(file, "conditional NuGetLockFilePath assignment")}
					}
				}
			}
			e.assignLock(values, certain)
			continue
		}
		if deferred {
			// Target property groups run after project evaluation. Import-control
			// and hook properties set here cannot change the imports already read.
			continue
		}
		tracked, unmodeled := nugetTrackedProperty(name)
		if !tracked {
			continue
		}
		if unmodeled {
			e.addUnmodeled(file, "property "+prop.name+" redirects SDK imports")
			continue
		}
		p := e.props[name]
		if p == nil {
			p = &nugetProp{}
			e.props[name] = p
		}
		if len(prop.children) != 0 || nugetExpressionDynamic(raw) {
			p.dynamic, p.dynamicIn = true, file
			continue
		}
		value := nugetPropValue{raw: strings.TrimSpace(raw), file: file}
		if certain {
			p.values, p.unset, p.dynamic = []nugetPropValue{value}, false, false
			continue
		}
		if len(p.values) == 0 && !p.dynamic {
			p.unset = true
		}
		p.values = append(p.values, value)
	}
}

// nugetExpressionDynamic reports whether a tracked property value contains an
// expression the static model does not expand.
func nugetExpressionDynamic(raw string) bool {
	rest := strings.TrimSpace(raw)
	lower := strings.ToLower(rest)
	if strings.HasPrefix(lower, "$([msbuild]::getpathoffileabove(") || strings.HasPrefix(lower, "$([msbuild]::getdirectorynameoffileabove(") {
		return false
	}
	for _, anchor := range []string{"$(MSBuildThisFileDirectory)", "$(MSBuildProjectDirectory)", "$(MSBuildProjectName)", "$(MSBuildProjectFile)"} {
		rest = replaceFold(rest, anchor, "")
	}
	return strings.ContainsAny(rest, "$@%")
}

// replaceFold replaces an ASCII MSBuild anchor case-insensitively. It compares
// bytes of s directly: lowercasing s first can change its byte length (for
// example U+0130), and an index into the lowered copy would then slice s at
// the wrong offset.
func replaceFold(s, old, replacement string) string {
	var b strings.Builder
	for {
		i := indexASCIIFold(s, old)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		b.WriteString(replacement)
		s = s[i+len(old):]
	}
}

func indexASCIIFold(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			if asciiLower(s[i+j]) != asciiLower(sub[j]) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func asciiLower(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

// items applies PackageReference and GlobalPackageReference operations in
// evaluation order. NuGet package IDs are case-insensitive. Update only
// changes metadata, so it does not affect which IDs are direct.
func (w *nugetWalker) items(e *nugetEval, file string, items []*nugetMSNode, possible bool) {
	for _, item := range items {
		name := strings.ToLower(item.name)
		if name != "packagereference" && name != "globalpackagereference" {
			continue
		}
		certain := !possible && item.attrs["condition"] == ""
		if include := item.attrs["include"]; strings.TrimSpace(include) != "" {
			excluded := map[string]bool{}
			excludeDynamic := false
			for _, id := range splitMSBuildList(item.attrs["exclude"]) {
				if !safeNuGetPackageID(id) {
					excludeDynamic = true
				}
				excluded[strings.ToLower(id)] = true
			}
			for _, id := range splitMSBuildList(include) {
				if !safeNuGetPackageID(id) {
					e.idsDynamic = append(e.idsDynamic, NuGetCause{Reason: "nuget-package-reference-dynamic", Path: file, Detail: nugetDetail(item.name + " Include " + id)})
					continue
				}
				key := strings.ToLower(id)
				if excluded[key] {
					continue
				}
				switch {
				case certain && !excludeDynamic:
					e.ids[key] = nugetIDPresent
					delete(e.idCause, key)
				case e.ids[key] != nugetIDPresent:
					e.ids[key] = nugetIDMaybe
					e.idCause[key] = NuGetCause{Reason: "nuget-package-reference-conditional", Path: file, Detail: e.possibleDetail(file, "conditional "+item.name+" "+id)}
				}
			}
		}
		for _, id := range splitMSBuildList(item.attrs["remove"]) {
			if !safeNuGetPackageID(id) {
				// A wildcard or item transform can remove any identity.
				for key, state := range e.ids {
					if state == nugetIDPresent {
						e.ids[key] = nugetIDMaybe
						e.idCause[key] = NuGetCause{Reason: "nuget-package-reference-dynamic", Path: file, Detail: nugetDetail(item.name + " Remove " + id)}
					}
				}
				continue
			}
			key := strings.ToLower(id)
			if _, known := e.ids[key]; !known {
				continue
			}
			if certain {
				delete(e.ids, key)
				delete(e.idCause, key)
			} else {
				e.ids[key] = nugetIDMaybe
				e.idCause[key] = NuGetCause{Reason: "nuget-package-reference-conditional", Path: file, Detail: e.possibleDetail(file, "conditional "+item.name+" Remove "+id)}
			}
		}
	}
}

func splitMSBuildList(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ";") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// target records what a target can change while restore runs: its property
// and item groups are possible, and a task that outputs or passes
// NuGetLockFilePath can assign any value.
func (w *nugetWalker) target(e *nugetEval, file string, node *nugetMSNode) {
	name := node.attrs["name"]
	for _, child := range node.children {
		switch child.name {
		case "PropertyGroup":
			w.properties(e, file, child.children, true, true)
		case "ItemGroup":
			w.items(e, file, child.children, true)
		default:
			w.task(e, file, name, child)
		}
	}
}

func (w *nugetWalker) task(e *nugetEval, file, target string, node *nugetMSNode) {
	open := func(detail string) {
		e.assignLock([]nugetLockValue{{kind: nugetLockOpen, cause: NuGetCause{Reason: "nuget-custom-lock-path-unresolved", Path: file, Detail: nugetDetail(detail)}}}, false)
	}
	if node.name == "Output" {
		property, item := strings.TrimSpace(node.attrs["propertyname"]), strings.TrimSpace(node.attrs["itemname"])
		if strings.EqualFold(property, "NuGetLockFilePath") || strings.Contains(property, "$(") {
			open("target " + target + " task output assigns " + property)
		}
		if strings.EqualFold(item, "PackageReference") || strings.Contains(item, "$(") {
			e.idsDynamic = append(e.idsDynamic, NuGetCause{Reason: "nuget-package-reference-dynamic", Path: file, Detail: nugetDetail("target " + target + " task output adds " + item + " items")})
		}
		return
	}
	for key, value := range node.attrs {
		if key != "condition" && nugetAssignsLockPath(value) {
			open("target " + target + " task " + node.name + " passes NuGetLockFilePath")
		}
	}
	for _, child := range node.children {
		w.task(e, file, target, child)
	}
}

// nugetAssignsLockPath recognizes NuGetLockFilePath=value in task parameters
// such as the MSBuild task's Properties, which set a global property for the
// build it starts.
func nugetAssignsLockPath(value string) bool {
	lower := strings.ToLower(value)
	for offset := 0; ; {
		i := strings.Index(lower[offset:], "nugetlockfilepath")
		if i < 0 {
			return false
		}
		rest := strings.TrimLeft(lower[offset+i+len("nugetlockfilepath"):], " \t")
		if strings.HasPrefix(rest, "=") && !strings.HasPrefix(rest, "==") {
			return true
		}
		offset += i + len("nugetlockfilepath")
	}
}

type nugetPathKind uint8

const (
	nugetPathResolved nugetPathKind = iota
	nugetPathAbsent
	nugetPathOutside
	nugetPathDynamic
	nugetPathCaseVariant
	nugetPathPropsMarker
	nugetPathTargetsMarker
)

func (w *nugetWalker) importElement(e *nugetEval, file string, node *nugetMSNode, possible, isProject bool) error {
	conditional := possible || node.attrs["condition"] != ""
	project := strings.TrimSpace(node.attrs["project"])
	if sdk := strings.TrimSpace(node.attrs["sdk"]); sdk != "" {
		modeled := true
		for _, name := range strings.Split(sdk, ";") {
			if strings.TrimSpace(name) != "" && !isNuGetModeledSDK(name) {
				modeled = false
			}
		}
		switch marker := strings.ToLower(project); {
		case modeled && isProject && !conditional && marker == "sdk.props":
			return w.propsPhase(e)
		case modeled && isProject && !conditional && marker == "sdk.targets":
			return w.targetsPhase(e)
		default:
			e.addUnmodeled(file, "Import "+project+" from SDK "+sdk)
			return nil
		}
	}
	if project == "" {
		e.addInvalid(file, "Import has no Project attribute")
		return nil
	}
	if strings.ContainsAny(nugetStripExpressions(project), "*?") {
		for _, target := range w.wildcardTargets(e, project, file) {
			if err := w.walkFile(e, target, true); err != nil {
				return err
			}
		}
		return nil
	}
	target, kind := w.resolveImportPath(project, file, e.manifest, false)
	switch kind {
	case nugetPathPropsMarker:
		return w.propsPhase(e)
	case nugetPathTargetsMarker:
		return w.targetsPhase(e)
	case nugetPathResolved:
		return w.walkFile(e, target, conditional)
	case nugetPathAbsent:
		// A missing conditional import is skipped; a missing unconditional
		// import stops evaluation (MSB4019), so restore cannot run.
		if !conditional {
			e.addInvalid(file, "imported file "+target+" is not in the snapshot")
		}
	case nugetPathCaseVariant:
		e.addUnmodeled(file, "Import "+project+" matches only a case variant; resolution depends on the file system")
	default:
		e.addUnmodeled(file, "Import "+project)
	}
	return nil
}

// nugetStripExpressions removes MSBuild expressions so literal wildcard
// characters can be told apart from expression syntax.
func nugetStripExpressions(raw string) string {
	var b strings.Builder
	depth := 0
	for i := 0; i < len(raw); i++ {
		if depth == 0 && i+1 < len(raw) && strings.ContainsRune("$@%", rune(raw[i])) && raw[i+1] == '(' {
			depth = 1
			i++
			continue
		}
		if depth > 0 {
			switch raw[i] {
			case '(':
				depth++
			case ')':
				depth--
			}
			continue
		}
		b.WriteByte(raw[i])
	}
	return b.String()
}

// wildcardTargets resolves an Import whose final segment has * or ? against
// the snapshot directory it names. Matches stay possible because MSBuild's
// wildcard import order is not modeled.
func (w *nugetWalker) wildcardTargets(e *nugetEval, project, importer string) []string {
	normalized := strings.ReplaceAll(project, `\`, "/")
	dir, pattern := path.Split(normalized)
	if strings.ContainsAny(nugetStripExpressions(dir), "*?") || strings.ContainsAny(pattern, "$@%") {
		e.addUnmodeled(importer, "wildcard Import "+project)
		return nil
	}
	base := path.Dir(importer)
	if dir != "" {
		resolved, anchored, status := resolveMSBuildAnchors(strings.TrimSuffix(dir, "/"), importer, e.manifest)
		if status != anchorOK || !anchored && (path.IsAbs(resolved) || strings.Contains(resolved, ":")) {
			e.addUnmodeled(importer, "wildcard Import "+project)
			return nil
		}
		if !anchored {
			resolved = path.Join(base, resolved)
		}
		base = path.Clean(resolved)
		if base == ".." || strings.HasPrefix(base, "../") {
			e.addUnmodeled(importer, "wildcard Import "+project)
			return nil
		}
	}
	// MSBuild wildcards are only * and ?; brackets are literal characters.
	glob := strings.NewReplacer("[", `\[`, "]", `\]`).Replace(strings.ToLower(pattern))
	var out []string
	for _, candidate := range w.index.byDir[base] {
		if matched, err := path.Match(glob, strings.ToLower(path.Base(candidate))); err == nil && matched && w.index.regular(candidate) {
			out = append(out, candidate)
		}
	}
	return out
}

// resolveImportPath resolves an Import Project value or an import-path
// property. MSBuild normalizes backslashes in import paths on every platform
// (SDK 10.0.401 check). A relative path is relative to the importing file; an
// import-path property is imported from an SDK file, so only anchored values
// resolve there.
func (w *nugetWalker) resolveImportPath(raw, importer, manifest string, anchoredOnly bool) (string, nugetPathKind) {
	raw = strings.ReplaceAll(strings.TrimSpace(raw), `\`, "/")
	if raw == "" || strings.ContainsAny(raw, "\r\n\x00") {
		return "", nugetPathDynamic
	}
	switch strings.ToLower(raw) {
	case "$(msbuildextensionspath)/$(msbuildtoolsversion)/microsoft.common.props":
		return "", nugetPathPropsMarker
	case "$(msbuildtoolspath)/microsoft.csharp.targets", "$(msbuildbinpath)/microsoft.csharp.targets",
		"$(msbuildtoolspath)/microsoft.visualbasic.targets", "$(msbuildbinpath)/microsoft.visualbasic.targets",
		"$(msbuildtoolspath)/microsoft.common.targets", "$(msbuildbinpath)/microsoft.common.targets":
		return "", nugetPathTargetsMarker
	}
	if target, kind, ok := w.resolveFileAbove(raw, importer, manifest); ok {
		return target, kind
	}
	resolved, anchored, status := resolveMSBuildAnchors(raw, importer, manifest)
	switch {
	case status == anchorOutside:
		return "", nugetPathOutside
	case status != anchorOK, !anchored && anchoredOnly:
		return "", nugetPathDynamic
	}
	if !anchored {
		if path.IsAbs(resolved) || strings.Contains(resolved, ":") {
			return "", nugetPathOutside
		}
		resolved = path.Join(path.Dir(importer), resolved)
	}
	resolved = path.Clean(resolved)
	if resolved == "." || resolved == ".." || strings.HasPrefix(resolved, "../") || path.IsAbs(resolved) {
		return "", nugetPathOutside
	}
	return w.classify(resolved)
}

func (w *nugetWalker) classify(resolved string) (string, nugetPathKind) {
	if w.index.regular(resolved) {
		return resolved, nugetPathResolved
	}
	if len(w.index.caseVariants(resolved)) > 0 {
		return resolved, nugetPathCaseVariant
	}
	if f, exists := w.index.files[resolved]; exists && f.NonRegular || !w.index.complete {
		return resolved, nugetPathDynamic
	}
	return resolved, nugetPathAbsent
}

type anchorStatus uint8

const (
	anchorOK anchorStatus = iota
	anchorDynamic
	anchorOutside
)

// resolveMSBuildAnchors expands the reserved properties the static model
// supports. MSBuild property names are case-insensitive.
// $(MSBuildThisFileDirectory) ends with a separator; $(MSBuildProjectDirectory)
// does not, so a following file name concatenates onto the directory name and
// names a sibling path (SDK 10.0.401 check). Any other expression is dynamic.
func resolveMSBuildAnchors(raw, definedIn, manifest string) (string, bool, anchorStatus) {
	projectDir := path.Dir(manifest)
	raw = replaceFold(raw, "$(MSBuildProjectName)", strings.TrimSuffix(path.Base(manifest), path.Ext(manifest)))
	raw = replaceFold(raw, "$(MSBuildProjectFile)", path.Base(manifest))
	anchored := false
	switch lower := strings.ToLower(raw); {
	case strings.HasPrefix(lower, "$(msbuildthisfiledirectory)"):
		raw = joinSnapshotAnchor(strings.TrimPrefix(path.Dir(definedIn), "."), raw[len("$(MSBuildThisFileDirectory)"):])
		anchored = true
	case strings.HasPrefix(lower, "$(msbuildprojectdirectory)"):
		rest := raw[len("$(MSBuildProjectDirectory)"):]
		switch {
		case strings.HasPrefix(rest, "/"):
			raw = joinSnapshotAnchor(strings.TrimPrefix(projectDir, "."), rest)
		case projectDir == ".":
			return "", true, anchorOutside
		default:
			raw = projectDir + rest
		}
		anchored = true
	}
	if strings.ContainsAny(raw, "$@%") {
		return "", anchored, anchorDynamic
	}
	return raw, anchored, anchorOK
}

func joinSnapshotAnchor(base, suffix string) string {
	base = strings.TrimSuffix(base, "/")
	if base == "" {
		return strings.TrimPrefix(suffix, "/")
	}
	return base + "/" + strings.TrimPrefix(suffix, "/")
}

// resolveFileAbove models $([MSBuild]::GetPathOfFileAbove(name, start)) and
// $([MSBuild]::GetDirectoryNameOfFileAbove(start, name))/name. The search
// stops at the snapshot root, matching the Directory.Build.* search limit.
func (w *nugetWalker) resolveFileAbove(raw, importer, manifest string) (string, nugetPathKind, bool) {
	lower := strings.ToLower(raw)
	var name, start string
	switch {
	case strings.HasPrefix(lower, "$([msbuild]::getpathoffileabove(") && strings.HasSuffix(raw, "))"):
		args := splitMSBuildArgs(raw[len("$([MSBuild]::GetPathOfFileAbove(") : len(raw)-2])
		if len(args) < 1 || len(args) > 2 {
			return "", nugetPathDynamic, true
		}
		name, start = args[0], "$(MSBuildThisFileDirectory)"
		if len(args) == 2 {
			start = args[1]
		}
	case strings.HasPrefix(lower, "$([msbuild]::getdirectorynameoffileabove("):
		closing := strings.Index(raw, "))")
		if closing < 0 {
			return "", nugetPathDynamic, true
		}
		args := splitMSBuildArgs(raw[len("$([MSBuild]::GetDirectoryNameOfFileAbove("):closing])
		if len(args) != 2 || !strings.EqualFold(strings.TrimPrefix(raw[closing+2:], "/"), args[1]) {
			return "", nugetPathDynamic, true
		}
		start, name = args[0], args[1]
	default:
		return "", 0, false
	}
	if name == "" || strings.ContainsAny(name, "$@%/*?") {
		return "", nugetPathDynamic, true
	}
	dir, anchored, status := resolveMSBuildAnchors(start, importer, manifest)
	if status != anchorOK || !anchored {
		return "", nugetPathDynamic, true
	}
	if dir = path.Clean(dir); dir == "" {
		dir = "."
	}
	if dir == ".." || strings.HasPrefix(dir, "../") {
		return "", nugetPathOutside, true
	}
	for {
		candidate := name
		if dir != "." {
			candidate = dir + "/" + name
		}
		if w.index.regular(candidate) {
			return candidate, nugetPathResolved, true
		}
		if len(w.index.caseVariants(candidate)) > 0 {
			return candidate, nugetPathCaseVariant, true
		}
		if dir == "." {
			// Nothing within the snapshot: the documented search limit.
			return candidate, nugetPathAbsent, true
		}
		dir = path.Dir(dir)
	}
}

func splitMSBuildArgs(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		out = append(out, strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(part, "'"), "'")))
	}
	return out
}

// nugetLockValues resolves one NuGetLockFilePath assignment. NuGet combines a
// relative value with the project directory. A backslash is a directory
// separator only on Windows; on Unix restore writes a file whose name
// contains it (SDK 10.0.401 restore check), so both spellings stay possible.
func nugetLockValues(raw, manifest, definedIn string) []nugetLockValue {
	appendPossible := func(out []nugetLockValue, value nugetLockValue) []nugetLockValue {
		out = append(out, value)
		if value.kind == nugetLockPattern {
			// An unexpanded property can contain separators or an absolute
			// path, so the visible glob is only a subset of its possible values.
			// Keep it for candidate discovery, but never use it to prove that no
			// lockfile exists outside the glob.
			out = append(out, nugetLockValue{kind: nugetLockOpen, cause: value.cause})
		}
		return out
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []nugetLockValue{{kind: nugetLockDefault}}
	}
	if strings.ContainsAny(raw, "\r\n\x00") {
		return []nugetLockValue{{kind: nugetLockOpen, cause: NuGetCause{Reason: "nuget-custom-lock-path-unresolved", Path: definedIn, Detail: "NuGetLockFilePath contains a control character"}}}
	}
	if !strings.Contains(raw, `\`) {
		return appendPossible(nil, nugetLockValueFor(raw, manifest, definedIn))
	}
	platform := NuGetCause{Reason: "nuget-lock-path-conditional", Path: definedIn, Detail: "NuGetLockFilePath uses a backslash, which is a directory separator only on Windows"}
	windows := nugetLockValueFor(strings.ReplaceAll(raw, `\`, "/"), manifest, definedIn)
	unix := nugetLockValueFor(raw, manifest, definedIn)
	for _, v := range []*nugetLockValue{&windows, &unix} {
		if v.cause.Reason == "" {
			v.cause = platform
		}
	}
	if windows.key() == unix.key() {
		return appendPossible(nil, windows)
	}
	out := appendPossible(nil, windows)
	return appendPossible(out, unix)
}

func nugetLockValueFor(raw, manifest, definedIn string) nugetLockValue {
	unresolved := func(kind nugetLockKind, detail string) nugetLockValue {
		return nugetLockValue{kind: kind, cause: NuGetCause{Reason: "nuget-custom-lock-path-unresolved", Path: definedIn, Detail: nugetDetail(detail + ": " + raw)}}
	}
	expanded, anchored, status := resolveMSBuildAnchors(raw, definedIn, manifest)
	switch status {
	case anchorOutside:
		return unresolved(nugetLockOutside, "NuGetLockFilePath names a path outside the snapshot")
	case anchorDynamic:
		// Remaining expressions become wildcards. A leading expression can
		// expand to an absolute path, so it constrains nothing.
		if pattern := nugetPatternFromExpressions(raw, definedIn, manifest); pattern != "" {
			return nugetLockValue{kind: nugetLockPattern, value: pattern, cause: NuGetCause{Reason: "nuget-custom-lock-path-unresolved", Path: definedIn, Detail: nugetDetail("NuGetLockFilePath uses an unexpanded expression: " + raw)}}
		}
		return unresolved(nugetLockOpen, "NuGetLockFilePath uses an unexpanded expression")
	}
	if !anchored {
		if path.IsAbs(expanded) || len(expanded) > 1 && expanded[1] == ':' {
			return unresolved(nugetLockOutside, "NuGetLockFilePath is an absolute path")
		}
		expanded = path.Join(path.Dir(manifest), expanded)
	}
	resolved := path.Clean(expanded)
	if resolved == "." || resolved == ".." || strings.HasPrefix(resolved, "../") || path.IsAbs(resolved) {
		return unresolved(nugetLockOutside, "NuGetLockFilePath names a path outside the snapshot")
	}
	return nugetLockValue{kind: nugetLockLiteral, value: resolved}
}

// nugetPatternFromExpressions turns a value with unexpanded expressions into a
// snapshot-relative glob, or "" when the value can name any path.
func nugetPatternFromExpressions(raw, definedIn, manifest string) string {
	projectDir := strings.TrimPrefix(path.Dir(manifest), ".")
	raw = strings.ReplaceAll(raw, `\`, "/")
	raw = replaceFold(raw, "$(MSBuildProjectName)", strings.TrimSuffix(path.Base(manifest), path.Ext(manifest)))
	lower := strings.ToLower(raw)
	prefix := projectDir
	switch {
	case strings.HasPrefix(lower, "$(msbuildthisfiledirectory)"):
		prefix, raw = strings.TrimPrefix(path.Dir(definedIn), "."), raw[len("$(MSBuildThisFileDirectory)"):]
	case strings.HasPrefix(lower, "$(msbuildprojectdirectory)/"):
		raw = raw[len("$(MSBuildProjectDirectory)"):]
	case strings.HasPrefix(raw, "$"), strings.HasPrefix(raw, "@"), strings.HasPrefix(raw, "%"):
		return ""
	}
	var b strings.Builder
	depth := 0
	for i := 0; i < len(raw); i++ {
		if depth == 0 && i+1 < len(raw) && strings.ContainsRune("$@%", rune(raw[i])) && raw[i+1] == '(' {
			depth = 1
			i++
			b.WriteByte('*')
			continue
		}
		if depth > 0 {
			switch raw[i] {
			case '(':
				depth++
			case ')':
				depth--
			}
			continue
		}
		b.WriteByte(raw[i])
	}
	if depth != 0 {
		return ""
	}
	pattern := path.Clean(joinSnapshotAnchor(prefix, b.String()))
	if pattern == "." || pattern == ".." || strings.HasPrefix(pattern, "../") || path.IsAbs(pattern) || strings.HasPrefix(pattern, "*") || strings.Contains(pattern, "*/..") {
		return ""
	}
	return pattern
}

// nugetDefaultName returns NuGet's project-specific lock file name. NuGet
// replaces spaces in the project name with underscores (SDK 10.0.401 restore
// check).
func nugetDefaultName(manifest string) string {
	name := strings.TrimSuffix(path.Base(manifest), path.Ext(manifest))
	return "packages." + strings.ReplaceAll(name, " ", "_") + ".lock.json"
}

// nugetClaims indexes every MSBuild project's possible lock paths so one
// project's ownership can be checked against the others without rescanning.
type nugetClaims struct {
	index    *nugetFileIndex
	projects map[string][]string // directory to every evaluated MSBuild project
	literal  map[string][]string // lower-cased path to manifests
	byDir    map[string][]string // directory to manifests whose value may be the default
	pattern  []nugetPatternClaim
	open     []string // manifests that may write any path
}

type nugetPatternClaim struct {
	pattern, manifest string
}

func newNuGetClaims(evals map[string]*nugetEval, index *nugetFileIndex) *nugetClaims {
	c := &nugetClaims{index: index, projects: map[string][]string{}, literal: map[string][]string{}, byDir: map[string][]string{}}
	manifests := make([]string, 0, len(evals))
	for m := range evals {
		manifests = append(manifests, m)
	}
	slices.Sort(manifests)
	for _, m := range manifests {
		e := evals[m]
		c.projects[path.Dir(m)] = append(c.projects[path.Dir(m)], m)
		if e.uninspected || e.projectUnreadable {
			// A project file that could not be read or parsed may still be
			// one MSBuild accepts, so it may name any path.
			c.open = append(c.open, m)
			continue
		}
		if len(e.invalid) > 0 {
			// The project parsed but names a missing import or an invalid
			// element, which MSBuild rejects, so its restore writes no lock.
			continue
		}
		for _, v := range e.finalLock() {
			switch v.kind {
			case nugetLockDefault:
				c.byDir[path.Dir(m)] = append(c.byDir[path.Dir(m)], m)
			case nugetLockLiteral:
				key := strings.ToLower(v.value)
				c.literal[key] = append(c.literal[key], m)
			case nugetLockPattern:
				c.pattern = append(c.pattern, nugetPatternClaim{pattern: strings.ToLower(v.value), manifest: m})
			case nugetLockOpen:
				c.open = append(c.open, m)
			}
		}
	}
	for key := range c.literal {
		c.literal[key] = slices.Compact(c.literal[key])
	}
	c.open = slices.Compact(c.open)
	return c
}

// others returns the other projects whose possible lock paths include
// lockPath. A project whose only uncertainty is an unmodeled SDK or import is
// not a claimant: unmodeled inputs qualify only the project that uses them.
func (c *nugetClaims) others(self, lockPath string) []string {
	seen := map[string]bool{}
	add := func(m string) {
		if m != self {
			seen[m] = true
		}
	}
	for _, m := range c.literal[strings.ToLower(lockPath)] {
		add(m)
	}
	dir, base := path.Dir(lockPath), path.Base(lockPath)
	for _, m := range c.byDir[dir] {
		specific := path.Join(dir, nugetDefaultName(m))
		switch {
		case strings.EqualFold(base, nugetDefaultName(m)):
			add(m)
		case strings.EqualFold(base, "packages.lock.json") && !c.index.regular(specific) && len(c.index.caseVariants(specific)) == 0:
			// NuGet prefers an existing project-specific file over
			// packages.lock.json.
			add(m)
		}
	}
	lower := strings.ToLower(lockPath)
	for _, p := range c.pattern {
		if nugetGlobMatch(p.pattern, lower) {
			add(p.manifest)
		}
	}
	for _, m := range c.open {
		add(m)
	}
	out := make([]string, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	slices.Sort(out)
	return out
}

func (s omissionScope) nugetCustomOwnersKnown() bool {
	if !s.declarations || !s.attributed || len(s.trees) != 0 {
		return false
	}
	return !slices.ContainsFunc(s.paths, func(p string) bool { return isMSBuildProjectRecord(path.Ext(p)) })
}

// packageCauses returns the reasons the effective PackageReference identity
// set is incomplete.
func (e *nugetEval) packageCauses() []NuGetCause {
	var out []NuGetCause
	for id, state := range e.ids {
		if state == nugetIDMaybe {
			out = append(out, e.idCause[id])
		}
	}
	out = append(out, e.idsDynamic...)
	out = append(out, e.unmodeled...)
	out = append(out, e.invalid...)
	return sortNuGetCauses(out)
}

// presentIDs returns the identities every evaluation keeps.
func (e *nugetEval) presentIDs() []string {
	var out []string
	for id, state := range e.ids {
		if state == nugetIDPresent {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

func (e *nugetEval) mayHavePackages() bool {
	return len(e.ids) > 0 || len(e.idsDynamic) > 0 || len(e.unmodeled) > 0 || len(e.invalid) > 0
}

func nugetCauseKey(c NuGetCause) string {
	return c.Reason + "\x00" + c.Path + "\x00" + c.Detail
}

func sortNuGetCauses(causes []NuGetCause) []NuGetCause {
	out := slices.Clone(causes)
	slices.SortFunc(out, func(a, b NuGetCause) int {
		return strings.Compare(nugetCauseKey(a), nugetCauseKey(b))
	})
	return slices.CompactFunc(out, func(a, b NuGetCause) bool { return a == b })
}
