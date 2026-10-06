package lockfiles

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/war-and-code/dircue/internal/xmlencoding"
	"github.com/war-and-code/dircue/pkg/declarations"
)

const nugetCandidatePathLimit = 16

type nugetMSNode struct {
	name      string
	namespace string
	attrs     map[string]string
	text      strings.Builder
	children  []*nugetMSNode
}

func inspectNuGetProjectXML(ctx context.Context, data []byte, manifest string, maxBytes int64) (customPath string, customPathSeen, pathUnknown, importsUnknown, sharedInputsSupported, sharedInputsUnknown bool, ok bool, err error) {
	root, valid := parseNuGetMSBuildFile(ctx, data, maxBytes)
	if ctx.Err() != nil {
		return "", false, true, true, false, true, false, ctx.Err()
	}
	if !valid || root.name != "Project" {
		return "", false, true, true, false, true, false, nil
	}
	sdk, hasSDK := root.attrs["sdk"]
	if hasSDK {
		if isNuGetSupportedSDK(sdk) && len(root.attrs) == 1 {
			sharedInputsSupported = true
		} else {
			sharedInputsUnknown, importsUnknown = true, true
		}
	} else if len(root.attrs) != 0 {
		sharedInputsUnknown, importsUnknown = true, true
	}
	for _, child := range root.children {
		if nugetMSBuildStructuralCaseMismatch(child.name) {
			sharedInputsUnknown, importsUnknown, pathUnknown = true, true, true
			continue
		}
		if strings.EqualFold(child.name, "Sdk") {
			// Child-form SDK declarations can import an arbitrary SDK chain;
			// leave applicability and ordering outside the static subset.
			sharedInputsUnknown, importsUnknown = true, true
		}
		if strings.EqualFold(child.name, "ImportGroup") {
			importsUnknown = true
		}
		if strings.EqualFold(child.name, "Import") {
			if child.attrs["condition"] != "" || child.attrs["project"] == "" || strings.ContainsAny(child.attrs["project"], "$@%*?[]{}") && !strings.Contains(child.attrs["project"], "$(MSBuildThisFileDirectory)") {
				importsUnknown = true
			}
		}
		if !strings.EqualFold(child.name, "PropertyGroup") {
			continue
		}
		groupConditional := child.attrs["condition"] != ""
		for _, prop := range child.children {
			if isNuGetSharedImportControl(prop.name) {
				sharedInputsUnknown = true
				continue
			}
			if !strings.EqualFold(prop.name, "NuGetLockFilePath") {
				continue
			}
			customPathSeen = true
			if groupConditional || prop.attrs["condition"] != "" || len(prop.children) != 0 {
				pathUnknown = true
				continue
			}
			resolved, safe := literalNuGetLockPath(prop.text.String(), manifest)
			if !safe {
				pathUnknown = true
			} else {
				// Unconditional MSBuild assignments are ordered; the last
				// literal value in this project file wins.
				customPath = resolved
			}
		}
	}
	if customPathSeen && customPath == "" {
		pathUnknown = true
	}
	return customPath, customPathSeen, pathUnknown, importsUnknown, sharedInputsSupported, sharedInputsUnknown, true, nil
}

func nugetMSBuildStructuralCaseMismatch(name string) bool {
	for _, structural := range []string{"Project", "PropertyGroup", "ItemGroup", "Import", "ImportGroup", "Target", "UsingTask", "Choose", "When", "Otherwise", "ItemDefinitionGroup", "ProjectExtensions", "Sdk"} {
		if strings.EqualFold(name, structural) && name != structural {
			return true
		}
	}
	return false
}

// These SDK identities were checked against the SDK's default
// Directory.Build.props / Directory.Build.targets import behavior. Other SDK
// chains can add imports or override that ordering, so they remain unresolved.
func isNuGetSupportedSDK(sdk string) bool {
	switch sdk {
	case "Microsoft.NET.Sdk", "Microsoft.NET.Sdk.Web", "Microsoft.NET.Sdk.Razor", "Microsoft.NET.Sdk.Worker":
		return true
	default:
		return false
	}
}

