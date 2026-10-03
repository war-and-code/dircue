package declarations

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func pythonTestInventory(t *testing.T, input map[string]string) ([]*Document, map[string]bool) {
	t.Helper()
	keys := make([]string, 0, len(input))
	for name := range input {
		keys = append(keys, name)
	}
	slices.Sort(keys)
	docs := []*Document{}
	files := map[string]bool{}
	for _, name := range keys {
		files[name] = true
		if d := ParsePython(name, []byte(input[name])); d != nil {
			docs = append(docs, d)
		}
	}
	ResolvePython(docs, files)
	return docs, files
}
func pythonTestDoc(t *testing.T, docs []*Document, id string) *Document {
	t.Helper()
	for _, d := range docs {
		if d.Project.ID == id {
			return d
		}
	}
	t.Fatalf("missing document %q", id)
	return nil
}
func pythonTestRef(t *testing.T, d *Document, kind, value, target, state string) {
	t.Helper()
	for _, r := range d.Project.References {
		if r.Kind == kind && r.Value == value && r.Target == target && r.State == state {
			return
		}
	}
	t.Fatalf("missing ref %s/%s -> %s state %s; have %+v", kind, value, target, state, d.Project.References)
}
func pythonTestDiag(d *Document, code string) bool {
	for _, diag := range d.Diagnostics {
		if diag.Code == code {
			return true
		}
	}
	return false
}
func pythonTestReq(t *testing.T, d *Document, kind, value, state string) {
	t.Helper()
	for _, req := range d.Project.Requirements {
		if req.Kind == kind && req.Value == value && req.State == state {
			return
		}
	}
	t.Fatalf("missing requirement %s=%q state=%s in %+v", kind, value, state, d.Project.Requirements)
}

func TestMaturinBinBindingDeclaresBinaryAtStaticInProjectManifest(t *testing.T) {
	d := ParsePython("python/pyproject.toml", []byte(`[project]
name = "ruff"
version = "1.0"
[dependency-groups]
dev = []
[tool.maturin]
bindings = "bin"
manifest-path = "crates/ruff/Cargo.toml"
`))
	if d == nil || len(d.Project.Interfaces) != 1 {
		t.Fatalf("maturin binary interface missing: %+v", d)
	}
	got := d.Project.Interfaces[0]
	if got.Kind != "binary" || got.Name != "unresolved" || got.Target != "python/crates/ruff/Cargo.toml" || got.State != "unresolved" {
		t.Fatalf("unexpected maturin interface: %+v", got)
	}
	for _, body := range []string{
		"[project]\nname='ruff'\nversion='1'\n[tool.maturin]\nbindings='bin'\nmanifest-path='../../outside/Cargo.toml'\n",
		"[project]\nname='ruff'\nversion='1'\n[tool.maturin]\nbindings='bin'\nmanifest-path='${CARGO_MANIFEST_DIR}/Cargo.toml'\n",
		"[project]\nname='ruff'\nversion='1'\n[tool.maturin]\nbindings='pyo3'\n",
	} {
		doc := ParsePython("python/pyproject.toml", []byte(body))
		if doc != nil {
			for _, iface := range doc.Project.Interfaces {
				if iface.Kind == "binary" {
					t.Errorf("unsupported/out-of-root maturin declaration became a binary: %+v", iface)
				}
			}
		}
	}
}

func TestMaturinBinaryNameComesFromCargoTarget(t *testing.T) {
	python := ParsePython("python/pyproject.toml", []byte(`[project]
name = "my-tool"
version = "1"
[tool.maturin]
bindings = "bin"
manifest-path = "../rust/Cargo.toml"
`))
	cargo := ParseCargo("rust/Cargo.toml", []byte(`[package]
name = "mtcli-package"
version = "1"
[[bin]]
name = "mtcli"
path = "src/main.rs"
`))
	docs := []*Document{python, cargo}
	ResolveCargo(docs, map[string]bool{"rust/Cargo.toml": true, "rust/src/main.rs": true})
	got := python.Project.Interfaces[0]
	if got.Name != "mtcli" || got.State != "declared" {
		t.Fatalf("maturin binary should use Cargo target name, got %+v", got)
	}
}

func TestMaturinBinaryHonorsDeclaredCargoDefaultRun(t *testing.T) {
	python := ParsePython("python/pyproject.toml", []byte(`[project]
name = "my-tool"
version = "1"
[tool.maturin]
bindings = "bin"
manifest-path = "../rust/Cargo.toml"
`))
	cargo := ParseCargo("rust/Cargo.toml", []byte(`[package]
name = "mtcli-package"
version = "1"
default-run = "mtcli"
[[bin]]
name = "mtcli"
path = "src/main.rs"
[[bin]]
name = "helper"
path = "src/helper.rs"
`))
	ResolveCargo([]*Document{python, cargo}, map[string]bool{"rust/Cargo.toml": true, "rust/src/main.rs": true, "rust/src/helper.rs": true})
	got := python.Project.Interfaces[0]
	if got.Name != "mtcli" || got.State != "declared" {
		t.Fatalf("maturin binary should use Cargo's valid default-run target, got %+v", got)
	}
}

func TestMaturinBinaryNameRemainsUnknownWhenCargoTargetsAreAmbiguous(t *testing.T) {
	python := ParsePython("python/pyproject.toml", []byte(`[project]
name = "my-tool"
version = "1"
[tool.maturin]
bindings = "bin"
manifest-path = "../rust/Cargo.toml"
`))
	cargo := ParseCargo("rust/Cargo.toml", []byte(`[package]
name = "mtcli-package"
version = "1"
[[bin]]
name = "mtcli"
path = "src/main.rs"
[[bin]]
name = "other"
path = "src/other.rs"
`))
	ResolveCargo([]*Document{python, cargo}, map[string]bool{"rust/Cargo.toml": true, "rust/src/main.rs": true, "rust/src/other.rs": true})
	got := python.Project.Interfaces[0]
	if got.Name != "unresolved" || got.State != "unresolved" {
		t.Fatalf("ambiguous Cargo targets should not inherit Python distribution name: %+v", got)
	}
}

