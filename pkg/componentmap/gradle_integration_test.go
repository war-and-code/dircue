package componentmap_test

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/war-and-code/dircue/pkg/componentmap"
	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/mapdoc"
)

func gradleMapFacts(t *testing.T, source, tree string, files map[string]string) (componentmap.Fragment, []mapdoc.Node, []mapdoc.Edge) {
	t.Helper()
	c := declarations.New(source, tree, 0)
	for path, content := range files {
		name := path
		body := []byte(content)
		var candidate *declarations.Candidate
		if declarations.IsManifest(name) {
			candidate = &declarations.Candidate{
				Path: name,
				Size: int64(len(body)),
				Read: func(_ context.Context, _ int64) ([]byte, int64, error) {
					return slices.Clone(body), int64(len(body)), nil
				},
			}
		}
		c.Add(name, candidate)
	}
	report, err := c.Finish(context.Background())
	if err != nil {
		t.Fatalf("finish declarations: %v", err)
	}
	fragment := componentmap.Build(report)
	nodes, edges := componentmap.MapFacts(fragment)
	return fragment, nodes, edges
}

func TestActualGradleSettingsBecomeConditionalMapMembership(t *testing.T) {
	files := map[string]string{
		"settings.gradle":      "rootProject.name = 'suite'\ninclude(':app')\n",
		"build.gradle":         "apply plugin: 'java'\n",
		"app/build.gradle.kts": "plugins { java }\n",
		"app/src/Main.java":    "class Main {}\n",
	}
	fragment, nodes, edges := gradleMapFacts(t, "directory", "", files)
	if !slices.ContainsFunc(fragment.Components, func(c componentmap.Component) bool { return c.Key == "build.gradle" && c.Name == "suite" }) {
		t.Fatalf("root Gradle component ID/name changed: %+v", fragment.Components)
	}
	if !slices.ContainsFunc(fragment.Components, func(c componentmap.Component) bool { return c.Key == "app/build.gradle.kts" }) {
		t.Fatalf("included Gradle project missing: %+v", fragment.Components)
	}
	rootID, childID := mapComponentID(t, nodes, "build.gradle"), mapComponentID(t, nodes, "app/build.gradle.kts")
	var membership *mapdoc.Edge
	for i := range edges {
		if edges[i].Type == mapdoc.EdgeMemberOf && edges[i].From == childID && edges[i].To == rootID {
			membership = &edges[i]
		}
	}
	if membership == nil {
		t.Fatalf("missing child-to-root Gradle membership edge: %+v", edges)
	}
	if membership.Evidence[0].Rule == nil || membership.Evidence[0].Rule.ID != "dircue/gradle-workspace" || membership.Evidence[0].Rule.Version != "1.0.0" {
		t.Fatalf("Gradle membership should be attributed to its dedicated producer: %+v", membership.Evidence)
	}
	for _, key := range []string{"build.gradle", "app/build.gradle.kts"} {
		node, found := componentNodeByID(nodes, mapComponentID(t, nodes, key))
		if !found || len(node.Evidence) != 1 || node.Evidence[0].Rule == nil || node.Evidence[0].Rule.ID != "dircue/component-declarations" || node.Evidence[0].Rule.Version != "1.0.0" {
			t.Fatalf("existing Gradle component fact changed producer for %s: %+v", key, node)
		}
	}
	if membership.Coverage.Status != mapdoc.CoveragePartial || membership.Properties["state"] != "conditional" || membership.Properties["condition"] != "condition-present; expression-withheld" {
		t.Fatalf("Gradle membership overstated evaluation: %+v", membership)
	}
	if len(membership.Evidence) != 1 || membership.Evidence[0].Path != "settings.gradle" {
		t.Fatalf("membership evidence must name settings.gradle: %+v", membership.Evidence)
	}
}