func isNuGetSharedImportControl(name string) bool {
	switch strings.ToLower(name) {
	case "importdirectorybuildprops", "importdirectorybuildtargets", "directorybuildpropspath", "directorybuildtargetspath", "directorypackagespropspath":
		return true
	default:
		return false
	}
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

func nugetCentralVersionOnly(root *nugetMSNode) bool {
	if len(root.attrs) != 0 {
		return false
	}
	for _, group := range root.children {
		if strings.EqualFold(group.name, "PropertyGroup") {
			for _, prop := range group.children {
				name := strings.ToLower(prop.name)
				if name != "managepackageversionscentrally" && name != "centralpackagetransitivepinningenabled" {
					return false
				}
				if len(prop.children) != 0 {
					return false
				}
			}
			continue
		}
		if strings.EqualFold(group.name, "ItemGroup") {
			for _, item := range group.children {
				if !strings.EqualFold(item.name, "PackageVersion") || len(item.children) != 0 {
					return false
				}
				for k := range item.attrs {
					if k != "include" && k != "update" && k != "remove" && k != "version" && k != "condition" && k != "label" {
						return false
					}
				}
			}
			continue
		}
		return false
	}
	return true
}

func literalNuGetLockPath(raw, manifest string) (string, bool) {
	return literalNuGetLockPathForBase(raw, manifest, path.Dir(manifest))
}

func literalNuGetLockPathForBase(raw, manifest, baseDir string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n\x00") {
		return "", false
	}
	projectRoot := path.Dir(manifest)
	projectName := strings.TrimSuffix(path.Base(manifest), path.Ext(manifest))
	anchored := false
	projectDirectory := "$(MSBuildProjectDirectory)"
	thisFileDirectory := "$(MSBuildThisFileDirectory)"
	projectNameToken := "$(MSBuildProjectName)"
	if containsMacroAlias(raw, projectDirectory) || containsMacroAlias(raw, thisFileDirectory) || containsMacroAlias(raw, projectNameToken) {
		return "", false
	}
	for _, token := range []string{projectDirectory, thisFileDirectory} {
		if strings.Contains(raw, token) {
			// Directory-valued reserved properties are only modeled as a
			// leading path anchor. An embedded occurrence would concatenate
			// an absolute MSBuild path into a fictional snapshot-relative path.
			if !strings.HasPrefix(raw, token) || strings.Count(raw, token) != 1 {
				return "", false
			}
			if token == projectDirectory {
				raw = projectRoot + strings.TrimPrefix(raw, token)
			} else {
				base := strings.TrimSuffix(baseDir, "/")
				if base == "." {
					base = ""
				}
				raw = joinSnapshotAnchor(base, strings.TrimPrefix(raw, token))
			}
			anchored = true
		}
	}
	raw = strings.ReplaceAll(raw, projectNameToken, projectName)
	if strings.Count(raw, "$(MSBuildProjectDirectory)")+strings.Count(raw, "$(MSBuildThisFileDirectory)") != 0 {
		return "", false
	}
	if strings.ContainsAny(raw, "$@%*?[]{}") {
		return "", false
	}
	if path.IsAbs(raw) || strings.Contains(raw, ":") || strings.Contains(raw, "\\") {
		return "", false
	}
	resolved := path.Clean(raw)
	if !anchored {
		resolved = path.Clean(path.Join(projectRoot, raw))
	}
	if resolved == ".." || strings.HasPrefix(resolved, "../") || resolved == "." {
		return "", false
	}
	return resolved, true
}

type selectedFileRead struct {
	data   []byte
	reason string
}

type nugetProjectConfig struct {
	customPath            string
	pathUnknown           bool
	importsUnknown        bool
	sharedInputsSupported bool
	sharedInputsUnknown   bool
	unresolved            bool
}

