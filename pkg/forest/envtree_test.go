package forest_test

import (
	"os"
	"path/filepath"
	"testing"

	"dircue/pkg/forest"
)

// makeRoot creates a temp dir, populates it with the given tree, and returns
// an *os.Root for it. The cleanup func removes the temp dir.
func makeRoot(t *testing.T, tree map[string]string) (*os.Root, func()) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range tree {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if content == "" {
			// Empty directory marker: create parent only.
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", full, err)
			}
		} else {
			if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
				t.Fatalf("write %s: %v", full, err)
			}
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	return root, func() {
		root.Close()
		os.RemoveAll(dir)
	}
}

func TestCheckEnvTree_NodeModules(t *testing.T) {
	root, cleanup := makeRoot(t, map[string]string{
		"node_modules/": "",
	})
	defer cleanup()
	m := forest.CheckEnvTree(root, "node_modules")
	if m == nil {
		t.Fatal("want match for node_modules, got nil")
	}
	if m.Ecosystem != "npm" {
		t.Errorf("ecosystem: want npm, got %q", m.Ecosystem)
	}
	if m.Kind != forest.KindNodeModules {
		t.Errorf("kind: want %q, got %q", forest.KindNodeModules, m.Kind)
	}
	if m.Basis != "filename_hint" {
		t.Errorf("basis: want filename_hint, got %q", m.Basis)
	}
}

func TestCheckEnvTree_PythonVenv(t *testing.T) {
	root, cleanup := makeRoot(t, map[string]string{
		".venv/pyvenv.cfg": "[virtualenv]\n",
	})
	defer cleanup()
	m := forest.CheckEnvTree(root, ".venv")
	if m == nil {
		t.Fatal("want match for .venv (pyvenv.cfg), got nil")
	}
	if m.Ecosystem != "python_venv" {
		t.Errorf("ecosystem: want python_venv, got %q", m.Ecosystem)
	}
	if m.Basis != "rule_inferred" {
		t.Errorf("basis: want rule_inferred, got %q", m.Basis)
	}
}

func TestCheckEnvTree_PycacheName(t *testing.T) {
	root, cleanup := makeRoot(t, map[string]string{
		"src/__pycache__/": "",
	})
	defer cleanup()
	m := forest.CheckEnvTree(root, "src/__pycache__")
	if m == nil {
		t.Fatal("want match for __pycache__, got nil")
	}
	if m.Ecosystem != "python_cache" {
		t.Errorf("ecosystem: want python_cache, got %q", m.Ecosystem)
	}
}

func TestCheckEnvTree_CachedirTag(t *testing.T) {
	root, cleanup := makeRoot(t, map[string]string{
		"target/CACHEDIR.TAG": "Signature: 8a477f597d28d172789f06886806bc55\n# more stuff\n",
	})
	defer cleanup()
	m := forest.CheckEnvTree(root, "target")
	if m == nil {
		t.Fatal("want match for target (CACHEDIR.TAG), got nil")
	}
	if m.Ecosystem != "cachedir_tagged" {
		t.Errorf("ecosystem: want cachedir_tagged, got %q", m.Ecosystem)
	}
}

func TestCheckEnvTree_CachedirTagWrongSignature(t *testing.T) {
	root, cleanup := makeRoot(t, map[string]string{
		"target/CACHEDIR.TAG": "Signature: WRONG\n",
	})
	defer cleanup()
	m := forest.CheckEnvTree(root, "target")
	if m != nil {
		t.Errorf("want nil for wrong CACHEDIR.TAG signature, got %+v", m)
	}
}

func TestCheckEnvTree_GradleDir(t *testing.T) {
	root, cleanup := makeRoot(t, map[string]string{
		"build.gradle":     "apply plugin: 'java'\n",
		".gradle/wrapper/": "",
	})
	defer cleanup()
	m := forest.CheckEnvTree(root, ".gradle")
	if m == nil {
		t.Fatal("want match for .gradle next to build.gradle, got nil")
	}
	if m.Ecosystem != "gradle" {
		t.Errorf("ecosystem: want gradle, got %q", m.Ecosystem)
	}
}

func TestCheckEnvTree_GradleDirNoSibling(t *testing.T) {
	// .gradle without a Gradle build file: must NOT be summarized.
	root, cleanup := makeRoot(t, map[string]string{
		".gradle/wrapper/": "",
	})
	defer cleanup()
	m := forest.CheckEnvTree(root, ".gradle")
	if m != nil {
		t.Errorf("want nil for .gradle without build.gradle sibling, got %+v", m)
	}
}

func TestCheckEnvTree_BuildDirWithGradle(t *testing.T) {
	root, cleanup := makeRoot(t, map[string]string{
		"build.gradle": "apply plugin: 'java'\n",
		"build/libs/":  "",
	})
	defer cleanup()
	m := forest.CheckEnvTree(root, "build")
	if m == nil {
		t.Fatal("want match for build/ next to build.gradle, got nil")
	}
	if m.Ecosystem != "gradle_build" {
		t.Errorf("ecosystem: want gradle_build, got %q", m.Ecosystem)
	}
}