func TestPythonDeclarationsAndInterfaces(t *testing.T) {
	d := ParsePython("service/pyproject.toml", []byte(`[project]
name = "weather-service"
version = "1.2.3"
requires-python = ">=3.11,<4"
dependencies = ["httpx>=0.28", "typing-extensions; python_version < '3.12'"]
[project.optional-dependencies]
fast = ["orjson>=3"]
[project.scripts]
weather = "weather.cli:main"
[project.gui-scripts]
weather-gui = "weather.ui:launch"
[build-system]
requires = ["hatchling>=1.25"]
build-backend = "hatchling.build"
backend-path = ["backend"]
[dependency-groups]
test = ["pytest", { include-group = "lint" }]
lint = ["ruff"]
`))
	if !d.Parsed || d.Project.Name != "weather-service" || d.Project.Version != "1.2.3" || d.Project.Kind != "python" {
		t.Fatalf("identity: %+v", d)
	}
	if len(d.Diagnostics) > 0 {
		t.Fatalf("diagnostics: %+v", d.Diagnostics)
	}
	pythonTestReq(t, d, "python-requires-python", ">=3.11,<4", "declared")
	pythonTestReq(t, d, "python-dependency", "orjson>=3", "conditional")
	pythonTestReq(t, d, "python-dependency-group-include", "lint", "declared")
	pythonTestReq(t, d, "python-build-requirement", "hatchling>=1.25", "conditional")
	if len(d.Project.Interfaces) != 3 {
		t.Fatalf("interfaces: %+v", d.Project.Interfaces)
	}
	for _, it := range d.Project.Interfaces {
		if it.State != "declared" || it.Target == "" {
			t.Fatalf("interface: %+v", it)
		}
	}
}

func TestPythonWorkspacesAndSourceInheritance(t *testing.T) {
	docs, _ := pythonTestInventory(t, map[string]string{
		"pyproject.toml": `[project]
name="suite"
version="1"
dependencies=["common", "partner"]
[tool.uv.workspace]
members=["packages/*", "services/*"]
exclude=["packages/ignored"]
[tool.uv.sources]
common={workspace=true}
partner={workspace="other"}
`,
		"packages/common/pyproject.toml": `[project]
name="common"
version="1"
`,
		"packages/ignored/pyproject.toml": `[project]
name="ignored"
version="1"
`,
		"services/api/pyproject.toml": `[project]
name="api"
version="1"
dependencies=["common", "partner"]
`,
		"services/worker/pyproject.toml": `[project]
name="worker"
version="1"
dependencies=["common"]
[tool.uv.sources]
common={path="../../overrides/common", marker="sys_platform == 'linux'"}
`,
		"overrides/common/pyproject.toml": `[project]
name="common"
version="2"
`,
		"other/pyproject.toml": `[tool.uv.workspace]
members=["libs/*"]
`,
		"other/libs/partner/pyproject.toml": `[project]
name="partner"
version="1"
`,
		"unrelated/nested/pyproject.toml": `[project]
name="stranger"
version="1"
`,
		"uv.lock":       "version = 1",
		"other/uv.lock": "version = 1",
	})
	root := pythonTestDoc(t, docs, "pyproject.toml")
	if root.Project.Kind != "python-uv" {
		t.Fatalf("explicit uv declaration kind: %+v", root.Project)
	}
	pythonTestRef(t, root, "uv-workspace-member", "declared-member", "pyproject.toml", "resolved")
	pythonTestRef(t, root, "uv-local-dependency", "partner", "other/libs/partner/pyproject.toml", "resolved")
	api := pythonTestDoc(t, docs, "services/api/pyproject.toml")
	pythonTestRef(t, api, "uv-local-dependency", "common", "packages/common/pyproject.toml", "resolved")
	pythonTestRef(t, api, "uv-local-dependency", "partner", "other/libs/partner/pyproject.toml", "resolved")
	pythonTestRef(t, api, "uv-lockfile", "presence-only", "uv.lock", "declared")
	worker := pythonTestDoc(t, docs, "services/worker/pyproject.toml")
	pythonTestRef(t, worker, "uv-local-dependency", "common", "overrides/common/pyproject.toml", "conditional")
	for _, r := range worker.Project.References {
		if r.Kind == "uv-local-dependency" && r.Target == "packages/common/pyproject.toml" {
			t.Fatal("member override fell back to root source")
		}
	}
	for _, r := range root.Project.References {
		if r.Kind == "uv-workspace-member" && (strings.Contains(r.Target, "ignored") || strings.Contains(r.Target, "stranger")) {
			t.Fatalf("unselected member %+v", r)
		}
	}
	other := pythonTestDoc(t, docs, "other/pyproject.toml")
	if other.Project.Kind != "python-workspace" {
		t.Fatal("virtual workspace not distinguished")
	}
}

func TestPythonDependencySourcesAreNotDependencies(t *testing.T) {
	docs, _ := pythonTestInventory(t, map[string]string{
		"pyproject.toml": `[project]
name="app"
version="1"
[tool.uv.sources]
unrequested={path="lib"}
`, "lib/pyproject.toml": `[project]
name="unrequested"
version="1"
`})
	root := pythonTestDoc(t, docs, "pyproject.toml")
	pythonTestRef(t, root, "uv-source-path", "unrequested", "lib/pyproject.toml", "resolved")
	for _, r := range root.Project.References {
		if r.Kind == "uv-local-dependency" {
			t.Fatal("source mapping invented a dependency")
		}
	}
}