type nugetRecordStatic struct {
	config         nugetProjectConfig
	customPath     string
	pathUnknown    bool
	sharedUnknown  bool
	importsUnknown bool
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

func inspectNuGetProjectConfigStatic(ctx context.Context, in Input, file File, manifest string, limits Limits, inputBytes *int64, cache map[string]selectedFileRead) (nugetProjectConfig, error) {
	config := nugetProjectConfig{}
	selected := map[string]File{manifest: file}
	data, reason, err := readNuGetSelected(ctx, in, selected, manifest, limits, inputBytes, cache)
	if err != nil {
		return config, err
	}
	if reason != "" {
		config.unresolved = true
		return config, nil
	}
	custom, seen, pathUnknown, importsUnknown, sharedInputsSupported, sharedInputsUnknown, ok, err := inspectNuGetProjectXML(ctx, data, manifest, limits.FileBytes)
	if err != nil {
		return config, err
	}
	if !ok {
		config.unresolved = true
		return config, nil
	}
	config.customPath, config.pathUnknown, config.importsUnknown = custom, pathUnknown, importsUnknown
	config.sharedInputsSupported, config.sharedInputsUnknown = sharedInputsSupported, sharedInputsUnknown
	if seen && custom == "" {
		config.pathUnknown = true
	}
	return config, nil
}

func indexNuGetSharedPaths(paths []string) map[string]map[string][]string {
	index := map[string]map[string][]string{}
	for _, p := range paths {
		base := strings.ToLower(path.Base(p))
		if base != "directory.build.props" && base != "directory.build.targets" && base != "directory.packages.props" {
			continue
		}
		dir := path.Dir(p)
		if index[dir] == nil {
			index[dir] = map[string][]string{}
		}
		index[dir][base] = append(index[dir][base], p)
	}
	return index
}

func inspectNuGetSharedInputs(ctx context.Context, in Input, files map[string]File, sharedIndex map[string]map[string][]string, root, manifest string, limits Limits, inputBytes *int64, cache map[string]selectedFileRead) (unknown bool, propsPath, targetsPath string, pathUnknown bool, err error) {
	nearest := map[string]string{}
	for dir := root; ; dir = path.Dir(dir) {
		for _, base := range []string{"directory.build.props", "directory.build.targets", "directory.packages.props"} {
			if nearest[base] != "" {
				continue
			}
			matches := sharedIndex[dir][base]
			if len(matches) > 1 {
				pathUnknown = true
				continue
			}
			if len(matches) == 1 {
				nearest[base] = matches[0]
			}
		}
		if dir == "." || dir == "" {
			break
		}
		parent := path.Dir(dir)
		if parent == dir {
			break
		}
	}
	for _, base := range []string{"directory.build.props", "directory.build.targets", "directory.packages.props"} {
		p := nearest[base]
		if p == "" {
			continue
		}
		unclear, sharedPath, pathUnclear, inspectErr := inspectNuGetSharedPath(ctx, in, files, p, base, manifest, limits, inputBytes, cache, map[string]bool{}, 0)
		if inspectErr != nil {
			return true, "", "", true, inspectErr
		}
		unknown = unknown || unclear
		if base == "directory.packages.props" && sharedPath != "" {
			// Its implicit position relative to the explicit layers is not
			// included in this static precedence subset.
			pathUnknown = true
		}
		pathUnknown = pathUnknown || pathUnclear
		if sharedPath != "" {
			switch base {
			case "directory.build.props":
				propsPath = sharedPath
			case "directory.build.targets":
				targetsPath = sharedPath
			}
		}
	}
	return unknown, propsPath, targetsPath, pathUnknown, nil
}

func inspectNuGetSharedPath(ctx context.Context, in Input, selected map[string]File, filePath, category, manifest string, limits Limits, inputBytes *int64, cache map[string]selectedFileRead, seen map[string]bool, depth int) (unknown bool, customPath string, pathUnknown bool, err error) {
	if depth > 8 || seen[filePath] {
		return true, "", true, nil
	}
	seen[filePath] = true
	data, reason, err := readNuGetSelected(ctx, in, selected, filePath, limits, inputBytes, cache)
	if err != nil {
		return true, "", true, err
	}
	if reason != "" {
		return true, "", true, nil
	}
	root, ok := parseNuGetMSBuildFile(ctx, data, limits.FileBytes)
	if err := ctx.Err(); err != nil {
		return true, "", true, err
	}
	if !ok || root.name != "Project" {
		return true, "", true, nil
	}
	if len(root.attrs) != 0 {
		pathUnknown = true
	}
	var nonImports []*nugetMSNode
	for _, child := range root.children {
		if nugetMSBuildStructuralCaseMismatch(child.name) {
			return true, "", true, nil
		}
		if strings.EqualFold(child.name, "Import") {
			if child.attrs["condition"] != "" {
				unknown, pathUnknown = true, true
				continue
			}
			target, safe := resolveNuGetImportPath(child.attrs["project"], filePath)
			if !safe {
				unknown, pathUnknown = true, true
				continue
			}
			selectedTarget, found := selected[target]
			if !found || selectedTarget.NonRegular || nugetSelectedCaseAlias(selected, target) {
				unknown, pathUnknown = true, true
				continue
			}
			nestedUnknown, nestedPath, nestedPathUnknown, nestedErr := inspectNuGetSharedPath(ctx, in, selected, target, category, manifest, limits, inputBytes, cache, seen, depth+1)
			if nestedErr != nil {
				return true, "", true, nestedErr
			}
			unknown, pathUnknown = unknown || nestedUnknown, pathUnknown || nestedPathUnknown
			if nestedPath != "" {
				customPath = nestedPath
			}
			continue
		}
		nonImports = append(nonImports, child)
		if strings.EqualFold(child.name, "PropertyGroup") {
			for _, prop := range child.children {
				if isNuGetSharedImportControl(prop.name) {
					pathUnknown = true
				}
			}
			groupRoot := &nugetMSNode{name: "Project", children: []*nugetMSNode{child}}
			pathValue, seenPath, unresolved := extractNuGetLockPath(groupRoot, manifest, path.Dir(filePath))
			if unresolved {
				pathUnknown = true
			}
			if seenPath && pathValue != "" && !unresolved {
				customPath = pathValue
			}
		}
	}
	base := *root
	base.children = nonImports
	if inspectNuGetSharedTree(&base, category) {
		unknown = true
	}
	return unknown, customPath, pathUnknown, nil
}

func inspectNuGetSharedTree(root *nugetMSNode, category string) bool {
	if len(root.attrs) != 0 {
		return true
	}
	if strings.EqualFold(category, "directory.packages.props") {
		return !nugetCentralVersionOnly(root)
	}
	for _, group := range root.children {
		switch strings.ToLower(group.name) {
		case "propertygroup":
			for _, prop := range group.children {
				name := strings.ToLower(prop.name)
				if name != "nugetlockfilepath" && name != "managepackageversionscentrally" && name != "centralpackagetransitivepinningenabled" {
					return true
				}
				if len(prop.children) != 0 {
					return true
				}
			}
		case "itemgroup":
			for _, item := range group.children {
				if !strings.EqualFold(item.name, "PackageVersion") || len(item.children) != 0 {
					return true
				}
				for key := range item.attrs {
					if key != "include" && key != "update" && key != "remove" && key != "version" && key != "condition" && key != "label" {
						return true
					}
				}
			}
		default:
			return true
		}
	}
	return false
}

func extractNuGetLockPath(root *nugetMSNode, manifest, baseDir string) (value string, seen, unknown bool) {
	for _, group := range root.children {
		if !strings.EqualFold(group.name, "PropertyGroup") {
			continue
		}
		for _, prop := range group.children {
			if !strings.EqualFold(prop.name, "NuGetLockFilePath") {
				continue
			}
			seen = true
			if group.attrs["condition"] != "" || prop.attrs["condition"] != "" || len(prop.children) != 0 {
				unknown = true
				continue
			}
			resolved, safe := literalNuGetLockPathForBase(prop.text.String(), manifest, baseDir)
			if !safe {
				unknown = true
			} else {
				// Later unconditional assignments override earlier literals.
				value = resolved
			}
		}
	}
	if seen && value == "" {
		unknown = true
	}
	return
}

func inspectNuGetImports(ctx context.Context, in Input, rec declarations.ProjectRecord, selected map[string]File, limits Limits, inputBytes *int64, cache map[string]selectedFileRead) (bool, error) {
	unknown := false
	seen := map[string]bool{}
	var visit func(string, int) (bool, error)
	visit = func(name string, depth int) (bool, error) {
		if depth > 8 || seen[name] {
			return true, nil
		}
		seen[name] = true
		data, reason, err := readNuGetSelected(ctx, in, selected, name, limits, inputBytes, cache)
		if err != nil {
			return true, err
		}
		if reason != "" {
			return true, nil
		}
		root, ok := parseNuGetMSBuildFile(ctx, data, limits.FileBytes)
		if err := ctx.Err(); err != nil {
			return true, err
		}
		if !ok || root.name != "Project" {
			return true, nil
		}
		if len(root.attrs) != 0 {
			return true, nil
		}
		for _, child := range root.children {
			if nugetMSBuildStructuralCaseMismatch(child.name) {
				return true, nil
			}
			switch strings.ToLower(child.name) {
			case "import":
				if child.attrs["condition"] != "" {
					return true, nil
				}
				target, safe := resolveNuGetImportPath(child.attrs["project"], name)
				if !safe {
					return true, nil
				}
				f, found := selected[target]
				if !found || f.NonRegular || nugetSelectedCaseAlias(selected, target) {
					return true, nil
				}
				unclear, err := visit(target, depth+1)
				if err != nil {
					return true, err
				}
				if unclear {
					return true, nil
				}
			case "importgroup", "target", "usingtask":
				return true, nil
			case "itemgroup":
				for _, item := range child.children {
					if !strings.EqualFold(item.name, "PackageVersion") {
						return true, nil
					}
					for key := range item.attrs {
						if key != "include" && key != "update" && key != "remove" && key != "version" && key != "condition" && key != "label" {
							return true, nil
						}
					}
				}
			case "propertygroup":
				for _, prop := range child.children {
					if !strings.EqualFold(prop.name, "ManagePackageVersionsCentrally") && !strings.EqualFold(prop.name, "CentralPackageTransitivePinningEnabled") {
						return true, nil
					}
				}
			default:
				return true, nil
			}
		}
		return false, nil
	}
	for _, ref := range rec.Project.References {
		if !strings.EqualFold(ref.Kind, "import") {
			continue
		}
		if ref.State != "resolved" || ref.TargetStatus != "present" || ref.Target == "" {
			unknown = true
			continue
		}
		unclear, err := visit(ref.Target, 0)
		if err != nil {
			return true, err
		}
		unknown = unknown || unclear
	}
	return unknown, nil
}

func resolveNuGetImportPath(raw, importer string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "@%*?[]{}\\:\r\n\x00") {
		return "", false
	}
	baseDir := path.Dir(importer)
	token := "$(MSBuildThisFileDirectory)"
	if containsMacroAlias(raw, token) {
		return "", false
	}
	anchored := false
	if strings.Contains(raw, token) {
		if !strings.HasPrefix(raw, token) || strings.Count(raw, token) != 1 {
			return "", false
		}
		baseDir = strings.TrimSuffix(baseDir, "/")
		if baseDir == "." {
			baseDir = ""
		}
		raw = joinSnapshotAnchor(baseDir, strings.TrimPrefix(raw, token))
		anchored = true
	}
	if strings.ContainsAny(raw, "$@%") || path.IsAbs(raw) {
		return "", false
	}
	resolved := path.Clean(raw)
	if !anchored {
		resolved = path.Clean(path.Join(baseDir, raw))
	}
	if resolved == ".." || strings.HasPrefix(resolved, "../") || resolved == "." {
		return "", false
	}
	return resolved, true
}

