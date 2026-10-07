package projects

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
)

// IsDotnet recognizes project, solution, and conventional .NET configuration files.
func IsDotnet(name string) bool {
	base := strings.ToLower(path.Base(strings.ReplaceAll(name, `\`, "/")))
	switch path.Ext(base) {
	case ".csproj", ".fsproj", ".vbproj", ".vcxproj", ".sqlproj", ".wixproj", ".shproj", ".proj", ".sln", ".slnx", ".slnf":
		return true
	}
	switch base {
	case "global.json", "directory.build.props", "directory.build.targets", "directory.packages.props", "nuget.config", "packages.config":
		return true
	}
	return false
}

const dotnetMaxBytes = 8 << 20
const dotnetMaxNodes = 100000
const dotnetMaxDepth = 128
const dotnetMaxConditionBytes = 64 << 10
const dotnetMaxExpandedBytes = 4 << 20
const dotnetMaxObservations = 8192

// Input size alone does not bound repeated inherited conditions. Both
// intermediate expressions and retained observation text count against the budget.
type dotnetBudget struct {
	remaining    int
	observations int
	exceeded     bool
}

func (b *dotnetBudget) charge(bytes int) bool {
	if b.exceeded || bytes > b.remaining {
		b.exceeded = true
		return false
	}
	b.remaining -= bytes
	return true
}
func (b *dotnetBudget) observation(bytes int) bool {
	if b.observations >= dotnetMaxObservations {
		b.exceeded = true
		return false
	}
	if !b.charge(bytes) {
		return false
	}
	b.observations++
	return true
}
func (b *dotnetBudget) condition(parent, current string) string {
	current = strings.TrimSpace(current)
	size := len(parent) + len(current)
	if parent != "" && current != "" {
		size += 9
	}
	if size > dotnetMaxConditionBytes {
		b.exceeded = true
		return ""
	}
	if parent != "" && current != "" && !b.charge(size) {
		return ""
	}
	return dotnetCondition(parent, current)
}

type dotnetNode struct {
	name     string
	attrs    map[string]string
	line     int
	text     strings.Builder
	children []*dotnetNode
}

// ParseDotnet reads declarations only. It does not evaluate MSBuild expressions,
// restore dependencies, execute targets, or contact package registries.
func ParseDotnet(name string, content []byte) Document {
	name = strings.ReplaceAll(name, `\`, "/")
	doc := Document{}
	if !IsDotnet(name) {
		return doc
	}
	fail := func(code, message string) Document {
		doc.Diagnostics = append(doc.Diagnostics, Diagnostic{Path: name, Code: code, Message: message})
		return doc
	}
	if len(content) > dotnetMaxBytes {
		return fail("manifest-too-large", "Manifest exceeds the 8 MiB declaration parser limit.")
	}
	base := strings.ToLower(path.Base(name))
	if base == "global.json" {
		return parseDotnetGlobal(name, content)
	}
	ext := strings.ToLower(path.Ext(name))
	if ext == ".sln" {
		return parseDotnetSolution(name, content)
	}
	if ext == ".slnf" {
		return parseDotnetSolutionFilter(name, content)
	}
	root, err := readDotnetXML(content)
	if err != nil {
		return fail("invalid-xml", err.Error())
	}
	expected := "Project"
	if ext == ".slnx" {
		expected = "Solution"
	}
	if base == "nuget.config" {
		expected = "configuration"
	}
	if base == "packages.config" {
		expected = "packages"
	}
	if root == nil || root.name != expected {
		return fail("unexpected-root", "Manifest does not have the expected "+expected+" root element.")
	}
	var reqs []Requirement
	var refs []Reference
	var interfaces []Interface
	projectNameCustomized := false
	updatedAspireNames := map[string]bool{}
	unknownAspireNameUpdate := false
	updatedAspireResources := map[string]string{}
	unknownAspireResourceUpdate := false
	budget := dotnetBudget{remaining: dotnetMaxExpandedBytes}
	addReq := func(kind, value, condition string) {
		value = strings.TrimSpace(value)
		if value == "" || !budget.observation(len(kind)+len(value)+len(condition)+len(name)+128) {
			return
		}
		reqs = append(reqs, Requirement{Kind: kind, Value: value, State: dotnetState(value, condition), Evidence: name, Condition: condition})
	}
	addRef := func(ref Reference) {
		if budget.observation(len(ref.Kind) + len(ref.Value) + len(ref.Target) + len(ref.Condition) + len(ref.Evidence) + 128) {
			refs = append(refs, ref)
		}
	}
	var visit func(*dotnetNode, string, string)
	visit = func(n *dotnetNode, inherited, parent string) {
		condition := budget.condition(inherited, n.attrs["Condition"])
		if budget.exceeded {
			return
		}
		value := strings.TrimSpace(n.text.String())
		semanticName := dotnetSemanticName(parent, n.name)
		switch semanticName {
		case "ProjectName":
			if (parent == "PropertyGroup" || parent == "Project") && value != "" {
				projectNameCustomized = true
			}
		case "Project":
			if ext == ".slnx" {
				if v := n.attrs["Path"]; v != "" {
					addRef(dotnetReference(name, "solution-member", v, condition))
				}
			} else {
				for sdk := range strings.SplitSeq(n.attrs["Sdk"], ";") {
					if budget.exceeded {
						break
					}
					addReq("dotnet-sdk", sdk, condition)
				}
			}
		case "Sdk":
			sdk := n.attrs["Name"]
			if v := n.attrs["Version"]; v != "" {
				sdk += "/" + v
			}
			addReq("dotnet-sdk", sdk, condition)
		case "TargetFramework", "TargetFrameworks":
			if parent != "PropertyGroup" && parent != "Project" {
				break
			}
			for v := range strings.SplitSeq(value, ";") {
				if budget.exceeded {
					break
				}
				addReq("target-framework", v, condition)
			}
		case "OutputType":
			if parent != "PropertyGroup" && parent != "Project" {
				break
			}
			// Exe and WinExe are explicit application outputs. Preserve MSBuild
			// expressions as unresolved launch declarations; they may evaluate to
			// either an application or a library in a later build context.
			if value == "Exe" || value == "WinExe" || dotnetDynamic(value) {
				appName := strings.TrimSuffix(path.Base(name), path.Ext(name))
				if appName == "" {
					appName = "application"
				}
				iface := Interface{Kind: "dotnet-application", Name: appName, Target: value, State: dotnetState(value, condition), Evidence: name, Condition: condition, Line: n.line}
				if budget.observation(len(iface.Kind) + len(iface.Name) + len(iface.Target) + len(iface.Condition) + len(name) + 128) {
					interfaces = append(interfaces, iface)
				}
			}
		case "AssemblyName":
			if parent != "PropertyGroup" && parent != "Project" {
				break
			}
			addReq("assembly-name", value, condition)
		case "TargetFrameworkVersion":
			if parent != "PropertyGroup" && parent != "Project" {
				break
			}
			addReq("target-framework-version", value, condition)
		case "LangVersion":
			if parent != "PropertyGroup" && parent != "Project" {
				break
			}
			addReq("language-version", value, condition)
		case "RuntimeIdentifier", "RuntimeIdentifiers":
			if parent != "PropertyGroup" && parent != "Project" {
				break
			}
			for v := range strings.SplitSeq(value, ";") {
				if budget.exceeded {
					break
				}
				addReq("runtime-identifier", v, condition)
			}
		case "ProjectReference":
			if parent != "ItemGroup" && parent != "Project" {
				break
			}
			if strings.TrimSpace(n.attrs["Include"]) == "" && strings.TrimSpace(n.attrs["Update"]) != "" {
				customName := false
				resource, resourceSeen, resourceConflict := "", false, false
				setUpdatedResource := func(value, updateCondition string) {
					value = aspireResourceValue(value, updateCondition)
					if resourceSeen && resource != value {
						resourceConflict = true
					}
					resourceSeen = true
					resource = value
				}
				for key, value := range n.attrs {
					switch strings.ToLower(key) {
					case "aspireprojectmetadatatypename", "projectname", "name":
						customName = true
					case "isaspireprojectresource":
						setUpdatedResource(value, condition)
					}
				}
				for _, child := range n.children {
					switch strings.ToLower(child.name) {
					case "aspireprojectmetadatatypename", "projectname", "name":
						customName = true
					case "isaspireprojectresource":
						childCondition := budget.condition(condition, child.attrs["Condition"])
						setUpdatedResource(strings.TrimSpace(child.text.String()), childCondition)
					}
				}
				if resourceConflict {
					resource = "unresolved"
				}
				if customName || resourceSeen {
					for update := range strings.SplitSeq(n.attrs["Update"], ";") {
						update = strings.TrimSpace(update)
						if update == "" {
							continue
						}
						if !budget.observation(len(update) + len(condition) + 128) {
							unknownAspireNameUpdate = unknownAspireNameUpdate || customName
							unknownAspireResourceUpdate = unknownAspireResourceUpdate || resourceSeen
							break
						}
						target := dotnetReference(name, "project-reference", update, condition).Target
						if target == "" {
							unknownAspireNameUpdate = unknownAspireNameUpdate || customName
							unknownAspireResourceUpdate = unknownAspireResourceUpdate || resourceSeen
							continue
						}
						if customName {
							updatedAspireNames[target] = true
						}
						if resourceSeen {
							if previous, exists := updatedAspireResources[target]; exists && previous != resource {
								updatedAspireResources[target] = "unresolved"
							} else {
								updatedAspireResources[target] = resource
							}
						}
					}
				}
				break
			}
			for v := range strings.SplitSeq(n.attrs["Include"], ";") {
				if budget.exceeded {
					break
				}
				if strings.TrimSpace(v) != "" {
					ref := dotnetReference(name, "project-reference", v, condition)
					ref.AspireResource = "default"
					resourceSeen := false
					resourceConflict := false
					setResource := func(value string) {
						value = aspireResourceValue(value, condition)
						if resourceSeen && ref.AspireResource != value {
							resourceConflict = true
						}
						resourceSeen = true
						ref.AspireResource = value
					}
					for key, value := range n.attrs {
						switch strings.ToLower(key) {
						case "aspireprojectmetadatatypename":
							ref.AspireCustomName = true
						case "projectname", "name":
							ref.AspireCustomName = true
						case "isaspireprojectresource":
							setResource(value)
						}
					}
					for _, child := range n.children {
						key := strings.ToLower(child.name)
						value := strings.TrimSpace(child.text.String())
						childCondition := budget.condition(condition, child.attrs["Condition"])
						switch key {
						case "aspireprojectmetadatatypename":
							ref.AspireCustomName = true
						case "projectname", "name":
							ref.AspireCustomName = true
						case "isaspireprojectresource":
							value = aspireResourceValue(value, childCondition)
							if resourceSeen && ref.AspireResource != value {
								resourceConflict = true
							}
							resourceSeen = true
							ref.AspireResource = value
						}
					}
					if resourceConflict {
						ref.AspireResource = "unresolved"
					}
					addRef(ref)
				}
			}
		case "Reference":
			if parent != "ItemGroup" && parent != "Project" {
				break
			}
			// An assembly reference names an assembly, optionally with a
			// strong-name suffix after a comma. Keep the simple name so the
			// structural view can match it to an in-repository project's
			// assembly name; it says nothing about how the build resolves it.
			for v := range strings.SplitSeq(n.attrs["Include"], ";") {
				if simple, _, _ := strings.Cut(v, ","); strings.TrimSpace(simple) != "" {
					addReq("assembly-reference", strings.TrimSpace(simple), condition)
				}
			}
			// MSBuild permits item metadata in XML attributes as well as child
			// elements. Keep HintPath declarations visible in either form; the
			// normal path resolver leaves property expressions unresolved.
			if hint := strings.TrimSpace(n.attrs["HintPath"]); hint != "" {
				ref := dotnetReference(name, "local-artifact", hint, condition)
				if ref.Target != "" && !strings.EqualFold(path.Ext(ref.Target), ".dll") {
					ref.Target = ""
					ref.State = "unresolved"
				}
				addRef(ref)
			}
			for _, child := range n.children {
				if !strings.EqualFold(child.name, "HintPath") {
					continue
				}
				hint := strings.TrimSpace(child.text.String())
				if hint == "" {
					continue
				}
				childCondition := budget.condition(condition, child.attrs["Condition"])
				ref := dotnetReference(name, "local-artifact", hint, childCondition)
				if ref.Target != "" && !strings.EqualFold(path.Ext(ref.Target), ".dll") {
					ref.Target = ""
					ref.State = "unresolved"
				}
				addRef(ref)
			}
		case "Import":
			if v := n.attrs["Project"]; v != "" {
				ref := dotnetReference(name, "import", v, condition)
				if n.attrs["Sdk"] != "" {
					ref.Target = ""
					ref.State = "unresolved"
				}
				addRef(ref)
			}
			if v := n.attrs["Sdk"]; v != "" {
				if version := n.attrs["Version"]; version != "" {
					v += "/" + version
				}
				addReq("dotnet-sdk", v, condition)
			}
		case "PackageReference", "PackageVersion":
			if parent != "ItemGroup" && parent != "Project" {
				break
			}
			id := n.attrs["Include"]
			if id == "" {
				break
			}
			version := n.attrs["Version"]
			if version != "" {
				addReq("package-reference", id+"@"+version, condition)
			}
			override := n.attrs["VersionOverride"]
			if override != "" {
				addReq("package-reference", id+"@"+override, condition)
			}
			versions := 0
			for _, child := range n.children {
				if strings.EqualFold(child.name, "Version") || strings.EqualFold(child.name, "VersionOverride") {
					childVersion := strings.TrimSpace(child.text.String())
					if childVersion != "" {
						addReq("package-reference", id+"@"+childVersion, budget.condition(condition, child.attrs["Condition"]))
						versions++
					}
				}
			}
			if version == "" && override == "" && versions == 0 {
				addReq("package-reference", id, condition)
			}

		case "package":
			if base == "packages.config" {
				id := n.attrs["id"]
				if v := n.attrs["version"]; v != "" {
					id += "@" + v
				}
				addReq("package-reference", id, condition)
				addReq("target-framework", n.attrs["targetFramework"], condition)
			}
		case "Protobuf", "OpenApiReference", "WCFMetadata", "WCFMetadataStorage":
			if parent != "ItemGroup" && parent != "Project" {
				break
			}
			addReq("code-generation", n.name, condition)
		case "Generator":
			addReq("code-generation", value, condition)
		}
		// Credentials and arbitrary settings in NuGet configuration are not inventory data.
		if base == "nuget.config" {
			return
		}
		var alternatives []string
		for _, child := range n.children {
			if budget.exceeded {
				break
			}
			childCondition := condition
			if n.name == "Choose" && child.name == "Otherwise" {
				branch := "MSBuild Otherwise branch"
				if len(alternatives) > 0 {
					size := 6 + 4*(len(alternatives)-1)
					for _, alternative := range alternatives {
						size += len(alternative)
					}
					if size > dotnetMaxConditionBytes || !budget.charge(size) {
						budget.exceeded = true
						break
					}
					branch = "Not (" + strings.Join(alternatives, " Or ") + ")"
				}
				childCondition = budget.condition(condition, branch)
			}
			visit(child, childCondition, n.name)
			if n.name == "Choose" && child.name == "When" {
				if expression := strings.TrimSpace(child.attrs["Condition"]); expression != "" {
					if !budget.charge(len(expression) + 2) {
						break
					}
					alternatives = append(alternatives, "("+expression+")")
				}
			}
		}
	}
	visit(root, "", "")
	for i := range refs {
		if refs[i].Kind != "project-reference" {
			continue
		}
		if unknownAspireNameUpdate || updatedAspireNames[refs[i].Target] {
			refs[i].AspireCustomName = true
		}
		if unknownAspireResourceUpdate {
			refs[i].AspireResource = "unresolved"
		} else if resource, ok := updatedAspireResources[refs[i].Target]; ok {
			refs[i].AspireResource = resource
		}
	}
	if projectNameCustomized || hasAspireReferenceDefaults(root, "") {
		for i := range refs {
			if refs[i].Kind == "project-reference" {
				refs[i].AspireCustomName = true
			}
		}
	}
	if budget.exceeded {
		doc.Diagnostics = append(doc.Diagnostics, Diagnostic{Path: name, Code: "declaration-limit", Message: "Declaration extraction exceeded its condition, observation, or expanded-text limit; remaining declarations were omitted."})
	}
	if isMSBuildProjectExtension(ext) || ext == ".slnx" {
		kind := "dotnet"
		if ext == ".slnx" {
			kind = "solution"
		}
		doc.Projects = []Project{{ID: name, Root: path.Dir(name), Kind: kind, Evidence: []string{name}, Requirements: reqs, References: refs, Interfaces: interfaces}}
	} else {
		doc.Requirements = reqs
		doc.References = refs
	}
	return doc
}

func hasAspireReferenceDefaults(n *dotnetNode, parent string) bool {
	if strings.EqualFold(parent, "ItemDefinitionGroup") && strings.EqualFold(n.name, "ProjectReference") {
		for key := range n.attrs {
			switch strings.ToLower(key) {
			case "aspireprojectmetadatatypename", "projectname", "name", "isaspireprojectresource":
				return true
			}
		}
		for _, child := range n.children {
			switch strings.ToLower(child.name) {
			case "aspireprojectmetadatatypename", "projectname", "name", "isaspireprojectresource":
				return true
			}
		}
	}
	for _, child := range n.children {
		if hasAspireReferenceDefaults(child, n.name) {
			return true
		}
	}
	return false
}

func aspireResourceValue(value, condition string) string {
	value = strings.TrimSpace(value)
	if condition != "" || dotnetDynamic(value) {
		return "unresolved"
	}
	switch strings.ToLower(value) {
	case "true":
		return "true"
	case "false":
		return "false"
	default:
		return "unresolved"
	}
}

// MSBuild's XML vocabulary is case-sensitive, while property, item and item
// metadata names are case-insensitive. Canonicalize only declarations whose
// exact structural parent establishes one of those name domains.
func dotnetSemanticName(parent, name string) string {
	var supported []string
	switch parent {
	case "PropertyGroup":
		supported = []string{"TargetFramework", "TargetFrameworks", "TargetFrameworkVersion", "LangVersion", "RuntimeIdentifier", "RuntimeIdentifiers", "OutputType", "ProjectName"}
	case "ItemGroup":
		supported = []string{"ProjectReference", "Reference", "PackageReference", "PackageVersion", "Protobuf", "OpenApiReference", "WCFMetadata", "WCFMetadataStorage"}
	default:
		return name
	}
	for _, canonical := range supported {
		if strings.EqualFold(name, canonical) {
			return canonical
		}
	}
	return name
}

func readDotnetXML(content []byte) (*dotnetNode, error) {
	decoded, err := decodeBOMXML(content)
	if err != nil {
		return nil, fmt.Errorf("Cannot parse XML: unsupported or invalid text encoding")
	}
	decoder := xml.NewDecoder(bytes.NewReader(decoded))
	decoder.CharsetReader = func(label string, input io.Reader) (io.Reader, error) {
		if strings.EqualFold(label, "utf-8") || strings.EqualFold(label, "utf-16") {
			return input, nil
		}
		return nil, fmt.Errorf("unsupported XML encoding")
	}
	var stack []*dotnetNode
	var root *dotnetNode
	nodes := 0
	lineCursor := 0
	currentLine := 1
	for {
		before := int(decoder.InputOffset())
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("Cannot parse XML: %w", err)
		}
		after := int(decoder.InputOffset())
		if before < lineCursor || after < before || after > len(decoded) {
			return nil, fmt.Errorf("Cannot parse XML: invalid token offsets")
		}
		if before > lineCursor {
			currentLine += bytes.Count(decoded[lineCursor:before], []byte("\n"))
		}
		segment := decoded[before:after]
		switch t := token.(type) {
		case xml.StartElement:
			nodes++
			if nodes > dotnetMaxNodes || len(stack) >= dotnetMaxDepth {
				return nil, fmt.Errorf("XML exceeds declaration parser depth or element limits")
			}
			n := &dotnetNode{name: t.Name.Local, attrs: make(map[string]string)}
			// Token slices are disjoint, so source line counting remains linear.
			// A multiline start tag points at its opening delimiter.
			if start := bytes.IndexByte(segment, '<'); start >= 0 {
				n.line = currentLine + bytes.Count(segment[:start], []byte("\n"))
			}
			for _, a := range t.Attr {
				if _, exists := n.attrs[a.Name.Local]; exists {
					return nil, fmt.Errorf("XML has duplicate attribute names")
				}
				n.attrs[a.Name.Local] = a.Value
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, n)
			} else {
				if root != nil {
					return nil, fmt.Errorf("XML has more than one root element")
				}
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text.Write(t)
			} else if len(bytes.TrimSpace(t)) > 0 {
				return nil, fmt.Errorf("XML has text outside the root element")
			}
		case xml.Directive:
			return nil, fmt.Errorf("XML directives are not supported by the declaration parser")
		}
		currentLine += bytes.Count(segment, []byte("\n"))
		lineCursor = after
	}
	if len(stack) != 0 {
		return nil, fmt.Errorf("XML has unclosed elements")
	}
	return root, nil
}

func dotnetCondition(parent, current string) string {
	current = strings.TrimSpace(current)
	if parent == "" {
		return current
	}
	if current == "" {
		return parent
	}
	return "(" + parent + ") And (" + current + ")"
}
func dotnetDynamic(value string) bool {
	return strings.Contains(value, "$(") || strings.Contains(value, "@(") || strings.Contains(value, "%(")
}
func dotnetState(value, condition string) string {
	if dotnetDynamic(value) {
		return "unresolved"
	}
	if condition != "" {
		return "conditional"
	}
	return "declared"
}
func dotnetReference(manifest, kind, value, condition string) Reference {
	value = strings.TrimSpace(value)
	ref := Reference{Kind: kind, Value: value, State: dotnetState(value, condition), Evidence: manifest, Condition: condition}
	normalized := strings.ReplaceAll(value, `\`, "/")
	if normalized == "" || dotnetDynamic(value) || strings.ContainsAny(normalized, "*?[]%:\x00\r\n") || strings.HasPrefix(normalized, "/") {
		ref.State = "unresolved"
		return ref
	}
	target := path.Clean(path.Join(path.Dir(manifest), normalized))
	if target == ".." || strings.HasPrefix(target, "../") {
		ref.State = "unresolved"
		return ref
	}
	ref.Target = target
	return ref
}

var dotnetSolutionLine = regexp.MustCompile(`^\s*Project\("((?:[^"\r\n]|"")*)"\)\s*=\s*"(?:[^"\r\n]|"")*"\s*,\s*"((?:[^"\r\n]|"")*)"\s*,\s*"(?:[^"\r\n]|"")*"\s*$`)

func parseDotnetSolution(name string, content []byte) Document {
	project := Project{ID: name, Root: path.Dir(name), Kind: "solution", Evidence: []string{name}}
	doc := Document{}
	budget := dotnetBudget{remaining: dotnetMaxExpandedBytes}
	var err error
	content, err = decodeBOMText(content)
	if err != nil {
		doc.Diagnostics = []Diagnostic{{Path: name, Code: "unsupported-solution-encoding", Message: "Solution text encoding is invalid or unsupported."}}
		return doc
	}
	content = bytes.TrimSpace(content)
	if !bytes.HasPrefix(content, []byte("Microsoft Visual Studio Solution File, Format Version ")) {
		doc.Diagnostics = []Diagnostic{{Path: name, Code: "invalid-solution", Message: "Solution header is missing or unsupported."}}
		return doc
	}
	for line := range strings.SplitSeq(string(content), "\n") {
		if budget.exceeded {
			break
		}
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Project(") {
			continue
		}
		matches := dotnetSolutionLine.FindStringSubmatch(line)
		if matches == nil {
			doc.Diagnostics = append(doc.Diagnostics, Diagnostic{Path: name, Code: "invalid-solution-project", Message: "A solution project declaration could not be read."})
			continue
		}
		if strings.EqualFold(matches[1], "{2150E333-8FDC-42A3-9474-1A3956D46DE8}") {
			continue
		}
		member := strings.ReplaceAll(matches[2], `""`, `"`)
		switch strings.ToLower(path.Ext(member)) {
		case ".csproj", ".fsproj", ".vbproj", ".vcxproj", ".sqlproj", ".wixproj", ".shproj", ".proj":
			ref := dotnetReference(name, "solution-member", member, "")
			if !budget.observation(len(ref.Value) + len(ref.Target) + len(ref.Evidence) + 128) {
				break
			}
			project.References = append(project.References, ref)
		}
	}
	if budget.exceeded {
		doc.Diagnostics = append(doc.Diagnostics, Diagnostic{Path: name, Code: "declaration-limit", Message: "Solution declaration extraction exceeded its observation or expanded-text limit; remaining declarations were omitted."})
	}
	doc.Projects = []Project{project}
	return doc
}

func isMSBuildProjectExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".csproj", ".fsproj", ".vbproj", ".vcxproj", ".sqlproj", ".wixproj", ".shproj", ".proj":
		return true
	}
	return false
}

