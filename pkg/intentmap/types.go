// Package intentmap observes declared interfaces, configuration entry points,
// source imports, and neutral capability evidence without executing content.
package intentmap

import "dircue/pkg/declarations"

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
	Kind       Kind              `json:"kind"`
	Name       string            `json:"name"`
	ProjectID  string            `json:"project_id,omitempty"`
	State      string            `json:"state"`
	Basis      string            `json:"basis"`
	Path       string            `json:"path"`
	StartLine  int               `json:"start_line,omitempty"`
	EndLine    int               `json:"end_line,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
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
		for _, v := range project.Interfaces {
			d.addLocked(Observation{Kind: KindInterface, Name: v.Name, ProjectID: project.ID, State: v.State, Basis: "declared_manifest", Path: v.Evidence, Properties: compact(map[string]string{"interface_kind": v.Kind, "target": v.Target, "condition": v.Condition})})
		}
		for _, req := range project.Requirements {
			for _, capability := range capabilitiesFor(req.Value) {
				d.addLocked(Observation{Kind: KindCapability, Name: capability, ProjectID: project.ID, State: declarationState(req.State), Basis: "declared_dependency", Path: req.Evidence, Properties: compact(map[string]string{"requirement_kind": req.Kind, "requirement": req.Value, "condition": req.Condition})})
			}
		}
	}
}