func joinSnapshotAnchor(base, suffix string) string {
	if base == "" {
		return strings.TrimPrefix(suffix, "/")
	}
	return base + "/" + strings.TrimPrefix(suffix, "/")
}

func nugetSelectedCaseAlias(selected map[string]File, target string) bool {
	for candidate := range selected {
		if candidate != target && strings.EqualFold(candidate, target) {
			return true
		}
	}
	return false
}

func containsMacroAlias(raw, exactToken string) bool {
	lowerRaw, lowerToken := strings.ToLower(raw), strings.ToLower(exactToken)
	for offset := 0; ; {
		relative := strings.Index(lowerRaw[offset:], lowerToken)
		if relative < 0 {
			return false
		}
		start := offset + relative
		if raw[start:start+len(exactToken)] != exactToken {
			return true
		}
		offset = start + len(exactToken)
		if offset >= len(raw) {
			return false
		}
	}
}

func makeNuGetEvidence(rec declarations.ProjectRecord, paths []string, files, selected map[string]File, customPath, lockPath, associationState, associationReason string, inventoryComplete bool) *NuGetEvidence {
	e := &NuGetEvidence{PresenceState: "not_observed", CandidatePaths: []string{}, PresenceReasons: []string{}, OwnershipState: associationState, OwnershipReasons: []string{}}
	root := rec.Project.Root
	for _, p := range paths {
		if path.Dir(p) != root || !isNuGetLockCandidateName(path.Base(p)) {
			continue
		}
		f := files[p]
		if !f.NonRegular {
			e.CandidateCount++
			e.CandidatePaths = append(e.CandidatePaths, p)
		} else {
			e.PresenceReasons = append(e.PresenceReasons, "nuget-candidate-not-regular")
		}
	}
	for _, candidatePath := range []string{customPath, lockPath} {
		if candidatePath == "" {
			continue
		}
		f, found := files[candidatePath]
		if !found {
			f, found = selected[candidatePath]
		}
		if found && !f.NonRegular && !slices.Contains(e.CandidatePaths, candidatePath) {
			e.CandidateCount++
			e.CandidatePaths = append(e.CandidatePaths, candidatePath)
		}
		for candidate, candidateFile := range files {
			if candidate != candidatePath && strings.EqualFold(candidate, candidatePath) && !candidateFile.NonRegular && !slices.Contains(e.CandidatePaths, candidate) {
				e.CandidateCount++
				e.CandidatePaths = append(e.CandidatePaths, candidate)
			}
		}
		for candidate, candidateFile := range selected {
			if candidate != candidatePath && strings.EqualFold(candidate, candidatePath) && !candidateFile.NonRegular && !slices.Contains(e.CandidatePaths, candidate) {
				e.CandidateCount++
				e.CandidatePaths = append(e.CandidatePaths, candidate)
			}
		}
	}
	slices.Sort(e.CandidatePaths)
	if e.CandidateCount > 0 {
		e.PresenceState = "observed"
	} else if len(e.PresenceReasons) > 0 || !inventoryComplete {
		e.PresenceState = "unknown"
	}
	if e.CandidateCount > nugetCandidatePathLimit {
		e.OmittedCandidatePaths = e.CandidateCount - nugetCandidatePathLimit
		e.CandidatePaths = e.CandidatePaths[:nugetCandidatePathLimit]
	}
	if associationReason == "nuget-custom-lock-path-unresolved" && e.CandidateCount == 0 {
		e.PresenceState = "unknown"
		e.PresenceReasons = append(e.PresenceReasons, "nuget-custom-lock-path-unresolved")
	}
	if associationReason != "" {
		e.OwnershipReasons = append(e.OwnershipReasons, associationReason)
	}
	return e
}