func TestPythonDuplicateNamesAndPartialMembership(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			members := `["libs/*"]`
			if missing {
				members = `["libs/*", "absent"]`
			}
			input := map[string]string{"pyproject.toml": `[project]
name="app"
version="1"
dependencies=["my_lib"]
[tool.uv.workspace]
members=` + members + `
[tool.uv.sources]
my_lib={workspace=true}
`, "libs/one/pyproject.toml": `[project]
name="my-lib"
version="1"
`}
			if !missing {
				input["libs/two/pyproject.toml"] = `[project]
name="MY.lib"
version="2"
`
			}
			docs, _ := pythonTestInventory(t, input)
			root := pythonTestDoc(t, docs, "pyproject.toml")
			found := false
			for _, r := range root.Project.References {
				if r.Kind == "uv-local-dependency" {
					found = true
					if r.State != "unresolved" || r.Target != "" {
						t.Fatalf("false resolution %+v", r)
					}
					expected := "ambiguous"
					if missing {
						expected = "unresolved"
					}
					if r.TargetStatus != expected {
						t.Fatalf("target state %+v", r)
					}
				}
			}
			if !found {
				t.Fatal("missing qualified dependency")
			}
		})
	}
}

func TestPythonUnsafeValuesNotExposed(t *testing.T) {
	input := `[project]
name="safe"
version="1"
dependencies=["demo @ https://token:secret@example.test/pkg.whl?token=secret"]
[project.scripts]
run="echo secret && curl example.test"
[tool.uv.sources]
demo={path="/Users/private/secret"}
remote={git="https://token:secret@example.test/repo"}
`
	d := ParsePython("pyproject.toml", []byte(input))
	ResolvePython([]*Document{d}, map[string]bool{"pyproject.toml": true})
	encoded, err := json.Marshal(struct {
		Project     *Project
		Diagnostics []Diagnostic
	}{d.Project, d.Diagnostics})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"secret", "token:", "/Users/private", "echo ", "curl "} {
		if strings.Contains(string(encoded), s) {
			t.Fatalf("leaked %q: %s", s, encoded)
		}
	}
	if len(d.Project.Interfaces) != 1 || d.Project.Interfaces[0].State != "unresolved" {
		t.Fatalf("invalid entrypoint: %+v", d.Project.Interfaces)
	}
}

func TestPythonMalformedAndDynamicMetadata(t *testing.T) {
	bad := ParsePython("pyproject.toml", []byte("[project]\nname = 'one'\nname = 'two'\n"))
	if bad.Parsed || !pythonTestDiag(bad, "invalid-python-manifest") {
		t.Fatalf("duplicate TOML keys accepted: %+v", bad)
	}
	d := ParsePython("pyproject.toml", []byte(`[project]
name="dynamic-project"
dynamic=["version", "dependencies"]
`))
	pythonTestReq(t, d, "python-version", "", "dynamic")
	pythonTestReq(t, d, "python-requires-python", "", "missing")
	pythonTestReq(t, d, "python-dynamic-field", "dependencies", "dynamic")
	if d.Project.Version != "" {
		t.Fatal("invented dynamic version")
	}
}

func TestPythonPatternExclusionAndUnsupported(t *testing.T) {
	docs, _ := pythonTestInventory(t, map[string]string{"pyproject.toml": `[project]
name="root"
version="1"
[tool.uv.workspace]
members=["absent"]
exclude=["absent"]
`})
	root := docs[0]
	for _, r := range root.Project.References {
		if r.Kind == "uv-workspace-member" && r.Target != "pyproject.toml" {
			t.Fatalf("excluded missing member %+v", r)
		}
	}
	docs, _ = pythonTestInventory(t, map[string]string{"pyproject.toml": `[project]
name="root"
version="1"
dependencies=["lib"]
[tool.uv.workspace]
members=["packages/*"]
exclude=["{packages/lib}"]
[tool.uv.sources]
lib={workspace=true}
`, "packages/lib/pyproject.toml": `[project]
name="lib"
version="1"
`})
	root = pythonTestDoc(t, docs, "pyproject.toml")
	if !pythonTestDiag(root, "unsupported-python-workspace-pattern") {
		t.Fatal("unsupported glob silently ignored")
	}
	for _, r := range root.Project.References {
		if r.Kind == "uv-local-dependency" && r.State == "resolved" {
			t.Fatal("unknown exclusion produced resolved link")
		}
	}
}

func TestPythonBadMemberPatternKeepsValidSibling(t *testing.T) {
	docs, _ := pythonTestInventory(t, map[string]string{
		"pyproject.toml": `[tool.uv.workspace]
members=["packages/lib", "../outside"]
`,
		"packages/lib/pyproject.toml": `[project]
name="lib"
version="1"
`,
	})
	root := pythonTestDoc(t, docs, "pyproject.toml")
	found := false
	for _, ref := range root.Project.References {
		found = found || ref.Kind == "uv-workspace-member" && ref.Target == "packages/lib/pyproject.toml"
	}
	if !found || !pythonTestDiag(root, "unsupported-python-workspace-pattern") {
		t.Fatalf("valid uv sibling lost: %+v", root)
	}
}

