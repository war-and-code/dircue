package declarations

import "testing"

func TestParseDartBasic(t *testing.T) {
	content := []byte(`name: myapp
version: 1.0.0
dependencies:
  http: ^1.2.0
  provider: ^6.0.5
dev_dependencies:
  flutter_test:
    sdk: flutter
  mockito: ^5.4.1
`)
	d := ParseDart("pubspec.yaml", content)
	if d == nil || !d.Parsed {
		t.Fatal("parse failed")
	}
	if d.Project.Name != "myapp" {
		t.Errorf("name=%q; want myapp", d.Project.Name)
	}
	// Should have http and provider as runtime dart-dependency requirements.
	reqKinds := map[string]bool{}
	reqValues := map[string]bool{}
	for _, r := range d.Project.Requirements {
		if r.Kind == "dart-dependency" {
			reqValues[r.Value] = true
			if r.Condition != "" {
				reqKinds[r.Condition] = true
			}
		}
	}
	for _, want := range []string{"http", "provider"} {
		if !reqValues[want] {
			t.Errorf("missing runtime dependency %q", want)
		}
	}
	// sdk: flutter pseudo-dependency should not produce a dart-dependency requirement.
	if reqValues["flutter"] {
		t.Errorf("flutter sdk dependency should not produce a dart-dependency requirement")
	}
	// dev deps should have condition "dev_dependencies".
	for _, r := range d.Project.Requirements {
		if r.Kind == "dart-dependency" && r.Value == "mockito" {
			if r.Condition != "dev_dependencies" {
				t.Errorf("mockito condition=%q; want dev_dependencies", r.Condition)
			}
		}
	}
}

func TestParseDartPathDependencyInDependencies(t *testing.T) {
	content := []byte(`name: example
dependencies:
  mylib:
    path: ../mylib
`)
	d := ParseDart("example/pubspec.yaml", content)
	if d == nil || !d.Parsed {
		t.Fatal("parse failed")
	}
	var found bool
	for _, ref := range d.Project.References {
		if ref.Kind == "pub-path-dependency" {
			found = true
			if ref.Target != "mylib/pubspec.yaml" {
				t.Errorf("target=%q; want mylib/pubspec.yaml", ref.Target)
			}
			if ref.Condition != "" {
				t.Errorf("condition=%q; want empty for regular dependencies", ref.Condition)
			}
		}
	}
	if !found {
		t.Errorf("expected pub-path-dependency reference, got references: %+v", d.Project.References)
	}
}

func TestParseDartPathDependencyInDependencyOverrides(t *testing.T) {
	content := []byte(`name: example
dependencies:
  appflowy_editor:
dependency_overrides:
  appflowy_editor:
    path: ../
`)
	d := ParseDart("example/pubspec.yaml", content)
	if d == nil || !d.Parsed {
		t.Fatal("parse failed")
	}
	var found bool
	for _, ref := range d.Project.References {
		if ref.Kind == "pub-path-dependency" && ref.Condition == "dependency_overrides" {
			found = true
			if ref.Target != "pubspec.yaml" {
				t.Errorf("target=%q; want pubspec.yaml", ref.Target)
			}
		}
	}
	if !found {
		t.Errorf("expected pub-path-dependency in dependency_overrides: %+v", d.Project.References)
	}
}

func TestParseDartPathDependencyInDevDependencies(t *testing.T) {
	content := []byte(`name: example
dev_dependencies:
  local_tool:
    path: tools/mytool
`)
	d := ParseDart("pubspec.yaml", content)
	if d == nil || !d.Parsed {
		t.Fatal("parse failed")
	}
	var found bool
	for _, ref := range d.Project.References {
		if ref.Kind == "pub-path-dependency" && ref.Condition == "dev_dependencies" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected pub-path-dependency with dev_dependencies condition: %+v", d.Project.References)
	}
}

func TestParseDartPathOutsideInventoryProducesDiagnostic(t *testing.T) {
	// A path that escapes the scan root (e.g. ../external from the root) must
	// not produce a reference but should produce a diagnostic.
	content := []byte(`name: example
dependencies:
  external:
    path: ../external
`)
	d := ParseDart("pubspec.yaml", content)
	if d == nil || !d.Parsed {
		t.Fatal("parse failed")
	}
	var foundRef, foundDiag bool
	for _, ref := range d.Project.References {
		if ref.Kind == "pub-path-dependency" {
			foundRef = true
		}
	}
	for _, diag := range d.Diagnostics {
		if diag.Code == "external-pub-path-dependency" {
			foundDiag = true
		}
	}
	if foundRef {
		t.Error("out-of-tree path should not produce a reference")
	}
	if !foundDiag {
		t.Error("out-of-tree path should produce external-pub-path-dependency diagnostic")
	}
}

func TestParseDartNoPathDepsProducesNoReferences(t *testing.T) {
	content := []byte(`name: mylib
version: 1.0.0
dependencies:
  http: ^1.2.0
`)
	d := ParseDart("pubspec.yaml", content)
	if d == nil || !d.Parsed {
		t.Fatal("parse failed")
	}
	for _, ref := range d.Project.References {
		if ref.Kind == "pub-path-dependency" {
			t.Errorf("unexpected pub-path-dependency reference: %+v", ref)
		}
	}
}

func TestParseDartSdkDepsAreIgnored(t *testing.T) {
	content := []byte(`name: myapp
dependencies:
  flutter:
    sdk: flutter
  flutter_localizations:
    sdk: flutter
`)
	d := ParseDart("pubspec.yaml", content)
	if d == nil || !d.Parsed {
		t.Fatal("parse failed")
	}
	// sdk: sub-values should not produce path deps or runtime dart-dependency reqs.
	for _, ref := range d.Project.References {
		if ref.Kind == "pub-path-dependency" {
			t.Errorf("sdk: dep produced unexpected pub-path-dependency: %+v", ref)
		}
	}
}

func TestParseDartNestedKeysAreNotPackages(t *testing.T) {
	content := []byte(`name: app
dependencies:
  internal_api:
    hosted: https://pub.example.test
    version: ^1.0.0
  shared:
    git:
      url: https://example.test/shared.git
      path: packages/shared
  local_ui:
    path: ../local_ui # sibling package
`)
	d := ParseDart("app/pubspec.yaml", content)
	values := map[string]bool{}
	for _, r := range d.Project.Requirements {
		if r.Kind == "dart-dependency" {
			values[r.Value] = true
		}
	}
	for _, want := range []string{"internal_api", "shared", "local_ui"} {
		if !values[want] {
			t.Errorf("missing declared dependency %q", want)
		}
	}
	for _, bogus := range []string{"hosted", "version", "git", "url", "path"} {
		if values[bogus] {
			t.Errorf("nested key %q was read as a package", bogus)
		}
	}
	var refs []string
	for _, ref := range d.Project.References {
		refs = append(refs, ref.Value+" -> "+ref.Target)
	}
	if len(refs) != 1 || refs[0] != "local_ui path:../local_ui -> local_ui/pubspec.yaml" {
		t.Errorf("references=%q; want only the local path dependency (not the git subdirectory)", refs)
	}
}
