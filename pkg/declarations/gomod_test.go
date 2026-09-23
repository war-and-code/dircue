package declarations

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestGoModuleDeclarations(t *testing.T) {
	d := ParseGo("service/go.mod", []byte(`module example.org/service
go 1.24.0
toolchain go1.25.2
require (
 example.org/library v1.2.3
 example.org/indirect v1.0.0 // indirect
)
exclude example.org/obsolete v1.0.0
replace example.org/library => ../library
replace example.org/old v1.0.0 => example.org/fork v1.2.0
retract [v1.0.0, v1.1.0]
tool example.org/tools/check
godebug panicnil=1
`))
	if !d.Parsed || len(d.Diagnostics) != 0 || d.Project.Name != "example.org/service" {
		t.Fatalf("module: %+v", d)
	}
	for kind, value := range map[string]string{
		"go-language-minimum": "1.24.0", "go-toolchain-suggestion": "go1.25.2", "go-exclude": "example.org/obsolete@v1.0.0", "go-retract": "v1.0.0..v1.1.0", "go-debug-default": "panicnil=1",
	} {
		found := false
		for _, req := range d.Project.Requirements {
			found = found || req.Kind == kind && req.Value == value && req.Evidence == "service/go.mod"
		}
		if !found {
			t.Errorf("missing %s=%s: %+v", kind, value, d.Project.Requirements)
		}
	}
	if len(d.Project.References) != 4 {
		t.Fatalf("references: %+v", d.Project.References)
	}
	for _, ref := range d.Project.References {
		if ref.Kind == "go-local-replacement" && (ref.Target != "library/go.mod" || ref.Condition != "example.org/library" || ref.State != "declared") {
			t.Errorf("local replacement: %+v", ref)
		}
		if ref.Value == "example.org/indirect@v1.0.0" && ref.Condition != "indirect declaration" {
			t.Errorf("indirect: %+v", ref)
		}
	}
	if len(d.Project.Interfaces) != 1 || d.Project.Interfaces[0].Name != "example.org/tools/check" {
		t.Fatalf("interfaces: %+v", d.Project.Interfaces)
	}
}

func TestGoWorkspaceResolutionUsesOnlySelectedModules(t *testing.T) {
	workspace := ParseGo("group/go.work", []byte(`go 1.24.0
use (
 ./service
 ./broken
 ./absent
 ../../outside
)
replace example.org/dep => ../library
`))
	service := ParseGo("group/service/go.mod", []byte("module example.org/service\ngo 1.24.0\n"))
	broken := ParseGo("group/broken/go.mod", []byte("unknown directive\n"))
	library := ParseGo("library/go.mod", []byte("module example.org/library\ngo 1.24.0\n"))
	unrelated := ParseGo("group/unrelated/go.mod", []byte("module example.org/unrelated\ngo 1.24.0\n"))
	files := map[string]bool{"group/go.work": true, "group/service/go.mod": true, "group/broken/go.mod": true, "library/go.mod": true, "group/unrelated/go.mod": true}
	ResolveGo([]*Document{workspace, service, broken, library, unrelated}, files)
	want := map[string]string{"group/service/go.mod": "resolved/present", "group/broken/go.mod": "unresolved/present", "group/absent/go.mod": "missing/missing", "": "unresolved/external", "library/go.mod": "resolved/present"}
	for _, ref := range workspace.Project.References {
		if got := ref.State + "/" + ref.TargetStatus; got != want[ref.Target] {
			t.Errorf("target %q got %s want %s", ref.Target, got, want[ref.Target])
		}
		delete(want, ref.Target)
	}
	if len(want) != 0 {
		t.Fatalf("missing references: %v", want)
	}
	if len(service.Project.References) != 0 || len(unrelated.Project.References) != 0 {
		t.Fatal("workspace membership leaked into independent module declarations")
	}
}

func TestGoManifestFailuresDoNotEchoInput(t *testing.T) {
	for _, content := range []string{
		"module example.org/app\nunknown secret-value\n",
		"module example.org/app\nreplace example.org/lib => /secret-value\ninvalid\n",
		"go 1.24.0\n",
		"module bad@secret-value\n",
		string([]byte{0xff}),
		strings.Repeat("x", int(MaxManifestBytes)+1),
	} {
		d := ParseGo("go.mod", []byte(content))
		if d.Parsed || len(d.Diagnostics) == 0 || len(d.Project.Requirements)+len(d.Project.References) != 0 {
			t.Fatalf("invalid manifest retained observations: %+v", d)
		}
		encoded, _ := json.Marshal(d)
		if strings.Contains(string(encoded), "secret-value") {
			t.Fatal("diagnostic disclosed parser input")
		}
	}
	if ParseGo("README.md", nil) != nil {
		t.Fatal("unrelated path recognized")
	}
}