func TestPythonBounds(t *testing.T) {
	d := ParsePython("pyproject.toml", []byte("value="+strings.Repeat("[", 70)+"0"+strings.Repeat("]", 70)))
	if d.Parsed {
		t.Fatal("deep TOML parsed")
	}
	var b strings.Builder
	b.WriteString("[project]\nname='bulk'\nversion='1'\ndependencies=[")
	for i := 0; i < MaxObservationsPerManifest+100; i++ {
		fmt.Fprintf(&b, "'pkg%d',", i)
	}
	b.WriteString("]")
	d = ParsePython("pyproject.toml", []byte(b.String()))
	if len(d.Project.Requirements) > MaxObservationsPerManifest || len(d.Data.(*pythonData).dependencies) > MaxObservationsPerManifest {
		t.Fatal("unbounded requirements")
	}
	if len(d.Diagnostics) == 0 {
		t.Fatal("missing bound diagnostic")
	}
}

func TestPythonPathSourcesNeedMatchingParsedProject(t *testing.T) {
	for _, target := range []string{`[project]
name="different"
version="1"
`, `[project]
name="lib"
name="duplicate"
`, `[tool.other]
setting=true
`} {
		docs, _ := pythonTestInventory(t, map[string]string{"pyproject.toml": `[project]
name="app"
version="1"
dependencies=["lib"]
[tool.uv.sources]
lib={path="lib"}
`, "lib/pyproject.toml": target})
		root := pythonTestDoc(t, docs, "pyproject.toml")
		for _, r := range root.Project.References {
			if r.Kind == "uv-local-dependency" && r.State == "resolved" {
				t.Fatalf("unverified dependency identity: %+v", r)
			}
		}
	}
}

func TestPythonSourceOptionsAndConditionalScopes(t *testing.T) {
	docs, _ := pythonTestInventory(t, map[string]string{"pyproject.toml": `[project]
name="app"
version="1"
dependencies=["lib"]
[tool.uv.sources]
lib={path="lib", editable=true, package=false}
`, "lib/pyproject.toml": `[project]
name="lib"
version="1"
`})
	root := pythonTestDoc(t, docs, "pyproject.toml")
	pythonTestReq(t, root, "uv-source-editable", "lib=true", "declared")
	pythonTestReq(t, root, "uv-source-package", "lib=false", "declared")
	pythonTestRef(t, root, "uv-local-dependency", "lib", "lib/pyproject.toml", "resolved")
}

func TestPythonMemberInvalidOverrideDoesNotInheritRoot(t *testing.T) {
	docs, _ := pythonTestInventory(t, map[string]string{"pyproject.toml": `[project]
name="app"
version="1"
[tool.uv.workspace]
members=["lib", "api"]
[tool.uv.sources]
lib={workspace=true}
`, "lib/pyproject.toml": `[project]
name="lib"
version="1"
`, "api/pyproject.toml": `[project]
name="api"
version="1"
dependencies=["lib"]
[tool.uv.sources]
lib="not-a-source-table"
`})
	api := pythonTestDoc(t, docs, "api/pyproject.toml")
	for _, r := range api.Project.References {
		if r.Kind == "uv-local-dependency" && r.State == "resolved" {
			t.Fatal("invalid member override incorrectly inherited root")
		}
	}
}

func TestPythonRecursiveGlobAndNestedWorkspace(t *testing.T) {
	docs, _ := pythonTestInventory(t, map[string]string{"pyproject.toml": `[tool.uv.workspace]
members=["packages/**"]
`, "packages/lib/pyproject.toml": `[project]
name="lib"
version="1"
`, "packages/nested/pyproject.toml": `[tool.uv.workspace]
members=["child"]
`, "packages/nested/child/pyproject.toml": `[project]
name="child"
version="1"
`})
	root := pythonTestDoc(t, docs, "pyproject.toml")
	if !pythonTestDiag(root, "nested-python-workspace") {
		t.Fatal("nested workspace not qualified")
	}
	child := pythonTestDoc(t, docs, "packages/nested/child/pyproject.toml")
	if !pythonTestDiag(child, "ambiguous-python-workspace") {
		t.Fatal("overlapping explicit workspace claims not reported")
	}
}

func TestPythonResolutionOrderIndependent(t *testing.T) {
	input := map[string]string{"pyproject.toml": `[project]
name="app"
version="1"
dependencies=["lib"]
[tool.uv.workspace]
members=["lib"]
[tool.uv.sources]
lib={workspace=true}
`, "lib/pyproject.toml": `[project]
name="lib"
version="1"
`}
	docs, files := pythonTestInventory(t, input)
	var expected []byte
	for _, d := range docs {
		b, _ := json.Marshal(d.Project)
		expected = append(expected, b...)
	}
	reversed := []*Document{ParsePython("pyproject.toml", []byte(input["pyproject.toml"])), ParsePython("lib/pyproject.toml", []byte(input["lib/pyproject.toml"]))}
	ResolvePython(reversed, files)
	slices.SortFunc(reversed, func(a, b *Document) int { return strings.Compare(a.Project.ID, b.Project.ID) })
	var actual []byte
	for _, d := range reversed {
		b, _ := json.Marshal(d.Project)
		actual = append(actual, b...)
	}
	if string(actual) != string(expected) {
		t.Fatalf("input order changed observations:\n%s\n%s", actual, expected)
	}
}

func FuzzPythonDeclarations(f *testing.F) {
	f.Add([]byte("[project]\nname='app'\nversion='1'\n"))
	f.Add([]byte("[tool.uv.workspace]\nmembers=['packages/*']\n"))
	f.Add([]byte("[tool.uv.sources]\nlib={workspace=true}\n"))
	f.Fuzz(func(t *testing.T, content []byte) {
		d := ParsePython("pyproject.toml", content)
		ResolvePython([]*Document{d}, map[string]bool{"pyproject.toml": true})
		count := len(d.Project.Requirements) + len(d.Project.References) + len(d.Project.Interfaces)
		if count > MaxObservationsPerManifest || len(d.Diagnostics) > 32 {
			t.Fatal("observation limits exceeded")
		}
		if _, err := json.Marshal(d.Project); err != nil {
			t.Fatal(err)
		}
	})
}

