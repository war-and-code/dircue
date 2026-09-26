// Package intentmap observes declared interfaces, configuration entry points,
// source imports, and neutral capability evidence without executing content.
package intentmap

import (
	"path"
	"strings"

	"github.com/war-and-code/dircue/pkg/declarations"
)

const (
	DetectorName           = "dircue-intent-map"
	DetectorVersion        = "1.0.0"
	DefaultMaxFileBytes    = 1 << 20
	DefaultMaxObservations = 4096
)

type Kind string

const (
	KindInterface  Kind = "interface"
	KindConfig     Kind = "config_entry_point"
	KindImport     Kind = "import"
	KindCapability Kind = "capability"
)

type Observation struct {
	Kind      Kind   `json:"kind"`
	Name      string `json:"name"`
	ProjectID string `json:"project_id,omitempty"`
	// ProjectAttribution is "directory_containment" when ProjectID was assigned
	// by path-containment heuristic in Finish rather than by the source
	// observation. An empty value means the ProjectID was explicitly set by the
	// source (e.g. a declared requirement in a manifest).
	ProjectAttribution string            `json:"project_attribution,omitempty"`
	State              string            `json:"state"`
	Basis              string            `json:"basis"`
	Path               string            `json:"path"`
	StartLine          int               `json:"start_line,omitempty"`
	EndLine            int               `json:"end_line,omitempty"`
	Properties         map[string]string `json:"properties,omitempty"`
}

type Coverage struct {
	Status               string         `json:"status"`
	SelectedFiles        int            `json:"selected_files"`
	InspectedFiles       int            `json:"inspected_files"`
	OmittedFiles         int            `json:"omitted_files"`
	RetainedObservations int            `json:"retained_observations"`
	Omissions            map[string]int `json:"omissions"`
}

type Report struct {
	Provider        string        `json:"provider"`
	ProviderVersion string        `json:"provider_version"`
	Coverage        Coverage      `json:"coverage"`
	Observations    []Observation `json:"observations"`
}

type Options struct {
	MaxFileBytes    int64
	MaxObservations int
}

// AddDeclarations converts already-retained manifest interfaces and dependency
// declarations. It never reads their evidence paths.
func (d *Detector) AddDeclarations(projects []declarations.Project) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, project := range projects {
		d.projects[project.ID] = project.Root
		// Store the module/package name base for Go binary naming. When
		// main.go sits at the project root, the binary name comes from
		// the module path rather than the directory name (which is not
		// portable across different checkout paths).
		if name := moduleBaseName(project.Name); project.Name != "" && name != "" && name != "." {
			d.projectNames[project.ID] = name
		}
		for _, v := range project.Interfaces {
			d.addLocked(Observation{Kind: KindInterface, Name: v.Name, ProjectID: project.ID, State: v.State, Basis: "declared_manifest", Path: v.Evidence, Properties: compact(map[string]string{"interface_kind": v.Kind, "target": v.Target, "condition": v.Condition})})
		}
		for _, req := range project.Requirements {
			// PEP 735 dependency groups (dev, test, docs, lint) are never
			// published with the package; like npm devDependencies they
			// describe tooling, not what the component uses.
			if strings.HasPrefix(req.Condition, "group:") {
				continue
			}
			// Gemfile groups made only of development and test gems describe
			// tooling. Other groups, such as production, keep their gems.
			if gemfileToolingGroup(req.Condition) {
				continue
			}
			// dev_dependencies (Dart/Flutter and any future ecosystem using this
			// convention) describe build/test tooling, not runtime capabilities.
			if req.Condition == "dev_dependencies" {
				continue
			}
			for _, capability := range capabilitiesFor(req.Kind, req.Value) {
				d.addLocked(Observation{Kind: KindCapability, Name: capability, ProjectID: project.ID, State: declarationState(req.State), Basis: "declared_dependency", Path: req.Evidence, Properties: compact(map[string]string{"requirement_kind": req.Kind, "requirement": req.Value, "condition": req.Condition})})
			}
		}
		// Some ecosystems store dependencies as References rather than Requirements
		// (npm-dependency, go-require). Process them here so they produce capability
		// observations alongside the other ecosystems.
		for _, ref := range project.References {
			switch ref.Kind {
			case "npm-dependency", "npm-workspace-dependency", "go-require":
			default:
				continue
			}
			// Development-only packages (test runners, fixtures, session
			// stores used in examples) describe how the project is built and
			// tested, not what the component itself uses.
			if ref.Condition == "devDependencies" {
				continue
			}
			for _, capability := range capabilitiesFor(ref.Kind, ref.Value) {
				state := declarationState(ref.State)
				// Optional and peer dependencies are conditional: the package may
				// or may not be installed, so capabilities derived from them are
				// conditional too.
				if ref.Condition == "optionalDependencies" || ref.Condition == "peerDependencies" {
					state = "conditional"
				}
				d.addLocked(Observation{Kind: KindCapability, Name: capability, ProjectID: project.ID, State: state, Basis: "declared_dependency", Path: ref.Evidence, Properties: compact(map[string]string{"requirement_kind": ref.Kind, "requirement": ref.Value, "condition": ref.Condition})})
			}
		}
	}
}

// moduleBaseName returns the last element of a module or package name,
// skipping a trailing Go major-version element such as /v3.
func moduleBaseName(name string) string {
	base := path.Base(name)
	if dir := path.Dir(name); dir != "." && len(base) > 1 && base[0] == 'v' && strings.Trim(base[1:], "0123456789") == "" {
		return path.Base(dir)
	}
	return base
}

// gemfileToolingGroup reports whether a Gemfile group condition names only
// the development and test groups.
func gemfileToolingGroup(condition string) bool {
	groups, ok := strings.CutPrefix(condition, "gemfile-group:")
	if !ok || groups == "" {
		return false
	}
	for _, group := range strings.Split(groups, ",") {
		if group != "development" && group != "test" {
			return false
		}
	}
	return true
}