func TestGoExternalPathsAreRedacted(t *testing.T) {
	d := ParseGo("go.work", []byte("go 1.24.0\nuse /secret-value\n"))
	if !d.Parsed || len(d.Project.References) != 1 {
		t.Fatalf("workspace: %+v", d)
	}
	encoded, _ := json.Marshal(d.Project)
	if strings.Contains(string(encoded), "secret-value") || d.Project.References[0].TargetStatus != "external" {
		t.Fatalf("external reference: %s", encoded)
	}
}

func TestGoModulePathTextIsValidatedBeyondParserGrammar(t *testing.T) {
	d := ParseGo("go.mod", []byte("module example.org/app\ngo 1.24.0\nrequire https://user:SECRET@example.org/lib v1.0.0\nreplace example.org/old => https://user:SECRET@example.org/fork v1.0.0\n"))
	encoded, _ := json.Marshal(d.Project)
	if strings.Contains(string(encoded), "SECRET") {
		t.Fatalf("unchecked module path leaked: %s", encoded)
	}
	if len(d.Diagnostics) == 0 {
		t.Fatal("unsupported path was not diagnosed")
	}
	for _, name := range []string{"go.mod", "go.work"} {
		prefix := "go 1.24.0\n"
		if name == "go.mod" {
			prefix = "module example.org/app\n" + prefix
		}
		d := ParseGo(name, []byte(prefix+"replace example.org/old => ..\\local\n"))
		if d.Parsed || len(d.Diagnostics) == 0 {
			t.Fatal("backslash replacement behavior would depend on the host OS")
		}
	}
}

func TestGoObservationLimit(t *testing.T) {
	var content strings.Builder
	content.WriteString("module example.org/app\ngo 1.24.0\nrequire (\n")
	for i := 0; i < MaxObservationsPerManifest+20; i++ {
		fmt.Fprintf(&content, "example.org/lib%d v1.0.0\n", i)
	}
	content.WriteString(")\n")
	d := ParseGo("go.mod", []byte(content.String()))
	p := d.Project
	if len(p.Requirements)+len(p.References)+len(p.Interfaces) > MaxObservationsPerManifest || len(d.Diagnostics) != 1 {
		t.Fatalf("limit: %+v", d)
	}
}

func FuzzParseGoDeclarations(f *testing.F) {
	f.Add("module example.org/app\ngo 1.24.0\n", false)
	f.Add("go 1.24.0\nuse ./app\n", true)
	f.Fuzz(func(t *testing.T, input string, workspace bool) {
		name := "go.mod"
		if workspace {
			name = "go.work"
		}
		d := ParseGo(name, []byte(input))
		if d == nil || d.Project == nil {
			t.Fatal("recognized path lost")
		}
		ResolveGo([]*Document{d}, map[string]bool{name: true})
		if _, err := json.Marshal(d.Project); err != nil {
			t.Fatal(err)
		}
	})
}

func TestGoWorkspaceBackslashesDoNotInventUnixPaths(t *testing.T) {
	for _, use := range []string{`use .\app`, `use ".\\app"`} {
		workspace := ParseGo("go.work", []byte("go 1.24.0\n"+use+"\n"))
		app := ParseGo("app/go.mod", []byte("module example.org/app\ngo 1.24.0\n"))
		ResolveGo([]*Document{workspace, app}, map[string]bool{"go.work": true, "app/go.mod": true})
		if len(workspace.Diagnostics) == 0 {
			t.Fatal("platform-dependent path was not diagnosed")
		}
		for _, ref := range workspace.Project.References {
			if ref.Target != "" || ref.State != "unresolved" {
				t.Fatalf("backslash path invented a local edge: %+v", ref)
			}
		}
	}
	d := ParseGo("go.mod", []byte("module example.org/app\ngo 1.26.0\nignore .\\generated\n"))
	for _, req := range d.Project.Requirements {
		if req.Kind == "go-ignore-directory" {
			t.Fatalf("backslash ignore path normalized: %+v", req)
		}
	}
	if len(d.Diagnostics) == 0 {
		t.Fatal("backslash ignore path not diagnosed")
	}
}