func BenchmarkPythonWorkspace(b *testing.B) {
	input := []byte(`[project]
name="app"
version="1"
dependencies=["lib"]
[tool.uv.workspace]
members=["packages/*"]
[tool.uv.sources]
lib={workspace=true}
`)
	b.ReportAllocs()
	for b.Loop() {
		root := ParsePython("pyproject.toml", input)
		lib := ParsePython("packages/lib/pyproject.toml", []byte("[project]\nname='lib'\nversion='1'\n"))
		ResolvePython([]*Document{root, lib}, map[string]bool{"pyproject.toml": true, "packages/lib/pyproject.toml": true, "uv.lock": true})
	}
}

func TestPythonWorkspaceBudgetCountsOutsideCandidates(t *testing.T) {
	input := make(map[string]string, 1600)
	for i := 0; i < 800; i++ {
		input[fmt.Sprintf("workspace%04d/pyproject.toml", i)] = "[tool.uv.workspace]\nmembers=['absent']\n"
		input[fmt.Sprintf("unrelated%04d/file.txt", i)] = "text"
	}
	docs, _ := pythonTestInventory(t, input)
	limited := false
	for _, d := range docs {
		if pythonTestDiag(d, "python-workspace-match-limit") {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("workspace candidate budget did not bound unrelated directory checks")
	}
}

func TestPythonArchiveAndBackslashSourcesAreQualified(t *testing.T) {
	for _, source := range []string{"dist/library-1.0-py3-none-any.whl", "dist/library-1.0.tar.gz", "dist/library.zip", `libraries\lib`} {
		manifest := fmt.Sprintf("[project]\nname='app'\nversion='1'\ndependencies=['lib']\n[tool.uv.sources]\nlib={path='%s'}\n", source)
		docs, _ := pythonTestInventory(t, map[string]string{"pyproject.toml": manifest, "libraries/lib/pyproject.toml": "[project]\nname='lib'\nversion='1'\n", source: "archive"})
		root := pythonTestDoc(t, docs, "pyproject.toml")
		for _, r := range root.Project.References {
			if r.Kind == "uv-local-dependency" && (r.Target != "" || r.State != "unresolved") {
				t.Fatalf("fabricated project target for %q: %+v", source, r)
			}
		}
		if pythonArchiveSource(source) && !pythonTestDiag(root, "unsupported-uv-archive-source") {
			t.Fatalf("archive limitation missing for %q", source)
		}
	}
}

func TestPythonUnmanagedProjectsHaveNoUVRelationships(t *testing.T) {
	for _, rootUnmanaged := range []bool{false, true} {
		t.Run(fmt.Sprint(rootUnmanaged), func(t *testing.T) {
			rootManaged := "true"
			if rootUnmanaged {
				rootManaged = "false"
			}
			docs, _ := pythonTestInventory(t, map[string]string{"pyproject.toml": `[project]
name="app"
version="1"
dependencies=["lib"]
[tool.uv]
managed=` + rootManaged + `
[tool.uv.workspace]
members=["lib", "api"]
[tool.uv.sources]
lib={workspace=true}
`, "lib/pyproject.toml": `[project]
name="lib"
version="1"
`, "api/pyproject.toml": `[project]
name="api"
version="1"
dependencies=["lib"]
[tool.uv]
managed=false
`})
			root := pythonTestDoc(t, docs, "pyproject.toml")
			api := pythonTestDoc(t, docs, "api/pyproject.toml")
			pythonTestReq(t, api, "uv-managed", "false", "declared")
			if api.Project.Name != "api" {
				t.Fatal("unmanaged project identity lost")
			}
			for _, r := range api.Project.References {
				if strings.HasPrefix(r.Kind, "uv-") {
					t.Fatalf("unmanaged project gained relationship: %+v", r)
				}
			}
			for _, r := range root.Project.References {
				if r.Kind == "uv-workspace-member" && (rootUnmanaged || r.Target == "api/pyproject.toml") {
					t.Fatalf("unmanaged workspace member claimed: %+v", r)
				}
			}
		})
	}
}

func TestPythonStaticDynamicConflictsDoNotDeriveInterfacesOrEdges(t *testing.T) {
	docs, _ := pythonTestInventory(t, map[string]string{"pyproject.toml": `[project]
name="app"
version="1"
dynamic=["dependencies", "optional-dependencies", "scripts", "gui-scripts"]
dependencies=["lib"]
[project.optional-dependencies]
fast=["lib"]
[project.scripts]
app="app:main"
[project.gui-scripts]
app-gui="app:main"
[tool.uv.sources]
lib={path="lib"}
`, "lib/pyproject.toml": `[project]
name="lib"
version="1"
`})
	root := pythonTestDoc(t, docs, "pyproject.toml")
	if !pythonTestDiag(root, "conflicting-python-metadata") {
		t.Fatal("conflicting fields undiagnosed")
	}
	for _, kind := range []string{"dependencies", "optional-dependencies", "scripts", "gui-scripts"} {
		pythonTestReq(t, root, "python-dynamic-conflict", kind, "unresolved")
	}
	for _, r := range root.Project.References {
		if r.Kind == "uv-local-dependency" {
			t.Fatalf("conflicting field invented edge: %+v", r)
		}
	}
	for _, i := range root.Project.Interfaces {
		if i.State != "unresolved" || i.Target != "" {
			t.Fatalf("conflicting script resolved: %+v", i)
		}
	}
}

func TestPythonUnnamedMembersAndProjectBackendBoundary(t *testing.T) {
	docs, _ := pythonTestInventory(t, map[string]string{"pyproject.toml": `[project]
name="app"
version="1"
dependencies=["lib"]
[tool.uv.workspace]
members=["lib"]
[tool.uv.sources]
lib={workspace=true}
`, "lib/pyproject.toml": `[project]
version="1"
[build-system]
requires=[]
build-backend="build:backend"
backend-path=["../sibling"]
`, "sibling/backend.py": "pass"})
	root := pythonTestDoc(t, docs, "pyproject.toml")
	lib := pythonTestDoc(t, docs, "lib/pyproject.toml")
	if !pythonTestDiag(lib, "missing-python-name") || !pythonTestDiag(lib, "external-python-backend") {
		t.Fatalf("missing required diagnostics: %+v", lib.Diagnostics)
	}
	for _, r := range root.Project.References {
		if r.Kind == "uv-local-dependency" && (r.State != "unresolved" || r.TargetStatus == "missing") {
			t.Fatalf("unknown member identity became definite: %+v", r)
		}
	}
	for _, r := range lib.Project.References {
		if r.Kind == "python-backend-path" && (r.Target != "" || r.State != "unresolved") {
			t.Fatalf("backend escaped project root: %+v", r)
		}
	}
}

func TestPythonPinnedUVOracleFixtures(t *testing.T) {
	const fixtureRoot = "testdata/python/uv-0.12.17"
	entries, err := os.ReadDir(fixtureRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			root := filepath.Join(fixtureRoot, entry.Name())
			expectedBytes, err := os.ReadFile(filepath.Join(root, "expected.json"))
			if err != nil {
				t.Fatal(err)
			}
			var expected struct {
				RootManifest      string     `json:"root_manifest"`
				Members           []string   `json:"members"`
				LocalDependencies [][]string `json:"local_dependencies"`
			}
			if err := json.Unmarshal(expectedBytes, &expected); err != nil {
				t.Fatal(err)
			}
			input := map[string]string{}
			err = filepath.WalkDir(root, func(filename string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() || entry.Name() != "pyproject.toml" {
					return nil
				}
				content, err := os.ReadFile(filename)
				if err != nil {
					return err
				}
				relative, err := filepath.Rel(root, filename)
				if err != nil {
					return err
				}
				input[filepath.ToSlash(relative)] = string(content)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			docs, _ := pythonTestInventory(t, input)
			byID := map[string]*Document{}
			for _, d := range docs {
				byID[d.Project.ID] = d
			}
			project := pythonTestDoc(t, docs, expected.RootManifest)
			members := []string{}
			hasWorkspace := false
			for _, req := range project.Project.Requirements {
				if req.Kind == "uv-workspace" {
					hasWorkspace = true
				}
			}
			if !hasWorkspace {
				members = append(members, project.Project.Name)
			}
			for _, ref := range project.Project.References {
				if ref.Kind == "uv-workspace-member" && ref.State == "resolved" {
					members = append(members, byID[ref.Target].Project.Name)
				}
			}
			slices.Sort(members)
			if !slices.Equal(members, expected.Members) {
				t.Fatalf("oracle members: got %v want %v", members, expected.Members)
			}
			edges := []string{}
			for _, d := range docs {
				for _, ref := range d.Project.References {
					if ref.Kind == "uv-local-dependency" && ref.State == "resolved" {
						edges = append(edges, d.Project.Name+" -> "+byID[ref.Target].Project.Name)
					}
				}
			}
			slices.Sort(edges)
			wantEdges := []string{}
			for _, edge := range expected.LocalDependencies {
				wantEdges = append(wantEdges, edge[0]+" -> "+edge[1])
			}
			slices.Sort(wantEdges)
			if !slices.Equal(edges, wantEdges) {
				t.Fatalf("oracle dependency pairs: got %v want %v", edges, wantEdges)
			}
		})
	}
}

func TestPythonRequirementsOnlyRootIsRetained(t *testing.T) {
	// A directory with only requirements.txt (no .py source files, no pyproject.toml)
	// should be kept as a Python component — it represents a service whose dependency
	// set is declared here even though the code lives elsewhere.
	docs, _ := pythonTestInventory(t, map[string]string{
		"requirements.txt": "flask==3.0.0\nrequests>=2.31\n",
	})
	if len(docs) == 0 {
		t.Fatal("requirements-only root should produce a Python component")
	}
	if docs[0].Project == nil {
		t.Fatal("requirements-only root component should not be dropped")
	}
}

func TestPythonRequirementsOnlySubdirIsRetained(t *testing.T) {
	// Same scenario but in a subdirectory.
	docs, _ := pythonTestInventory(t, map[string]string{
		"services/api/requirements.txt": "fastapi==0.110.0\nuvicorn>=0.27\n",
	})
	if len(docs) == 0 {
		t.Fatal("subdirectory requirements-only component should be retained")
	}
	found := false
	for _, d := range docs {
		if d.Project != nil && strings.Contains(d.Project.Root, "api") {
			found = true
		}
	}
	if !found {
		t.Fatal("requirements-only subdirectory component was dropped")
	}
}

func TestPythonRequirementsWithSetupPyDropsAuxiliary(t *testing.T) {
	// When setup.py is also present, the auxiliary requirements.txt is superseded.
	docs, _ := pythonTestInventory(t, map[string]string{
		"requirements.txt": "flask==3.0.0\n",
		"setup.py":         "from setuptools import setup\nsetup(name='myapp')\n",
	})
	// There may be a setup.py doc but the requirements.txt auxiliary should be absent.
	for _, d := range docs {
		if d.Project != nil {
			data, ok := d.Data.(*pythonData)
			if ok && data.auxiliary && strings.HasSuffix(d.Project.ID, "requirements.txt") {
				t.Fatal("requirements.txt auxiliary should be dropped when setup.py is present")
			}
		}
	}
}

func TestPythonReqsOnlyRootUnit(t *testing.T) {
	// Unit test for pythonReqsOnlyRoot helper directly.
	for _, tc := range []struct {
		name   string
		files  map[string]bool
		root   string
		expect bool
	}{
		{
			name:   "requirements.txt at repo root",
			files:  map[string]bool{"requirements.txt": true, "Dockerfile": true},
			root:   ".",
			expect: true,
		},
		{
			name:   "requirements.txt in subdir",
			files:  map[string]bool{"svc/api/requirements.txt": true},
			root:   "svc/api",
			expect: true,
		},
		{
			name:   "setup.py blocks requirements.txt",
			files:  map[string]bool{"requirements.txt": true, "setup.py": true},
			root:   ".",
			expect: false,
		},
		{
			name:   "no requirements file",
			files:  map[string]bool{"main.py": true},
			root:   ".",
			expect: false,
		},
		{
			name:   "requirements in subdirectory not at root",
			files:  map[string]bool{"sub/requirements.txt": true},
			root:   ".",
			expect: false,
		},
		{
			name:   "requirements-dev.txt counts",
			files:  map[string]bool{"requirements-dev.txt": true},
			root:   ".",
			expect: true,
		},
		{
			name:   "Pipfile counts",
			files:  map[string]bool{"Pipfile": true},
			root:   ".",
			expect: true,
		},
		{
			name:   "Pipfile in subdir counts",
			files:  map[string]bool{"services/api/Pipfile": true},
			root:   "services/api",
			expect: true,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := pythonReqsOnlyRoot(tc.files, tc.root)
			if got != tc.expect {
				t.Fatalf("pythonReqsOnlyRoot(%v, %q) = %v, want %v", tc.files, tc.root, got, tc.expect)
			}
		})
	}
}