func associateNuGetCustom(rec declarations.ProjectRecord, records []declarations.ProjectRecord, customPath string, files, selected map[string]File, selectedComplete bool, omissions omissionScope, claims map[string]nugetRecordStatic) association {
	unknown := func(reason string) association { return association{state: "indeterminate", reason: reason} }
	if !selectedComplete || !omissions.nugetCustomOwnersKnown() {
		return unknown("nuget-custom-lock-path-unresolved")
	}
	own := claims[rec.Project.ID]
	if own.pathUnknown || own.config.unresolved || own.config.importsUnknown || own.importsUnknown {
		return unknown("nuget-custom-lock-path-unresolved")
	}
	owners := 1
	for _, other := range records {
		if other.Project.ID == rec.Project.ID || other.Project.Kind != "dotnet" || !isMSBuildProjectRecord(path.Ext(other.Project.ID)) {
			continue
		}
		claim, known := claims[other.Project.ID]
		if !known || claim.pathUnknown || claim.config.unresolved || claim.config.importsUnknown || claim.importsUnknown {
			return unknown("ambiguous-nuget-lockfile-owner")
		}
		if claim.customPath != "" {
			if claim.customPath == customPath {
				owners++
			}
		} else if nugetDefaultLockPathMayEqual(other, customPath) {
			owners++
		}
	}
	if owners != 1 {
		return unknown("ambiguous-nuget-lockfile-owner")
	}
	f, ok := files[customPath]
	if !ok {
		f, ok = selected[customPath]
	}
	if nugetSelectedCaseAlias(files, customPath) || nugetSelectedCaseAlias(selected, customPath) {
		return unknown("nuget-custom-lock-path-unresolved")
	}
	if !ok {
		return association{state: missingState(rec), reason: "lockfile-not-present", reasonPath: customPath}
	}
	if f.NonRegular {
		return unknown("nuget-custom-lock-path-unresolved")
	}
	return association{lockPath: customPath, state: "observed"}
}