func TestSettingsOnlyGradleRootAndCustomProjectDir(t *testing.T) {
	files := map[string]string{
		"settings.gradle.kts":          "rootProject.name = \"platform\"\ninclude(\":api\")\nproject(\":api\").projectDir = file(\"modules/api\")\n",
		"modules/api/build.gradle.kts": "plugins { java }\n",
		"modules/api/src/Main.java":    "class Main {}\n",
	}
	fragment, nodes, edges := gradleMapFacts(t, "directory", "", files)
	var workspace *componentmap.Component
	for i := range fragment.Components {
		if fragment.Components[i].Key == "settings.gradle.kts" {
			workspace = &fragment.Components[i]
		}
	}
	if workspace == nil || workspace.Root != "." || workspace.Manifest != "settings.gradle.kts" || workspace.Name != "platform" || workspace.Coverage != "partial" {
		t.Fatalf("settings-only root was not represented as a qualified Gradle component: %+v", fragment.Components)
	}
	workspaceNode, found := componentNodeByID(nodes, mapComponentID(t, nodes, "settings.gradle.kts"))
	if !found || len(workspaceNode.Evidence) != 1 || workspaceNode.Evidence[0].Rule == nil || workspaceNode.Evidence[0].Rule.ID != "dircue/gradle-workspace" {
		t.Fatalf("synthetic settings-root should use the Gradle workspace producer: %+v", workspaceNode)
	}
	if got := countGradleMembership(edges, mapComponentID(t, nodes, "modules/api/build.gradle.kts"), mapComponentID(t, nodes, "settings.gradle.kts")); got != 1 {
		t.Fatalf("custom projectDir membership count = %d, edges: %+v", got, edges)
	}
	for _, edge := range edges {
		if edge.Type == mapdoc.EdgeContains && edge.From == mapComponentID(t, nodes, "settings.gradle.kts") {
			t.Fatalf("settings-only workspace inferred physical child ownership: %+v", edge)
		}
	}
}

func TestNestedGradleSettingsRootsRetainOverlappingMembershipClaims(t *testing.T) {
	files := map[string]string{
		"settings.gradle":              "rootProject.name = 'outer'\ninclude(':nested:service')\n",
		"build.gradle":                 "apply plugin: 'base'\n",
		"nested/settings.gradle.kts":   "rootProject.name = \"inner\"\ninclude(\":service\")\n",
		"nested/build.gradle.kts":      "plugins { base }\n",
		"nested/service/build.gradle":  "plugins { java }\n",
		"nested/service/src/Main.java": "class Main {}\n",
	}
	fragment, nodes, edges := gradleMapFacts(t, "directory", "", files)
	serviceID := mapComponentID(t, nodes, "nested/service/build.gradle")
	outerID := mapComponentID(t, nodes, "build.gradle")
	innerID := mapComponentID(t, nodes, "nested/build.gradle.kts")
	if got := countGradleMembership(edges, serviceID, outerID); got != 1 {
		t.Fatalf("outer membership count = %d, edges: %+v", got, edges)
	}
	if got := countGradleMembership(edges, serviceID, innerID); got != 1 {
		t.Fatalf("inner membership count = %d, edges: %+v", got, edges)
	}
	for _, edge := range edges {
		if edge.Type != mapdoc.EdgeMemberOf || edge.From != serviceID {
			continue
		}
		if len(edge.Evidence) != 1 || (edge.Evidence[0].Path != "settings.gradle" && edge.Evidence[0].Path != "nested/settings.gradle.kts") {
			t.Fatalf("overlapping claim lost its source settings file: %+v", edge)
		}
	}
	if len(fragment.QualifiedReferences) != 0 {
		t.Fatalf("unique overlapping declarations should remain separate conditional edges: %+v", fragment.QualifiedReferences)
	}
}

