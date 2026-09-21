package declarations

import (
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

type cargoData struct {
	usable                      bool
	incomplete                  bool
	packagePresent, workspace   bool
	members, excludes, defaults []string
	defaultsSet                 bool
	workspacePath               string
	workspacePathSet            bool
	inherited                   map[string]bool
	workspaceValues             map[string]string
	dependencies                []cargoDependency
	workspaceDependencies       map[string]cargoDependency
	build                       any
}

type cargoDependency struct {
	alias, name, version, localPath, scope, selector, manifest string
	source                                                     string
	optional, inherited                                        bool
	features                                                   []string
	defaultFeatures                                            *bool
	valid                                                      bool
}

// ParseCargo observes manifest declarations; membership is linked separately
// against the selected inventory, without running Cargo or build scripts.
func ParseCargo(name string, content []byte) *Document {
	if path.Base(name) != "Cargo.toml" {
		return nil
	}
	d := NewDocument(name, "cargo")
	object, err := ValidateTOML(content)
	if err != nil {
		d.Parsed = false
		AddDiagnostic(d, "invalid-cargo-toml", "Cargo manifest could not be read within the TOML limits.")
		return d
	}
	data := &cargoData{usable: true, inherited: map[string]bool{}, workspaceValues: map[string]string{}, workspaceDependencies: map[string]cargoDependency{}}
	d.Data = data
	pkg, hasPackage := cargoTable(object["package"])
	workspace, hasWorkspace := cargoTable(object["workspace"])
	data.packagePresent, data.workspace = hasPackage, hasWorkspace
	if !hasPackage && !hasWorkspace {
		AddDiagnostic(d, "cargo-missing-project", "Cargo manifest has neither a package nor a workspace table.")
	}
	if _, exists := object["package"]; exists && !hasPackage {
		cargoInvalid(d, "package")
	}
	if _, exists := object["workspace"]; exists && !hasWorkspace {
		cargoInvalid(d, "workspace")
	}
	if hasWorkspace {
		if !hasPackage {
			d.Project.Kind = "cargo-workspace"
		}
		AddRequirement(d, Requirement{Kind: "cargo-workspace-root", Value: "true", State: "declared", Evidence: name})
		count := 0
		data.members = cargoPatterns(d, data, workspace, "members", &count)
		data.excludes = cargoPatterns(d, data, workspace, "exclude", &count)
		data.defaults = cargoPatterns(d, data, workspace, "default-members", &count)
		_, data.defaultsSet = workspace["default-members"]
		if v, exists := workspace["resolver"]; exists {
			cargoScalar(d, "cargo-resolver", v, []string{"1", "2", "3"})
		}
		if values, ok := cargoTable(workspace["package"]); ok {
			for _, key := range []string{"version", "edition", "rust-version"} {
				if value, exists := values[key]; exists {
					if text, ok := cargoPackageValue(key, value); ok {
						data.workspaceValues[key] = text
						AddRequirement(d, Requirement{Kind: "cargo-workspace-" + key, Value: text, State: "declared", Evidence: name})
					} else {
						cargoInvalid(d, "workspace.package."+key)
					}
				}
			}
		} else if _, exists := workspace["package"]; exists {
			cargoInvalid(d, "workspace.package")
		}
		if deps, ok := cargoTable(workspace["dependencies"]); ok {
			for _, alias := range cargoKeys(deps) {
				if len(data.workspaceDependencies) >= MaxObservationsPerManifest {
					AddDiagnostic(d, "cargo-dependency-limit", "Cargo workspace dependency declarations exceed the observation limit.")
					break
				}
				dep := cargoParseDependency(d, alias, deps[alias], "workspace.dependencies", "")
				if dep.optional || dep.inherited {
					dep.valid = false
					cargoInvalid(d, "workspace.dependencies")
				}
				data.workspaceDependencies[alias] = dep
			}
		} else if _, exists := workspace["dependencies"]; exists {
			cargoInvalid(d, "workspace.dependencies")
		}
	}
	for _, key := range []string{"patch", "replace"} {
		if _, exists := object[key]; exists {
			AddDiagnostic(d, "cargo-dependency-overrides-unevaluated", "Cargo dependency override declarations are outside the supported static dependency subset.")
		}
	}
	if _, exists := object["cargo-features"]; exists {
		AddDiagnostic(d, "cargo-nightly-features", "Nightly Cargo feature declarations are not evaluated.")
	}
	if features, exists := object["features"]; exists {
		if table, ok := cargoTable(features); ok {
			for _, feature := range cargoKeys(table) {
				members, valid := cargoStrings(table[feature])
				if !cargoIdentifier(feature) || !valid || len(members) > MaxPatterns {
					cargoInvalid(d, "features."+feature)
					continue
				}
				AddRequirement(d, Requirement{Kind: "cargo-feature", Value: feature, State: "declared", Evidence: name})
			}
		} else {
			cargoInvalid(d, "features")
		}
	}
	if hasPackage {
		if name, ok := pkg["name"].(string); ok && cargoIdentifier(name) {
			d.Project.Name = name
		} else {
			cargoInvalid(d, "package.name")
		}
		for _, key := range []string{"version", "edition", "rust-version"} {
			if value, exists := pkg[key]; exists {
				if table, ok := cargoTable(value); ok {
					if inherited, ok := table["workspace"].(bool); ok && inherited && len(table) == 1 {
						data.inherited[key] = true
					} else {
						cargoInvalid(d, "package."+key)
					}
				} else if text, ok := cargoPackageValue(key, value); ok {
					if key == "version" {
						d.Project.Version = text
					}
					AddRequirement(d, Requirement{Kind: "cargo-" + key, Value: text, State: "declared", Evidence: d.Project.ID})
				} else {
					cargoInvalid(d, "package."+key)
				}
			}
		}
		if value, exists := pkg["workspace"]; exists {
			data.workspacePathSet = true
			if value, ok := value.(string); ok && cargoText(value) {
				data.workspacePath = value
			} else {
				cargoInvalid(d, "package.workspace")
			}
			if hasWorkspace {
				AddDiagnostic(d, "cargo-conflicting-workspace", "A package cannot declare both a workspace root and package.workspace.")
			}
		}
		if value, exists := pkg["build"]; exists {
			switch value := value.(type) {
			case bool:
				data.build = value
			case string:
				if cargoText(value) {
					data.build = value
				} else {
					cargoInvalid(d, "package.build")
				}
			default:
				cargoInvalid(d, "package.build")
			}
		}
		cargoTargets(d, object)
		if value, exists := pkg["default-run"]; exists {
			if text, ok := value.(string); ok && cargoIdentifier(text) {
				AddInterface(d, Interface{Kind: "cargo-default-run", Name: text, State: "declared", Evidence: name})
			} else {
				cargoInvalid(d, "package.default-run")
			}
		}
		for _, scope := range []string{"dependencies", "dev-dependencies", "build-dependencies"} {
			cargoDependencyTable(d, data, object, scope, "")
		}
		if targets, ok := cargoTable(object["target"]); ok {
			for _, selector := range cargoKeys(targets) {
				table, ok := cargoTable(targets[selector])
				if !ok || !cargoText(selector) {
					cargoInvalid(d, "target")
					continue
				}
				for _, scope := range []string{"dependencies", "dev-dependencies", "build-dependencies"} {
					cargoDependencyTable(d, data, table, scope, selector)
				}
			}
		} else if _, exists := object["target"]; exists {
			cargoInvalid(d, "target")
		}
	}
	return d
}

func cargoTable(value any) (map[string]any, bool) {
	table, ok := value.(map[string]any)
	return table, ok
}
func cargoKeys(table map[string]any) []string {
	keys := make([]string, 0, len(table))
	for key := range table {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
func cargoText(value string) bool {
	return value != "" && len(value) <= MaxStringBytes && !strings.ContainsFunc(value, unicode.IsControl)
}
func cargoIdentifier(value string) bool {
	if !cargoText(value) {
		return false
	}
	for _, r := range value {
		if r != '-' && r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

var cargoVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)
var cargoRustVersionPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,2}$`)

func cargoPackageValue(key string, value any) (string, bool) {
	text, ok := value.(string)
	if !ok || !cargoText(text) {
		return "", false
	}
	switch key {
	case "edition":
		return text, slices.Contains([]string{"2015", "2018", "2021", "2024"}, text)
	case "version":
		return text, cargoVersionPattern.MatchString(text)
	case "rust-version":
		return text, cargoRustVersionPattern.MatchString(text)
	}
	return text, true
}

func cargoInvalid(d *Document, field string) {
	AddDiagnostic(d, "invalid-cargo-declaration", "Unsupported or invalid Cargo declaration in "+field+".")
}
func cargoScalar(d *Document, kind string, value any, allowed []string) {
	text, ok := value.(string)
	if !ok || !slices.Contains(allowed, text) {
		cargoInvalid(d, kind)
		return
	}
	AddRequirement(d, Requirement{Kind: kind, Value: text, State: "declared", Evidence: d.Project.ID})
}
func cargoPatterns(d *Document, data *cargoData, table map[string]any, key string, count *int) []string {
	value, exists := table[key]
	if !exists {
		return nil
	}
	values, ok := value.([]any)
	if !ok {
		data.usable = false
		cargoInvalid(d, "workspace."+key)
		return nil
	}
	out := []string{}
	for _, value := range values {
		if *count >= MaxPatterns {
			data.usable = false
			AddDiagnostic(d, "cargo-pattern-limit", "Cargo workspace patterns exceed the per-manifest limit.")
			break
		}
		*count++
		text, ok := value.(string)
		if !ok || !cargoText(text) {
			if key == "exclude" {
				data.usable = false
			} else {
				data.incomplete = true
			}
			cargoInvalid(d, "workspace."+key)
			continue
		}
		// Invalid external paths are represented without disclosing host paths.
		if _, ok := LocalTarget(d.Project.ID, text, "Cargo.toml"); !ok {
			if key == "exclude" {
				data.usable = false
			} else {
				data.incomplete = true
			}
			AddReference(d, Reference{Kind: "cargo-workspace-" + key, Value: "[outside-selected-root]", State: "unresolved", TargetStatus: "external", Evidence: d.Project.ID})
			AddDiagnostic(d, "cargo-workspace-pattern-unresolved", "A Cargo workspace pattern is outside the selected inventory; valid sibling patterns remain eligible for resolution.")
			continue
		}
		if strings.ContainsAny(text, "*?[") {
			if _, err := MatchPattern(text, ""); err != nil {
				if key == "exclude" {
					data.usable = false
				} else {
					data.incomplete = true
				}
				AddDiagnostic(d, "cargo-unsupported-pattern", "Cargo workspace pattern uses unsupported syntax.")
				continue
			}
		}
		out = append(out, text)
	}
	return out
}
func cargoDependencyTable(d *Document, data *cargoData, table map[string]any, scope, selector string) {
	value, exists := table[scope]
	if !exists {
		return
	}
	deps, ok := cargoTable(value)
	if !ok {
		cargoInvalid(d, scope)
		return
	}
	for _, alias := range cargoKeys(deps) {
		if len(data.dependencies) >= MaxObservationsPerManifest {
			AddDiagnostic(d, "cargo-dependency-limit", "Cargo dependency declarations exceed the per-manifest limit.")
			return
		}
		data.dependencies = append(data.dependencies, cargoParseDependency(d, alias, deps[alias], scope, selector))
	}
}
func cargoParseDependency(d *Document, alias string, value any, scope, selector string) cargoDependency {
	dep := cargoDependency{alias: alias, name: alias, scope: scope, selector: selector, valid: true, source: "registry", manifest: d.Project.ID}
	if !cargoIdentifier(alias) {
		dep.valid = false
		cargoInvalid(d, scope)
		return dep
	}
	if text, ok := value.(string); ok {
		if cargoDependencyVersion(text) {
			dep.version = text
		} else {
			dep.valid = false
			cargoInvalid(d, scope)
		}
		return dep
	}
	table, ok := cargoTable(value)
	if !ok {
		dep.valid = false
		cargoInvalid(d, scope)
		return dep
	}
	for key, value := range table {
		switch key {
		case "package":
			text, ok := value.(string)
			if ok && cargoIdentifier(text) {
				dep.name = text
			} else {
				dep.valid = false
			}
		case "version":
			text, ok := value.(string)
			if ok && cargoDependencyVersion(text) {
				dep.version = text
			} else {
				dep.valid = false
			}
		case "path":
			text, ok := value.(string)
			if ok && cargoText(text) {
				dep.localPath = text
				dep.source = "path"
			} else {
				dep.valid = false
			}
		case "git":
			dep.source = "git" // Deliberately omit repository URLs and credentials.
			if text, ok := value.(string); !ok || !cargoText(text) {
				dep.valid = false
			}
		case "registry":
			if text, ok := value.(string); !ok || !cargoIdentifier(text) {
				dep.valid = false
			}
		case "rev", "tag", "branch":
			if text, ok := value.(string); !ok || !cargoText(text) {
				dep.valid = false
			}
		case "optional":
			flag, ok := value.(bool)
			if ok {
				dep.optional = flag
			} else {
				dep.valid = false
			}
		case "workspace":
			flag, ok := value.(bool)
			if ok && flag {
				dep.inherited = true
			} else {
				dep.valid = false
			}
		case "default-features":
			flag, ok := value.(bool)
			if ok {
				dep.defaultFeatures = &flag
			} else {
				dep.valid = false
			}
		case "features":
			values, ok := cargoStrings(value)
			if ok {
				dep.features = values
			} else {
				dep.valid = false
			}
		}
	}
	if _, git := table["git"]; git {
		dep.source = "git"
		if dep.localPath != "" {
			dep.valid = false
		}
	}
	if dep.inherited {
		for key := range table {
			if key != "workspace" && key != "optional" && key != "features" {
				dep.valid = false
			}
		}
	}
	if !dep.inherited && dep.version == "" && dep.localPath == "" && dep.source != "git" {
		dep.valid = false
	}
	if !dep.valid {
		cargoInvalid(d, scope)
	}
	return dep
}
func cargoDependencyVersion(value string) bool {
	if !cargoText(value) || !strings.ContainsAny(value, "0123456789*") {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || strings.ContainsRune(".+-<>=^~*, ", r)) {
			return false
		}
	}
	return true
}
func cargoStrings(value any) ([]string, bool) {
	values, ok := value.([]any)
	if !ok || len(values) > MaxPatterns {
		return nil, false
	}
	out := []string{}
	length := 0
	for _, value := range values {
		text, ok := value.(string)
		if !ok || !cargoText(text) {
			return nil, false
		}
		length += len(text) + 1
		if length > MaxStringBytes {
			return nil, false
		}
		out = append(out, text)
	}
	return out, true
}
func cargoTargets(d *Document, object map[string]any) {
	for _, kind := range []string{"lib", "bin", "test", "bench", "example"} {
		value, exists := object[kind]
		if !exists {
			continue
		}
		var targets []any
		if kind == "lib" {
			targets = []any{value}
		} else {
			var ok bool
			targets, ok = value.([]any)
			if !ok {
				cargoInvalid(d, kind)
				continue
			}
		}
		for i, value := range targets {
			if i >= MaxObservationsPerManifest {
				AddDiagnostic(d, "cargo-target-limit", "Cargo target declarations exceed the observation limit.")
				break
			}
			target, ok := cargoTable(value)
			if !ok {
				cargoInvalid(d, kind)
				continue
			}
			name, _ := target["name"].(string)
			if name == "" && kind == "lib" {
				name = strings.ReplaceAll(d.Project.Name, "-", "_")
			}
			if !cargoIdentifier(name) {
				cargoInvalid(d, kind+".name")
				continue
			}
			observation := Interface{Kind: "cargo-" + kind, Name: name, State: "declared", Evidence: d.Project.ID}
			if raw, exists := target["path"]; exists {
				raw, ok := raw.(string)
				if !ok || !cargoText(raw) {
					cargoInvalid(d, kind+".path")
					continue
				}
				resolved, ok := LocalTarget(d.Project.ID, raw, "")
				if ok {
					observation.Target = resolved
				} else {
					observation.State = "unresolved"
					AddDiagnostic(d, "cargo-external-target", "Cargo target path lies outside the selected inventory or uses unsupported path syntax.")
				}
			}
			if raw, exists := target["required-features"]; exists {
				features, ok := cargoStrings(raw)
				if !ok {
					cargoInvalid(d, kind+".required-features")
					continue
				}
				if len(features) > 0 {
					observation.Condition = "required-features=" + strings.Join(features, ",")
					if observation.State == "declared" {
						observation.State = "conditional"
					}
				}
			}
			if !AddInterface(d, observation) {
				break
			}
		}
	}
}