// pythonTestInventoryAll routes files through the correct Python parsers
// (ParsePython for requirements/pyproject/setup files, ParsePipfile for Pipfile,
// ParsePythonSetupCfg for setup.cfg) and calls ResolvePython.
func pythonTestInventoryAll(t *testing.T, input map[string]string) ([]*Document, map[string]bool) {
	t.Helper()
	keys := make([]string, 0, len(input))
	for name := range input {
		keys = append(keys, name)
	}
	slices.Sort(keys)
	docs := []*Document{}
	files := map[string]bool{}
	for _, name := range keys {
		files[name] = true
		var d *Document
		switch path.Base(name) {
		case "Pipfile":
			d = ParsePipfile(name, []byte(input[name]))
		case "setup.cfg":
			d = ParsePythonSetupCfg(name, []byte(input[name]))
		default:
			d = ParsePython(name, []byte(input[name]))
		}
		if d != nil {
			docs = append(docs, d)
		}
	}
	ResolvePython(docs, files)
	return docs, files
}

// TestPythonRequirementsInclude verifies that -r includes are followed:
// requirements from the included file are merged into the including file's
// component with their original evidence paths preserved.
func TestPythonRequirementsInclude(t *testing.T) {
	docs, _ := pythonTestInventoryAll(t, map[string]string{
		"requirements.txt":      "-r requirements/prod.txt\n",
		"requirements/prod.txt": "psycopg2==2.9.9\nFlask==3.0.0\n",
		"app.py":                "import flask\n",
	})
	// One component at root (not from requirements/prod.txt which is includeOnly).
	var root *Document
	for _, d := range docs {
		if d.Project != nil && d.Project.Root == "." {
			root = d
			break
		}
	}
	if root == nil {
		t.Fatal("no component at root; expected requirements.txt to anchor one")
	}
	// psycopg2 should have been merged with evidence pointing to the included file.
	found := false
	for _, req := range root.Project.Requirements {
		if req.Kind == "python-dependency" && req.Value == "psycopg2==2.9.9" && req.Evidence == "requirements/prod.txt" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("psycopg2 from included file not merged; requirements: %+v", root.Project.Requirements)
	}
	// The included file must NOT become a standalone component.
	for _, d := range docs {
		if d.Project != nil && d.Project.ID == "requirements/prod.txt" {
			t.Fatal("includeOnly file requirements/prod.txt became a standalone component")
		}
	}
}

