package declarations

import (
	"path"
	"strings"
	"unicode/utf8"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

type goDeclarationData struct{ valid bool }

// ParseGo reads declarations using the upstream module-file grammar. It does
// not select a workspace, evaluate a module graph, or invoke the Go toolchain.
func ParseGo(name string, content []byte) *Document {
	base := path.Base(name)
	if base != "go.mod" && base != "go.work" {
		return nil
	}
	kind := "go"
	if base == "go.work" {
		kind = "go-workspace"
	}
	d := NewDocument(name, kind)
	d.Data = goDeclarationData{}
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-go-manifest", "Go manifest exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	if base == "go.work" {
		f, err := modfile.ParseWork(name, content, nil)
		if err != nil || goPlatformReplacement(f.Replace) {
			d.Parsed = false
			AddDiagnostic(d, "invalid-go-manifest", "Cannot parse go.work with golang.org/x/mod v0.40.0; no declarations were retained.")
			return d
		}
		d.Data = goDeclarationData{valid: true}
		goAddRequirement(d, "declaration-format", "go-work-x-mod-v0.40.0", "")
		goDirectives(d, f.Go, f.Toolchain, f.Godebug)
		for _, use := range f.Use {
			goLocalReference(d, "go-workspace-member", use.Path, "")
		}
		goReplacements(d, f.Replace)
		return d
	}
	f, err := modfile.Parse(name, content, nil)
	if err != nil || f.Module == nil || module.CheckImportPath(f.Module.Mod.Path) != nil || goPlatformReplacement(f.Replace) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-go-manifest", "Cannot parse a module identity with golang.org/x/mod v0.40.0; no declarations were retained.")
		return d
	}
	d.Data = goDeclarationData{valid: true}
	if goAddRequirement(d, "go-module", f.Module.Mod.Path, "") {
		d.Project.Name = f.Module.Mod.Path
	}
	goAddRequirement(d, "declaration-format", "go-mod-x-mod-v0.40.0", "")
	goDirectives(d, f.Go, f.Toolchain, f.Godebug)
	for _, req := range f.Require {
		if !goSupportedModule(d, req.Mod.Path) {
			continue
		}
		condition := ""
		if req.Indirect {
			condition = "indirect declaration"
		}
		AddReference(d, Reference{Kind: "go-require", Value: goModuleVersion(req.Mod), State: "declared", TargetStatus: "external", Evidence: name, Condition: condition})
	}
	for _, excluded := range f.Exclude {
		if !goSupportedModule(d, excluded.Mod.Path) {
			continue
		}
		goAddRequirement(d, "go-exclude", goModuleVersion(excluded.Mod), "")
	}
	goReplacements(d, f.Replace)
	for _, retracted := range f.Retract {
		value := retracted.Low
		if retracted.Low != retracted.High {
			value += ".." + retracted.High
		}
		goAddRequirement(d, "go-retract", value, "")
	}
	for _, tool := range f.Tool {
		if !goSupportedModule(d, tool.Path) {
			continue
		}
		AddInterface(d, Interface{Kind: "tool", Name: tool.Path, State: "declared", Evidence: name})
	}
	for _, ignored := range f.Ignore {
		if target, ok := LocalTarget(name, ignored.Path, ""); ok && !strings.Contains(ignored.Path, "\\") {
			goAddRequirement(d, "go-ignore-directory", target, "")
		} else {
			AddDiagnostic(d, "external-go-ignore", "An ignore declaration is outside the supported selected-root path syntax.")
		}
	}
	return d
}

func goAddRequirement(d *Document, kind, value, condition string) bool {
	return AddRequirement(d, Requirement{Kind: kind, Value: value, State: "declared", Evidence: d.Project.ID, Condition: condition})
}

func goDirectives(d *Document, minimum *modfile.Go, toolchain *modfile.Toolchain, debug []*modfile.Godebug) {
	if minimum != nil {
		goAddRequirement(d, "go-language-minimum", minimum.Version, "")
	}
	if toolchain != nil {
		goAddRequirement(d, "go-toolchain-suggestion", toolchain.Name, "")
	}
	for _, setting := range debug {
		goAddRequirement(d, "go-debug-default", setting.Key+"="+setting.Value, "")
	}
}

func goModuleVersion(v module.Version) string {
	if v.Version == "" {
		return v.Path
	}
	return v.Path + "@" + v.Version
}

func goSupportedModule(d *Document, value string) bool {
	if module.CheckImportPath(value) != nil {
		AddDiagnostic(d, "unsupported-go-module-path", "A module or tool import path has unsupported syntax; its raw value was omitted.")
		return false
	}
	return true
}

// The upstream parser rejects backslash replacement paths on Unix. Apply the
// same supported subset on Windows so reports do not depend on the host OS.
func goPlatformReplacement(replacements []*modfile.Replace) bool {
	for _, replacement := range replacements {
		if replacement.New.Version == "" && strings.Contains(replacement.New.Path, "\\") {
			return true
		}
	}
	return false
}

func goReplacements(d *Document, replacements []*modfile.Replace) {
	for _, replacement := range replacements {
		if !goSupportedModule(d, replacement.Old.Path) || replacement.New.Version != "" && !goSupportedModule(d, replacement.New.Path) {
			continue
		}
		old := goModuleVersion(replacement.Old)
		if replacement.New.Version == "" {
			goLocalReference(d, "go-local-replacement", replacement.New.Path, old)
		} else {
			AddReference(d, Reference{Kind: "go-remote-replacement", Value: old + " => " + goModuleVersion(replacement.New), State: "declared", TargetStatus: "external", Evidence: d.Project.ID})
		}
	}
}

func goLocalReference(d *Document, kind, raw, replaced string) {
	ref := Reference{Kind: kind, Value: raw, State: "declared", Evidence: d.Project.ID, Condition: replaced}
	if target, ok := LocalTarget(d.Project.ID, raw, "go.mod"); ok && !strings.Contains(raw, "\\") {
		ref.Target = target
	} else {
		ref.Value = "[outside-selected-inventory]"
		ref.State, ref.TargetStatus = "unresolved", "external"
		AddDiagnostic(d, "external-go-reference", "A local Go declaration is external or uses unsupported platform-dependent path syntax.")
	}
	AddReference(d, ref)
}

// ResolveGo checks literal targets only against the selected inventory. A
// present file is not assumed to contain a valid module or to be buildable.
func ResolveGo(docs []*Document, files map[string]bool) {
	modules := map[string]*Document{}
	for _, d := range docs {
		if d != nil && d.Project != nil && d.Project.Kind == "go" {
			if data, ok := d.Data.(goDeclarationData); ok && data.valid {
				modules[d.Project.ID] = d
			}
		}
	}
	for _, d := range docs {
		if d == nil || d.Project == nil || !strings.HasPrefix(d.Project.Kind, "go") {
			continue
		}
		if _, ok := d.Data.(goDeclarationData); !ok {
			continue
		}
		for i := range d.Project.References {
			ref := &d.Project.References[i]
			if ref.Target == "" || (ref.Kind != "go-workspace-member" && ref.Kind != "go-local-replacement") {
				continue
			}
			if !files[ref.Target] {
				ref.TargetStatus, ref.State = "missing", "missing"
			} else if modules[ref.Target] == nil {
				ref.TargetStatus, ref.State = "present", "unresolved"
				AddDiagnostic(d, "unparsed-go-target", "A local Go target is present but is not a successfully parsed module.")
			} else {
				ref.TargetStatus, ref.State = "present", "resolved"
			}
		}
	}
}
