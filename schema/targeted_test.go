package schema_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/war-and-code/dircue/internal/cli"
	"github.com/war-and-code/dircue/pkg/reportdiff"
	"github.com/war-and-code/dircue/schema"

	git "github.com/war-and-code/dircue/third_party/go-git"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing/object"
)

func targetedCLIReport(t *testing.T, args ...string) map[string]any {
	t.Helper()
	var out, stderr bytes.Buffer
	if err := cli.Execute(context.Background(), args, &out, &stderr); err != nil {
		t.Fatalf("%v: %v (%s)", args, err, stderr.String())
	}
	var v map[string]any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if err := schema.ValidateProfile(v); err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return v
}

func TestTargetedSchemaAndComparisonBoundary(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{"app.csproj": "<Project Sdk=\"Microsoft.NET.Sdk\"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>", "App.cs": "class App {}\n", "blob.dat": "version https://git-lfs.github.com/spec/v1\noid sha256:" + strings.Repeat("a", 64) + "\nsize 999999\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"analyze", "focus", "--project", "app.csproj", "--metrics", "--files", "--json", "--source", "directory", root},
		{"analyze", "focus", "--affected-by", "app.csproj", "--json", "--source", "directory", root},
		{"analyze", "availability", "--json", "--source", "directory", root},
		{"analyze", "all", "--availability", "--formats", "--declarations", "--json", "--source", "directory", root},
		{"analyze", "explain", "--file", "App.cs", "--json", "--source", "directory", root},
	} {
		t.Run(strings.Join(args[1:4], "-"), func(t *testing.T) {
			v := targetedCLIReport(t, args...)
			if v["schema_version"] != "1.6.0" {
				t.Fatal("new report did not declare schema 1.6")
			}
			data, _ := json.Marshal(v)
			snapshot, err := reportdiff.Load(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("comparison rejected supported scope: %v", err)
			}
			compared, err := reportdiff.Compare(snapshot, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if len(compared.Modules) == 0 {
				t.Fatal("comparison omitted module qualifications")
			}
			find := func(name string) *reportdiff.Module {
				for i := range compared.Modules {
					if compared.Modules[i].Name == name {
						return &compared.Modules[i]
					}
				}
				return nil
			}
			switch {
			case v["focus"] != nil:
				if find("focus_context") == nil {
					t.Fatal("focus population modules omitted")
				}
				if m := find("languages"); m == nil || m.Compatibility != "incomparable" {
					t.Fatalf("focused report accidentally compared aggregate languages: %#v", m)
				}
				focusBody := v["focus"].(map[string]any)
				if focusBody["scope"].(map[string]any)["role"] == "affected-by" && find("focus_affected_projects") == nil {
					t.Fatal("affected-by query results omitted from comparison")
				}
			case v["availability"] != nil:
				if find("availability_lfs") == nil || find("availability_sparse") == nil {
					t.Fatal("availability acquisition populations omitted")
				}
			case v["explanation"] != nil:
				if m := find("explanation"); m == nil || m.Compatibility != "incomparable" {
					t.Fatalf("explanation was not explicitly unsupported: %#v", m)
				}
			}
			if _, digest, err := reportdiff.ReadEvidence(bytes.NewReader(data)); err != nil || len(digest) != 64 {
				t.Fatalf("retained evidence load: %s %v", digest, err)
			}
			for _, old := range []string{"1.0.0", "1.1.0", "1.2.0", "1.3.0", "1.4.0", "1.5.0"} {
				v["schema_version"] = old
				if schema.ValidateProfile(v) == nil {
					t.Fatalf("targeted fields accepted as %s", old)
				}
			}
		})
	}
}

func TestFocusSchemaRejectsMixedDenominatorsAndUnknownFields(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte("[project]\nname='app'\nversion='1'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	v := targetedCLIReport(t, "analyze", "focus", "--project", "pyproject.toml", "--metrics", "--json", root)
	focused := v["focused_metrics"].(map[string]any)
	v["metrics"] = focused["primary"]
	if schema.ValidateProfile(v) == nil {
		t.Fatal("focused and repository-wide metrics accepted together")
	}
	delete(v, "metrics")
	v["focus"].(map[string]any)["compiler_verified"] = true
	if schema.ValidateProfile(v) == nil {
		t.Fatal("undeclared assertion accepted")
	}
}

func TestRetainedFocusMetricsRequireMatchingPopulationAndSource(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte("[project]\nname='app'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	original := targetedCLIReport(t, "analyze", "focus", "--project", "pyproject.toml", "--metrics", "--json", root)
	raw, _ := json.Marshal(original)
	for _, mutation := range []string{"scope", "source", "related"} {
		t.Run(mutation, func(t *testing.T) {
			var value map[string]any
			json.Unmarshal(raw, &value)
			m := value["focused_metrics"].(map[string]any)
			switch mutation {
			case "scope":
				m["scope_id"] = strings.Repeat("b", 64)
			case "source":
				primary := m["primary"].(map[string]any)
				primary["source"] = "git"
				primary["tree"] = strings.Repeat("a", 40)
			case "related":
				m["related"] = []any{map[string]any{"project": "not-selected/pyproject.toml", "metrics": m["primary"]}}
			}
			data, _ := json.Marshal(value)
			if _, _, err := reportdiff.ReadEvidence(bytes.NewReader(data)); err == nil {
				t.Fatal("accepted conflicting metric population")
			}
		})
	}
}

func TestComparisonRejectsForgedFocusScopeIdentity(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte("[project]\nname='app'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	value := targetedCLIReport(t, "analyze", "focus", "--project", "pyproject.toml", "--json", root)
	value["focus"].(map[string]any)["scope"].(map[string]any)["id"] = strings.Repeat("b", 64)
	data, _ := json.Marshal(value)
	if _, err := reportdiff.Load(bytes.NewReader(data)); err == nil {
		t.Fatal("accepted forged focus scope identity")
	}
}

func TestComparisonTreatsChangedFocusSelectionAsScopeChange(t *testing.T) {
	root := t.TempDir()
	for name := range map[string]bool{"App.csproj": true, "Lib.csproj": true} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(`<Project Sdk="Microsoft.NET.Sdk"/>`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	base := targetedCLIReport(t, "analyze", "focus", "--project", "App.csproj", "--json", root)
	head := targetedCLIReport(t, "analyze", "focus", "--project", "App.csproj", "--related-project", "Lib.csproj", "--json", root)
	load := func(v map[string]any) *reportdiff.Snapshot {
		data, _ := json.Marshal(v)
		s, err := reportdiff.Load(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	r, err := reportdiff.Compare(load(base), load(head))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range r.Modules {
		if m.Name == "focus_related" {
			if m.Status != "incomparable" || m.Counts.Removed != 0 || m.Counts.Added != 0 {
				t.Fatalf("changed selection was rendered as membership removal/addition: %#v", m)
			}
			return
		}
	}
	t.Fatal("focus related population omitted")
}

func TestAvailabilityPartialCoverageDoesNotInferRemoval(t *testing.T) {
	root := t.TempDir()
	pointer := "version https://git-lfs.github.com/spec/v1\noid sha256:" + strings.Repeat("a", 64) + "\nsize 42\n"
	if err := os.WriteFile(filepath.Join(root, "asset.bin"), []byte(pointer), 0600); err != nil {
		t.Fatal(err)
	}
	base := targetedCLIReport(t, "analyze", "availability", "--json", "--source", "directory", root)
	raw, _ := json.Marshal(base)
	var head map[string]any
	json.Unmarshal(raw, &head)
	a := head["availability"].(map[string]any)
	a["status"] = "partial"
	a["lfs"] = []any{}
	c := a["coverage"].(map[string]any)
	c["selected_inventory_complete"] = false
	c["omitted_pointer_files"] = float64(1)
	a["omissions"] = map[string]any{"pointer_files": float64(1)}
	load := func(v map[string]any) *reportdiff.Snapshot {
		data, _ := json.Marshal(v)
		s, err := reportdiff.Load(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	r, err := reportdiff.Compare(load(base), load(head))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range r.Modules {
		if m.Name == "availability_lfs" {
			if m.Counts.Removed != 0 || m.Counts.Unavailable == 0 || m.Compatibility != "observed_only" {
				t.Fatalf("partial acquisition evidence inferred removal: %#v", m)
			}
			return
		}
	}
	t.Fatal("availability lfs module omitted")
}

func TestTargetedComparisonContractsFromCLIReports(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "App.csproj"), []byte(`<Project Sdk="Microsoft.NET.Sdk"/>`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "App.cs"), []byte("class App {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	load := func(v map[string]any) *reportdiff.Snapshot {
		data, _ := json.Marshal(v)
		s, err := reportdiff.Load(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	module := func(r *reportdiff.Report, name string) reportdiff.Module {
		for _, m := range r.Modules {
			if m.Name == name {
				return m
			}
		}
		t.Fatalf("module %s omitted", name)
		return reportdiff.Module{}
	}

	metricsA := targetedCLIReport(t, "analyze", "focus", "--project", "App.csproj", "--metrics", "--json", "--metrics-max-file-bytes", "1024", root)
	metricsB := targetedCLIReport(t, "analyze", "focus", "--project", "App.csproj", "--metrics", "--json", "--metrics-max-file-bytes", "2048", root)
	compared, err := reportdiff.Compare(load(metricsA), load(metricsB))
	if err != nil {
		t.Fatal(err)
	}
	if got := module(compared, "focused_metrics_primary"); got.Compatibility != "incomparable" {
		t.Fatalf("different focused metrics contracts compared: %#v", got)
	}
	partialRaw, _ := json.Marshal(metricsA)
	var partial map[string]any
	json.Unmarshal(partialRaw, &partial)
	focusBody := partial["focus"].(map[string]any)
	focusBody["status"] = "partial"
	focusBody["coverage"].(map[string]any)["omitted_files"] = float64(1)
	focusBody["omissions"] = []any{map[string]any{"reason": "inventory_limit", "count": float64(1)}}
	compared, err = reportdiff.Compare(load(partial), load(partial))
	if err != nil {
		t.Fatal(err)
	}
	if got := module(compared, "focused_metrics_primary"); got.Compatibility != "observed_only" {
		t.Fatalf("complete child metrics escaped partial focus coverage: %#v", got)
	}

	standalone := targetedCLIReport(t, "analyze", "availability", "--json", "--source", "directory", root)
	withDeclarations := targetedCLIReport(t, "analyze", "all", "--availability", "--declarations", "--json", "--source", "directory", root)
	compared, err = reportdiff.Compare(load(standalone), load(withDeclarations))
	if err != nil {
		t.Fatal(err)
	}
	if got := module(compared, "availability_references"); got.Compatibility != "incomparable" {
		t.Fatalf("different reference prerequisites compared: %#v", got)
	}

	environment := targetedCLIReport(t, "analyze", "environments", "--json", "--source", "directory", root)
	compared, err = reportdiff.Compare(load(environment), load(environment))
	if err != nil {
		t.Fatal(err)
	}
	if got := module(compared, "languages"); got.Compatibility != "incomparable" {
		t.Fatalf("environment-only report compared zero aggregate population: %#v", got)
	}
}

func TestAvailabilityGitTreeChangeRetainsSemanticCompatibility(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "asset.bin"), []byte("version https://git-lfs.github.com/spec/v1\noid sha256:"+strings.Repeat("a", 64)+"\nsize 42\n"), 0600); err != nil {
		t.Fatal(err)
	}
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	worktree, _ := repo.Worktree()
	_ = worktree.AddWithOptions(&git.AddOptions{All: true})
	if _, err := worktree.Commit("base", &git.CommitOptions{Author: &object.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Unix(1, 0)}}); err != nil {
		t.Fatal(err)
	}
	base := targetedCLIReport(t, "analyze", "availability", "--json", "--source", "git", root)
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("changed tree\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_ = worktree.AddWithOptions(&git.AddOptions{All: true})
	if _, err := worktree.Commit("head", &git.CommitOptions{Author: &object.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Unix(2, 0)}}); err != nil {
		t.Fatal(err)
	}
	head := targetedCLIReport(t, "analyze", "availability", "--json", "--source", "git", root)
	load := func(v map[string]any) *reportdiff.Snapshot {
		data, _ := json.Marshal(v)
		s, err := reportdiff.Load(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	compared, err := reportdiff.Compare(load(base), load(head))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range compared.Modules {
		if m.Name == "availability_lfs" {
			if m.Compatibility == "incomparable" {
				t.Fatalf("tree revision was treated as semantic policy: %#v", m)
			}
			return
		}
	}
	t.Fatal("availability lfs module omitted")
}