func parseDotnetSolutionFilter(name string, content []byte) Document {
	project := Project{ID: name, Root: path.Dir(name), Kind: "solution", Evidence: []string{name}}
	var value struct {
		Solution struct {
			Path     string   `json:"path"`
			Projects []string `json:"projects"`
		} `json:"solution"`
	}
	content = bytes.TrimSpace(bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf}))
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if len(content) > dotnetMaxBytes || !uniqueJSONKeys(content) || decoder.Decode(&value) != nil || value.Solution.Path == "" {
		return Document{Diagnostics: []Diagnostic{{Path: name, Code: "invalid-solution-filter", Message: "Solution filter must be a bounded JSON object with a solution path."}}}
	}
	base := dotnetReference(name, "solution-filter-base", value.Solution.Path, "")
	project.References = append(project.References, base)
	if len(value.Solution.Projects) > dotnetMaxObservations {
		return Document{Projects: []Project{project}, Diagnostics: []Diagnostic{{Path: name, Code: "declaration-limit", Message: "Solution filter project entries exceed the observation limit."}}}
	}
	solutionManifest := path.Join(path.Dir(name), strings.ReplaceAll(value.Solution.Path, `\`, "/"))
	for _, member := range value.Solution.Projects {
		project.References = append(project.References, dotnetReference(solutionManifest, "solution-member", member, ""))
	}
	return Document{Projects: []Project{project}}
}

func uniqueJSONKeys(content []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(content))
	var value func() bool
	value = func() bool {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return true
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return false
				}
				seen[name] = true
				if !value() {
					return false
				}
			}
			end, err := decoder.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			for decoder.More() {
				if !value() {
					return false
				}
			}
			end, err := decoder.Token()
			return err == nil && end == json.Delim(']')
		}
		return false
	}
	if !value() {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}

func parseDotnetGlobal(name string, content []byte) Document {
	var settings struct {
		SDK struct {
			Version         string `json:"version"`
			RollForward     string `json:"rollForward"`
			AllowPrerelease *bool  `json:"allowPrerelease"`
		} `json:"sdk"`
		MSBuildSDKs map[string]string `json:"msbuild-sdks"`
	}
	doc := Document{}
	content = bytes.TrimSpace(bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf}))
	if len(content) == 0 || content[0] != '{' {
		doc.Diagnostics = []Diagnostic{{Path: name, Code: "invalid-json", Message: "global.json must contain a JSON object."}}
		return doc
	}
	if err := json.Unmarshal(content, &settings); err != nil {
		doc.Diagnostics = []Diagnostic{{Path: name, Code: "invalid-json", Message: "Cannot parse global.json: " + err.Error()}}
		return doc
	}
	budget := dotnetBudget{remaining: dotnetMaxExpandedBytes}
	add := func(kind, value string) {
		if value != "" && budget.observation(len(kind)+len(value)+len(name)+128) {
			doc.Requirements = append(doc.Requirements, Requirement{Kind: kind, Value: value, State: dotnetState(value, ""), Evidence: name})
		}
	}
	add("dotnet-sdk-version", settings.SDK.Version)
	add("dotnet-sdk-roll-forward", settings.SDK.RollForward)
	if settings.SDK.AllowPrerelease != nil {
		add("dotnet-sdk-allow-prerelease", fmt.Sprint(*settings.SDK.AllowPrerelease))
	}
	// Map iteration must not make otherwise identical reports vary between runs.
	keys := make([]string, 0, len(settings.MSBuildSDKs))
	for key := range settings.MSBuildSDKs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if budget.exceeded {
			break
		}
		add("dotnet-sdk", key+"/"+settings.MSBuildSDKs[key])
	}
	if budget.exceeded {
		doc.Diagnostics = append(doc.Diagnostics, Diagnostic{Path: name, Code: "declaration-limit", Message: "SDK declaration extraction exceeded its observation or expanded-text limit; remaining declarations were omitted."})
	}
	return doc
}
