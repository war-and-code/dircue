package deployables

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/profile"
)

func TestProcfileCandidateIsExactRootCaseSensitive(t *testing.T) {
	for name, want := range map[string]bool{
		"Procfile":          true,
		"procfile":          false,
		"services/Procfile": false,
		"docs/Procfile":     false,
	} {
		if got := IsCandidate(name); got != want {
			t.Errorf("IsCandidate(%q) = %t, want %t", name, got, want)
		}
	}
}

func TestProcfileBoundsExposeFixedLimits(t *testing.T) {
	lineBytes, processLines, inventoryFiles, inventoryBytes := ProcfileBounds()
	if lineBytes != procfileLineBytes || processLines != procfileMaxLines || inventoryFiles != procfileTargetInventoryFiles || inventoryBytes != procfileTargetInventoryBytes {
		t.Fatalf("ProcfileBounds() = %d, %d, %d, %d", lineBytes, processLines, inventoryFiles, inventoryBytes)
	}
}

func TestParseProcfileKeepsOnlySafeProcessAndTargetIdentifiers(t *testing.T) {
	input := `web: gunicorn autoapp:app -b 0.0.0.0:$PORT -w 3
api: uvicorn service.api:application --host 0.0.0.0 --port $PORT
node: node ./src/server.js --token TOP-SECRET
pyfile: python3.12 ./worker/main.py --password TOP-SECRET
module: python -m tasks.runner --key TOP-SECRET
opaque: sh -c 'echo TOP-SECRET'
`
	defs, recognized, err := parseProcfile("Procfile", []byte(input))
	if !recognized || len(defs) != 6 {
		t.Fatalf("parseProcfile: defs=%+v recognized=%t err=%v", defs, recognized, err)
	}
	issues, ok := err.(*procfileIssuesError)
	if !ok || issues.counts["procfile_unrecognized_command"] != 1 {
		t.Fatalf("opaque command issue = %T %v", err, err)
	}
	want := map[string]string{
		"web":    "autoapp",
		"api":    "service.api",
		"node":   "src/server.js",
		"pyfile": "worker/main.py",
		"module": "tasks.runner",
	}
	for _, def := range defs {
		if def.Provider != "procfile" || def.Kind != "process" || def.Name == "" || len(def.Evidence) != 1 || def.Evidence[0].Value != def.Name {
			t.Errorf("unsafe or incomplete process definition: %+v", def)
		}
		if target, ok := want[def.Name]; ok {
			if def.Coverage != "complete" || len(def.References) != 1 || def.References[0].Value != target {
				t.Errorf("%s target: %+v", def.Name, def)
			}
		} else if def.Coverage != "qualified" || len(def.References) != 1 || def.References[0].Value != "unresolved" {
			t.Errorf("opaque command was not qualified: %+v", def)
		}
	}
	encoded, err := json.Marshal(defs)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"TOP-SECRET", "$PORT", "gunicorn", "uvicorn", "password", "token"} {
		if strings.Contains(string(encoded), secret) {
			t.Errorf("retained command detail %q in %s", secret, encoded)
		}
	}
}

func TestResolveProcfileTargetsRequiresUniqueSelectedFile(t *testing.T) {
	defs, _, err := parseProcfile("Procfile", []byte("web: gunicorn app:main\nnode: node src/server.js\nmodule: python -m worker\n"))
	if err != nil {
		t.Fatal(err)
	}
	selected := newProcfileTargetInventory()
	for _, name := range []string{
		"app.py",
		"app/__init__.py",
		"src/server.js",
		"worker.py",
		"worker/__main__.py",
	} {
		selected.add(name)
	}
	if unresolved := resolveProcfileTargets(defs, selected); unresolved != 2 {
		t.Fatalf("unresolved targets = %d, want 2", unresolved)
	}
	for _, def := range defs {
		ref := def.References[0]
		if ref.Kind != "process_target" {
			t.Errorf("target kind was not normalized: %+v", ref)
		}
		if def.Name == "node" {
			if ref.Qualification != "local" || ref.SourcePath != "src/server.js" {
				t.Errorf("unique node target unresolved: %+v", ref)
			}
		} else if ref.Qualification != "unresolved" || ref.SourcePath != "" {
			t.Errorf("ambiguous target linked: %+v", ref)
		}
	}
	selected = newProcfileTargetInventory()
	for _, name := range []string{"app.py", "src/server.js", "worker.py"} {
		selected.add(name)
	}
	defs, _, err = parseProcfile("Procfile", []byte("web: gunicorn app:main\nnode: node src/server.js\nmodule: python -m worker\n"))
	if err != nil {
		t.Fatal(err)
	}
	if unresolved := resolveProcfileTargets(defs, selected); unresolved != 0 {
		t.Fatalf("unresolved targets = %d, want 0", unresolved)
	}
	if defs[0].References[0].SourcePath != "app.py" || defs[1].References[0].SourcePath != "src/server.js" || defs[2].References[0].SourcePath != "worker.py" {
		t.Fatalf("unique selected paths not retained privately: %+v", defs)
	}
}