// TestPipfileRuntimeAndDevDeps verifies that Pipfile [packages] produce
// declared deps and [dev-packages] produce dev_dependencies-conditioned deps.
func TestPipfileRuntimeAndDevDeps(t *testing.T) {
	docs, _ := pythonTestInventoryAll(t, map[string]string{
		"Pipfile": `[[source]]
url = "https://pypi.org/simple"
name = "pypi"

[packages]
Flask = "*"
psycopg2 = "*"

[dev-packages]
pytest = "*"
factory-boy = "*"

[requires]
python_version = "3.11"
`,
		"app.py": "import flask\n",
	})
	if len(docs) != 1 {
		t.Fatalf("expected 1 component, got %d", len(docs))
	}
	root := docs[0]
	// Runtime deps: no condition.
	pythonTestReq(t, root, "python-dependency", "Flask", "declared")
	pythonTestReq(t, root, "python-dependency", "psycopg2", "declared")
	// Dev deps: condition must be dev_dependencies.
	found := false
	for _, req := range root.Project.Requirements {
		if req.Kind == "python-dependency" && req.Value == "pytest" && req.Condition == "dev_dependencies" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("pytest dev dep missing or wrong condition; reqs: %+v", root.Project.Requirements)
	}
	// python_version from [requires]
	pythonTestReq(t, root, "python-requires-python", "3.11", "declared")
}

