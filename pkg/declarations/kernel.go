package declarations

import (
	"path"
	"unicode/utf8"
)

type kernelMarkerData struct{}

// ParseKernelMarker records only the presence of a selected Kbuild/Kconfig
// root marker. It deliberately does not interpret build expressions.
func ParseKernelMarker(name string, content []byte) *Document {
	base := path.Base(name)
	if base != "Kbuild" && base != "Kconfig" {
		return nil
	}
	d := NewDocument(name, "kbuild-kconfig-marker")
	d.Data = kernelMarkerData{}
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-kbuild-marker", "A Kbuild/Kconfig marker exceeds the byte limit or is not valid UTF-8.")
	}
	return d
}

// ResolveKernelProjects requires both root marker files in the selected
// inventory. It makes no claims about targets, dependencies, or build output.
func ResolveKernelProjects(docs []*Document, files map[string]bool) {
	markers := map[string]map[string]*Document{}
	for _, d := range docs {
		if d == nil || d.Project == nil {
			continue
		}
		if _, ok := d.Data.(kernelMarkerData); !ok {
			continue
		}
		root, base := d.Project.Root, path.Base(d.Project.ID)
		d.Project = nil
		if !d.Parsed {
			continue
		}
		if markers[root] == nil {
			markers[root] = map[string]*Document{}
		}
		markers[root][base] = d
	}
	for root, byName := range markers {
		kbuildPath, kconfigPath := path.Join(root, "Kbuild"), path.Join(root, "Kconfig")
		if !files[kbuildPath] || !files[kconfigPath] {
			continue
		}
		marker := byName["Kbuild"]
		if marker == nil {
			continue
		}
		project := NewDocument(kbuildPath, "kbuild-kconfig")
		AddRequirement(project, Requirement{Kind: "kbuild-marker", Value: "present", State: "declared", Evidence: kbuildPath})
		AddRequirement(project, Requirement{Kind: "kconfig-marker", Value: "present", State: "declared", Evidence: kconfigPath})
		marker.Project = project.Project
	}
}