func TestProcfileMalformedDuplicateAndBoundedLinesAreOmitted(t *testing.T) {
	input := "broken\nweb: node app.js\nweb: node other.js\n" + strings.Repeat("x", procfileLineBytes+1) + "\n"
	defs, recognized, err := parseProcfile("Procfile", []byte(input))
	if !recognized || len(defs) != 1 {
		t.Fatalf("defs=%+v recognized=%t", defs, recognized)
	}
	issues, ok := err.(*procfileIssuesError)
	if !ok {
		t.Fatalf("issue error = %T %v", err, err)
	}
	for reason, want := range map[string]int64{
		"procfile_malformed_line":    1,
		"procfile_duplicate_process": 1,
		"procfile_line_limit":        1,
	} {
		if issues.counts[reason] != want {
			t.Errorf("%s = %d, want %d", reason, issues.counts[reason], want)
		}
	}
	if defs[0].Coverage != "qualified" || defs[0].References[0].Qualification != "unresolved" || defs[0].References[0].SourcePath != "" {
		t.Fatalf("duplicate process name selected one declaration: %+v", defs[0])
	}
}

func TestProcfileShellSyntaxAndDynamicTargetsStayUnresolved(t *testing.T) {
	input := `web: gunicorn app:main && echo secret
api: gunicorn ${APP}:main
worker: node ../outside.js
queue: python -m unknown.$MODULE
`
	defs, recognized, err := parseProcfile("Procfile", []byte(input))
	if !recognized || err == nil || len(defs) != 4 {
		t.Fatalf("defs=%+v recognized=%t err=%v", defs, recognized, err)
	}
	for _, def := range defs {
		if def.Coverage != "qualified" || def.References[0].Qualification != "unresolved" {
			t.Errorf("unsafe command produced a local target: %+v", def)
		}
	}
	encoded, _ := json.Marshal(defs)
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "APP") || strings.Contains(string(encoded), "MODULE") {
		t.Fatalf("dynamic or command text leaked: %s", encoded)
	}
}

func TestProcfileSearchPathChangingFlagsPreventTargetResolution(t *testing.T) {
	input := `web: gunicorn app:main --chdir /srv/app
api: uvicorn app.main:application --app-dir /srv/app
`
	defs, recognized, err := parseProcfile("Procfile", []byte(input))
	if !recognized || err == nil || len(defs) != 2 {
		t.Fatalf("defs=%+v recognized=%t err=%v", defs, recognized, err)
	}
	for _, def := range defs {
		if def.Coverage != "qualified" || def.References[0].Qualification != "unresolved" || def.References[0].Value != "unresolved" {
			t.Errorf("search-path-changing option retained a target: %+v", def)
		}
	}
}

func TestProcfileTargetInventoryIsFilteredBoundedAndOrderIndependent(t *testing.T) {
	unrelated := newProcfileTargetInventory()
	for i := 0; i < procfileTargetInventoryFiles+1; i++ {
		unrelated.add(fmt.Sprintf("src/file-%08d.txt", i))
	}
	if unrelated.capped || len(unrelated.paths) != 0 {
		t.Fatalf("unrelated inventory was retained or capped: %+v", unrelated)
	}

	// Each path is long enough for the byte ceiling to trip before the count
	// ceiling. The complete candidate set is identical in both insertion orders.
	paths := []string{"app.js"}
	for i := 0; i <= procfileTargetInventoryBytes/95; i++ {
		paths = append(paths, fmt.Sprintf("src/%s%08d.js", strings.Repeat("a", 80), i))
	}
	build := func(reverse bool) procfileTargetInventory {
		inventory := newProcfileTargetInventory()
		if reverse {
			for i := len(paths) - 1; i >= 0; i-- {
				inventory.add(paths[i])
			}
		} else {
			for _, name := range paths {
				inventory.add(name)
			}
		}
		return inventory
	}
	a, b := build(false), build(true)
	if !a.capped || !b.capped || len(a.paths) != 0 || len(b.paths) != 0 {
		t.Fatalf("over-limit inventory retained paths: forward=%+v reverse=%+v", a, b)
	}

	content := []byte("web: node app.js\n")
	files := make([]Candidate, 0, len(paths)+1)
	files = append(files, Candidate{Path: "Procfile", Size: int64(len(content)), Read: func(context.Context, int64) ([]byte, int64, error) {
		return content, int64(len(content)), nil
	}})
	for _, name := range paths {
		files = append(files, Candidate{Path: name})
	}
	report, err := Observe(context.Background(), files, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "partial" || report.Omissions["procfile_inventory_limit"] != 1 || len(report.Definitions) != 1 {
		t.Fatalf("inventory cap not reported: status=%s omissions=%v definitions=%+v", report.Status, report.Omissions, report.Definitions)
	}
	if ref := report.Definitions[0].References[0]; ref.Qualification != "unresolved" || ref.SourcePath != "" {
		t.Fatalf("target linked despite inventory cap: %+v", ref)
	}

	runCollector := func(reverse bool) *Report {
		collector := NewCollector(Options{})
		for i := range paths {
			index := i
			if reverse {
				index = len(paths) - i - 1
			}
			if _, err := collector.Detect(context.Background(), profile.File{Path: paths[index]}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := collector.Detect(context.Background(), profile.File{Path: "Procfile", Size: int64(len(content)), Content: content}); err != nil {
			t.Fatal(err)
		}
		return collector.Finish()
	}
	collected, reordered := runCollector(false), runCollector(true)
	if !reflect.DeepEqual(collected, reordered) {
		t.Fatalf("over-limit inventory result depends on traversal order:\n%+v\n%+v", collected, reordered)
	}
	if collected.Status != "partial" || collected.Omissions["procfile_inventory_limit"] != 1 || len(collected.Definitions) != 1 || collected.Definitions[0].References[0].Qualification != "unresolved" {
		t.Fatalf("collector cap not reported or linked: status=%s omissions=%v definitions=%+v", collected.Status, collected.Omissions, collected.Definitions)
	}
}