func TestCheckEnvTree_BuildDirNoMarker(t *testing.T) {
	// A source build/ without any Gradle marker must NOT be summarized.
	root, cleanup := makeRoot(t, map[string]string{
		"build/main.c": "int main(){}\n",
	})
	defer cleanup()
	m := forest.CheckEnvTree(root, "build")
	if m != nil {
		t.Errorf("want nil for build/ without Gradle marker, got %+v", m)
	}
}

func TestCheckEnvTree_Terraform(t *testing.T) {
	root, cleanup := makeRoot(t, map[string]string{
		".terraform/providers/": "",
	})
	defer cleanup()
	m := forest.CheckEnvTree(root, ".terraform")
	if m == nil {
		t.Fatal("want match for .terraform, got nil")
	}
	if m.Ecosystem != "terraform" {
		t.Errorf("ecosystem: want terraform, got %q", m.Ecosystem)
	}
}

func TestCheckEnvTree_CocoaPods(t *testing.T) {
	root, cleanup := makeRoot(t, map[string]string{
		"Podfile":        "platform :ios, '14.0'\n",
		"Pods/Manifest/": "",
	})
	defer cleanup()
	m := forest.CheckEnvTree(root, "Pods")
	if m == nil {
		t.Fatal("want match for Pods/ with Podfile, got nil")
	}
	if m.Ecosystem != "cocoapods" {
		t.Errorf("ecosystem: want cocoapods, got %q", m.Ecosystem)
	}
}

func TestCheckEnvTree_CocoaPodsNoSibling(t *testing.T) {
	// Pods/ without Podfile must NOT be summarized.
	root, cleanup := makeRoot(t, map[string]string{
		"Pods/SomePod/": "",
	})
	defer cleanup()
	m := forest.CheckEnvTree(root, "Pods")
	if m != nil {
		t.Errorf("want nil for Pods/ without Podfile, got %+v", m)
	}
}

func TestCheckEnvTree_ToxNoxCacheTools(t *testing.T) {
	for _, name := range []string{".tox", ".nox", ".mypy_cache", ".pytest_cache", ".ruff_cache"} {
		root, cleanup := makeRoot(t, map[string]string{name + "/": ""})
		m := forest.CheckEnvTree(root, name)
		if m == nil {
			t.Errorf("want match for %s, got nil", name)
		}
		cleanup()
	}
}

func TestSummarizeEnvTree_Bounded(t *testing.T) {
	tree := map[string]string{
		"node_modules/a.js": "// a\n",
		"node_modules/b.js": "// b\n",
		"node_modules/c.js": "// c\n",
	}
	root, cleanup := makeRoot(t, tree)
	defer cleanup()
	match := &forest.EnvTreeMatch{
		Path:      "node_modules",
		Kind:      forest.KindNodeModules,
		Ecosystem: "npm",
		Marker:    "node_modules",
		Basis:     "filename_hint",
	}
	summary := forest.SummarizeEnvTree(root, *match, 1_000_000)
	if summary.Entries != 3 {
		t.Errorf("want 3 entries, got %d", summary.Entries)
	}
	if summary.Bytes == 0 {
		t.Error("want nonzero bytes")
	}
	if !summary.Bounded {
		t.Error("want bounded=true for small tree")
	}
}

func TestSummarizeEnvTree_Cap(t *testing.T) {
	tree := map[string]string{
		"node_modules/a.js": "x",
		"node_modules/b.js": "x",
		"node_modules/c.js": "x",
	}
	root, cleanup := makeRoot(t, tree)
	defer cleanup()
	match := &forest.EnvTreeMatch{Path: "node_modules", Kind: forest.KindNodeModules, Ecosystem: "npm", Marker: "node_modules", Basis: "filename_hint"}
	summary := forest.SummarizeEnvTree(root, *match, 2) // cap at 2
	if summary.Bounded {
		t.Error("want bounded=false when cap exceeded")
	}
	if !summary.LowerBound {
		t.Error("want lower_bound=true when cap exceeded")
	}
	if summary.Reason == "" {
		t.Error("want a non-empty reason")
	}
}

// TestSummarizeEnvTree_WalkErrors verifies that walk errors set LowerBound and
// WalkErrors, without setting Bounded to false (which is reserved for the cap).
func TestSummarizeEnvTree_WalkErrors(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read everything; permission test meaningless")
	}
	dir := t.TempDir()
	nm := filepath.Join(dir, "node_modules")
	if err := os.MkdirAll(nm, 0o755); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(nm, "secret_pkg")
	if err := os.MkdirAll(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	// Write a file the test runner cannot read.
	if err := os.WriteFile(filepath.Join(locked, "index.js"), []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0o755)

	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	match := &forest.EnvTreeMatch{Path: "node_modules", Kind: forest.KindNodeModules, Ecosystem: "npm", Marker: "node_modules", Basis: "filename_hint"}
	summary := forest.SummarizeEnvTree(root, *match, 0)
	if summary.WalkErrors == 0 {
		t.Error("want WalkErrors > 0 for unreadable directory")
	}
	if !summary.LowerBound {
		t.Error("want lower_bound=true when walk errors occurred")
	}
	if summary.Reason == "" {
		t.Error("want non-empty reason when walk errors occurred")
	}
}