func TestGradleSettingsAmbiguityAndUnresolvedTargetsStayQualified(t *testing.T) {
	tests := []struct {
		name       string
		files      map[string]string
		wantReason string
		wantFrom   string
		wantCount  int
	}{
		{
			name: "missing target",
			files: map[string]string{
				"settings.gradle": "include(':missing')\n",
				"build.gradle":    "plugins { base }\n",
			},
			wantReason: "target_missing",
			wantFrom:   "build.gradle",
			wantCount:  1,
		},
		{
			name: "target without Gradle component",
			files: map[string]string{
				"settings.gradle":     "include(':vendor')\n",
				"build.gradle":        "plugins { base }\n",
				"vendor/package.json": "{}\n",
			},
			wantReason: "target_not_a_retained_gradle_component",
			wantFrom:   "build.gradle",
			wantCount:  1,
		},
		{
			name: "ambiguous Gradle target root",
			files: map[string]string{
				"settings.gradle":      "include(':api')\n",
				"build.gradle":         "plugins { base }\n",
				"api/build.gradle":     "plugins { java }\n",
				"api/build.gradle.kts": "plugins { java }\n",
			},
			wantReason: "ambiguous_gradle_target_root",
			wantFrom:   "build.gradle",
			wantCount:  1,
		},
		{
			name: "dynamic projectDir",
			files: map[string]string{
				"settings.gradle":  "include(':api')\nproject(':api').projectDir = file(rootDirFromEnvironment)\n",
				"build.gradle":     "plugins { base }\n",
				"api/build.gradle": "plugins { java }\n",
			},
			wantReason: "reference_unresolved",
			wantFrom:   "build.gradle",
			wantCount:  1,
		},
		{
			name: "duplicate settings files",
			files: map[string]string{
				"settings.gradle":     "include(':api')\n",
				"settings.gradle.kts": "include(\":api\")\n",
				"build.gradle":        "plugins { base }\n",
				"api/build.gradle":    "plugins { java }\n",
			},
			wantReason: "ambiguous_gradle_settings_root",
			wantFrom:   "build.gradle",
			wantCount:  2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fragment, nodes, edges := gradleMapFacts(t, "directory", "", tt.files)
			for _, edge := range edges {
				if edge.Type == mapdoc.EdgeMemberOf {
					t.Fatalf("ambiguous or unresolved Gradle input invented membership: %+v", edge)
				}
			}
			if len(fragment.QualifiedReferences) != tt.wantCount {
				t.Fatalf("qualified Gradle reference count = %d: %+v", len(fragment.QualifiedReferences), fragment.QualifiedReferences)
			}
			for _, q := range fragment.QualifiedReferences {
				if q.From != tt.wantFrom || q.Reason != tt.wantReason || q.Evidence != "settings.gradle" && q.Evidence != "settings.gradle.kts" {
					t.Fatalf("qualified Gradle reference = %+v", q)
				}
				node, found := componentNodeByID(nodes, mapComponentID(t, nodes, q.From))
				if !found {
					t.Fatalf("qualified reference source component missing: %+v", q)
				}
				foundGradleRule := false
				for _, fact := range node.Facts {
					if fact.Kind == "qualified_local_reference" && len(fact.Evidence) == 1 && fact.Evidence[0].Rule != nil && fact.Evidence[0].Rule.ID == "dircue/gradle-workspace" && fact.Evidence[0].Rule.Version == "1.0.0" {
						foundGradleRule = true
					}
				}
				if !foundGradleRule {
					t.Fatalf("qualified settings fact lost Gradle workspace producer: %+v", node.Facts)
				}
			}
		})
	}
}

func TestGradleMembershipFactsAreStableAcrossDirectoryAndGitSources(t *testing.T) {
	files := map[string]string{
		"settings.gradle":      "include(':api')\n",
		"build.gradle":         "plugins { base }\n",
		"api/build.gradle.kts": "plugins { java }\n",
	}
	directory, directoryNodes, directoryEdges := gradleMapFacts(t, "directory", "", files)
	git, gitNodes, gitEdges := gradleMapFacts(t, "git", "0123456789abcdef0123456789abcdef01234567", files)
	if !reflect.DeepEqual(directory, git) || !reflect.DeepEqual(directoryNodes, gitNodes) || !reflect.DeepEqual(directoryEdges, gitEdges) {
		directoryJSON, _ := json.Marshal(struct {
			Fragment componentmap.Fragment
			Nodes    []mapdoc.Node
			Edges    []mapdoc.Edge
		}{directory, directoryNodes, directoryEdges})
		gitJSON, _ := json.Marshal(struct {
			Fragment componentmap.Fragment
			Nodes    []mapdoc.Node
			Edges    []mapdoc.Edge
		}{git, gitNodes, gitEdges})
		t.Fatalf("source mode changed Gradle map facts:\ndirectory=%s\ngit=%s", directoryJSON, gitJSON)
	}
}

func countGradleMembership(edges []mapdoc.Edge, child, root string) int {
	count := 0
	for _, edge := range edges {
		if edge.Type == mapdoc.EdgeMemberOf && edge.From == child && edge.To == root && edge.Properties["declaration_kind"] == "gradle-module" {
			count++
		}
	}
	return count
}

func componentNodeByID(nodes []mapdoc.Node, id string) (mapdoc.Node, bool) {
	for _, n := range nodes {
		if n.ID == id && n.Kind == mapdoc.NodeComponent {
			return n, true
		}
	}
	return mapdoc.Node{}, false
}

func mapComponentID(t *testing.T, nodes []mapdoc.Node, manifest string) string {
	t.Helper()
	for _, n := range nodes {
		if n.Kind == mapdoc.NodeComponent && slices.Contains(n.Paths, manifest) {
			return n.ID
		}
	}
	t.Fatalf("no component node for manifest %q", manifest)
	return ""
}