func (s omissionScope) nugetCustomOwnersKnown() bool {
	if !s.declarations || !s.attributed || len(s.trees) != 0 {
		return false
	}
	return !slices.ContainsFunc(s.paths, func(p string) bool { return isMSBuildProjectRecord(path.Ext(p)) })
}

func nugetDefaultLockPathMayEqual(rec declarations.ProjectRecord, candidate string) bool {
	root := rec.Project.Root
	if candidate == path.Join(root, "packages.lock.json") {
		return true
	}
	name := strings.TrimSuffix(path.Base(rec.Project.ID), path.Ext(rec.Project.ID))
	return candidate == path.Join(root, "packages."+name+".lock.json")
}

func nugetCustomPathConflictsWithLock(rec declarations.ProjectRecord, lockPath string, records []declarations.ProjectRecord, claims map[string]nugetRecordStatic) bool {
	for _, other := range records {
		if other.Project.ID == rec.Project.ID || other.Project.Kind != "dotnet" || !isMSBuildProjectRecord(path.Ext(other.Project.ID)) {
			continue
		}
		claim, known := claims[other.Project.ID]
		if !known || claim.pathUnknown || claim.config.unresolved || claim.config.importsUnknown || claim.importsUnknown {
			return true
		}
		if claim.customPath != "" && claim.customPath == lockPath {
			return true
		}
	}
	return false
}