// TestSetupCfgToolOnlyReturnsNil verifies that a setup.cfg with only tool
// sections ([flake8], [mypy], etc.) returns nil and causes no component.
func TestSetupCfgToolOnlyReturnsNil(t *testing.T) {
	content := []byte("[flake8]\nmax-line-length = 120\n[mypy]\npython_version = 3.11\n")
	d := ParsePythonSetupCfg("setup.cfg", content)
	if d != nil {
		t.Fatalf("tool-only setup.cfg should return nil, got %+v", d)
	}
	// Confirm no component is created when combined with other files.
	docs, _ := pythonTestInventoryAll(t, map[string]string{
		"setup.cfg":        string(content),
		"requirements.txt": "flask==3.0.0\n",
		"app.py":           "import flask\n",
	})
	for _, d := range docs {
		if d.Project != nil {
			for _, req := range d.Project.Requirements {
				if req.Evidence == "setup.cfg" && req.Kind == "declaration-semantics" {
					t.Fatalf("tool-only setup.cfg contributed to a component: %+v", d.Project)
				}
			}
		}
	}
}

// TestSetupCfgWithMetadataCreatesComponent verifies that a setup.cfg with
// a [metadata] section still produces a Python component.
func TestSetupCfgWithMetadataCreatesComponent(t *testing.T) {
	d := ParsePythonSetupCfg("setup.cfg", []byte("[metadata]\nname = my-package\nversion = 1.0.0\n"))
	if d == nil {
		t.Fatal("setup.cfg with [metadata] should not return nil")
	}
	if d.Project == nil || d.Project.Name != "my-package" {
		t.Fatalf("expected name=my-package, got: %+v", d)
	}
}

// TestPipfileAndRequirementsDedup verifies that when both a Pipfile and a
// requirements.txt exist at the same root, only one component is produced
// (Pipfile wins the dedup; it has higher-priority auxiliary evidence).
func TestPipfileAndRequirementsDedup(t *testing.T) {
	docs, _ := pythonTestInventoryAll(t, map[string]string{
		"Pipfile": `[packages]
Flask = "*"
`,
		"requirements.txt": "Flask==3.0.0\n",
		"app.py":           "import flask\n",
	})
	// Count components with non-nil Project.
	var components []*Document
	for _, d := range docs {
		if d.Project != nil {
			components = append(components, d)
		}
	}
	if len(components) != 1 {
		t.Fatalf("expected 1 component after dedup, got %d: %+v", len(components), components)
	}
	// Pipfile should win (it has lower auxiliary priority value).
	if path.Base(components[0].Project.ID) != "Pipfile" {
		t.Fatalf("expected Pipfile to win dedup, got %s", components[0].Project.ID)
	}
}

// TestPyprojectToolOnly verifies that a pyproject.toml containing only
// tool configuration and/or dev dependency groups does not produce a
// Python component (#fix-2). Analogous to ParsePythonSetupCfg returning nil
// for a setup.cfg with only [flake8]/[mypy] sections.
func TestPyprojectToolOnly(t *testing.T) {
	// --- Cases that must return nil (tool-only) ---
	toolOnlyCases := []struct {
		name    string
		content string
	}{
		{
			name: "dev-groups-only (gitea-like Go repo)",
			content: `[project]
name = "gitea"

[dependency-groups]
dev = ["djlint", "yamllint", "zizmor"]

[tool.djlint]
profile = "jinja"
`,
		},
		{
			name: "tool-sections-only (no project table)",
			content: `[tool.ruff]
line-length = 88

[dependency-groups]
dev = ["ruff>=0.3"]
`,
		},
		{
			name: "empty-dependencies-list (no runtime deps)",
			content: `[project]
name = "my-tool-config"

[dependency-groups]
lint = ["flake8"]
`,
		},
	}
	for _, tc := range toolOnlyCases {
		t.Run(tc.name, func(t *testing.T) {
			d := ParsePython("pyproject.toml", []byte(tc.content))
			if d != nil {
				t.Errorf("expected nil (tool-only), got component: kind=%s name=%s",
					d.Project.Kind, d.Project.Name)
			}
		})
	}

	// --- Cases that must NOT return nil (genuine Python projects) ---
	genuineCases := []struct {
		name    string
		content string
	}{
		{
			name: "with build-system",
			content: `[build-system]
requires = ["setuptools"]
build-backend = "setuptools.build_meta"

[project]
name = "my-pkg"
`,
		},
		{
			name: "with runtime dependencies",
			content: `[project]
name = "my-app"
dependencies = ["flask>=2.0"]
`,
		},
		{
			name: "with scripts",
			content: `[project]
name = "my-cli"

[project.scripts]
my-cli = "my_cli:main"
`,
		},
		{
			name: "uv workspace",
			content: `[tool.uv.workspace]
members = ["packages/*"]
`,
		},
		{
			name: "with optional-dependencies",
			content: `[project]
name = "my-lib"

[project.optional-dependencies]
dev = ["pytest"]
`,
		},
	}
	for _, tc := range genuineCases {
		t.Run(tc.name, func(t *testing.T) {
			d := ParsePython("pyproject.toml", []byte(tc.content))
			if d == nil {
				t.Errorf("expected non-nil (genuine Python project), got nil")
			}
		})
	}
}
