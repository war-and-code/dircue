package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/mapdoc"
)

func TestRunnableDeployableKindClassifiesChartAndComputeKinds(t *testing.T) {
	for _, tc := range []struct {
		kind, provider string
		want           bool
	}{
		{"infrastructure", "helm", true},
		{"infrastructure", "terraform", true},
		{"infrastructure", "cloudformation", false},
		{"library", "helm", false},
		{"function", "cloudformation", true},
		{"container_task", "cloudformation", true},
		{"archive", "maven", true},
		{"process", "procfile", true},
		{"build_invocation", "makefile", false},
		{"service", "kubernetes", false},
		{"future-kind", "future-provider", false},
	} {
		if got := runnableDeployableKind(tc.kind, tc.provider); got != tc.want {
			t.Errorf("runnableDeployableKind(%q, %q) = %v, want %v", tc.kind, tc.provider, got, tc.want)
		}
	}
}

func TestMapSummaryCountsCloudFormationInfrastructureAsSupporting(t *testing.T) {
	d := mapdoc.New()
	for _, item := range []struct {
		id, name, kind, provider string
	}{
		{"bucket", "Bucket", "infrastructure", "cloudformation"},
		{"fn", "Function", "function", "cloudformation"},
		{"task", "Task", "container_task", "cloudformation"},
		{"library", "Library", "library", "helm"},
		{"chart", "Chart", "infrastructure", "helm"},
	} {
		node := mapdoc.NewNode(mapdoc.NodeDeployable, []string{"template.yaml"}, item.id)
		node.Name = item.name
		node.Properties = map[string]string{"kind": item.kind, "provider": item.provider}
		d.Nodes = append(d.Nodes, node)
	}
	var output bytes.Buffer
	if err := writeMapSummary(&output, d); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if !strings.Contains(text, "Function [function]") || !strings.Contains(text, "Task [container_task]") {
		t.Fatalf("CloudFormation compute declarations should remain named runnable entries:\n%s", text)
	}
	if strings.Contains(text, "Bucket [infrastructure]") {
		t.Fatalf("CloudFormation infrastructure resource was listed as runnable:\n%s", text)
	}
	if !strings.Contains(text, "Chart [infrastructure]") || strings.Contains(text, "Library [library]") {
		t.Fatalf("Helm application chart should be runnable while its library chart is supporting:\n%s", text)
	}
	if !strings.Contains(text, "+2 supporting declarations") {
		t.Fatalf("CloudFormation infrastructure and Helm libraries should be counted as supporting:\n%s", text)
	}
}
