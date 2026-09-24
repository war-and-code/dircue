package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"dircue/pkg/mapdoc"
	"dircue/pkg/profile"
	"dircue/pkg/structure"
)

func TestFunctionFlagsRequireExplicitStructuralAnalysis(t *testing.T) {
	for _, flag := range []string{"--functions", "--functions=false"} {
		out, stderr, err := invoke("analyze", "all", flag)
		if err == nil || err.Error() != "--functions requires --structure" || out != "" || stderr != "" {
			t.Fatalf("%s: stdout=%q stderr=%q error=%v", flag, out, stderr, err)
		}
	}
	for _, mode := range []string{"languages", "discovery", "metrics", "projects", "graph", "packages"} {
		out, _, err := invoke("analyze", mode, "--functions")
		if err == nil || !strings.Contains(err.Error(), "unknown flag") || out != "" {
			t.Fatalf("%s accepted unrelated function flag: %q %v", mode, out, err)
		}
	}
	out, _, err := invoke("analyze", "structure", "--functions")
	if err == nil || !strings.Contains(err.Error(), "worker") || out != "" {
		t.Fatalf("functions must require explicit worker: %q %v", out, err)
	}
}

func TestFunctionTextPreservesCoverageAndEscapesNames(t *testing.T) {
	r := &profile.FunctionReport{
		Provider: "big-code-analysis@2.2.0", Status: "partial", ParentStatus: "partial",
		TotalSpaces: 5, OmittedSpaces: 3, InvalidSpanSpaces: 1,
		Omissions: map[string]int64{"unsupported_language": 2, "file_too_large": 1},
		Entries: []profile.FunctionEvidence{{Path: "odd\npath.py", Language: "Python", FunctionEntry: structure.FunctionEntry{
			Name: "quoted\"name", NameStatus: "present", StartLine: 2, EndLine: 4,
		}}},
	}
	var out bytes.Buffer
	if err := writeFunctions(&out, r); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"status: partial; file coverage: partial", "Retained: 1 of 5", "omitted: 3; invalid spans: 1", "do not sum overlapping entries", `"odd\npath.py":2-4: "quoted\"name"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in %q", want, out.String())
		}
	}
	if strings.Index(out.String(), "file_too_large") > strings.Index(out.String(), "unsupported_language") {
		t.Fatal("coverage reasons are not sorted")
	}
	if err := writeFunctions(functionFailWriter{}, r); !errors.Is(err, errFunctionWrite) {
		t.Fatalf("write error lost: %v", err)
	}
	out.Reset()
	if err := writeFunctions(&out, nil); err != nil || out.Len() != 0 {
		t.Fatal("absent opt-in changed output")
	}
}

var errFunctionWrite = errors.New("write failed")

type functionFailWriter struct{}

func (functionFailWriter) Write([]byte) (int, error) { return 0, errFunctionWrite }

func TestMapNodeLabelUsesRootDirectoryNotManifestFilename(t *testing.T) {
	for _, tc := range []struct {
		desc string
		node mapdoc.Node
		want string
	}{
		{
			desc: "unnamed root component gets (root) label not Cargo",
			node: func() mapdoc.Node {
				n := mapdoc.NewNode(mapdoc.NodeComponent, []string{"Cargo.toml"}, "cargo")
				n.Properties = map[string]string{"root": "."}
				return n
			}(),
			want: "(root)",
		},
		{
			desc: "unnamed root component from Gemfile gets (root) not Gemfile",
			node: func() mapdoc.Node {
				n := mapdoc.NewNode(mapdoc.NodeComponent, []string{"Gemfile"}, "ruby-bundler")
				n.Properties = map[string]string{"root": "."}
				return n
			}(),
			want: "(root)",
		},
		{
			desc: "unnamed nested component uses directory basename",
			node: func() mapdoc.Node {
				n := mapdoc.NewNode(mapdoc.NodeComponent, []string{"services/api/go.mod"}, "go")
				n.Properties = map[string]string{"root": "services/api"}
				return n
			}(),
			want: "api",
		},
		{
			desc: "named component label is the declared name",
			node: func() mapdoc.Node {
				n := mapdoc.NewNode(mapdoc.NodeComponent, []string{"go.mod"}, "go")
				n.Name = "myapp"
				n.Properties = map[string]string{"root": "."}
				return n
			}(),
			want: "myapp",
		},
	} {
		got := mapNodeLabel(tc.node)
		if got != tc.want {
			t.Errorf("%s: mapNodeLabel = %q, want %q", tc.desc, got, tc.want)
		}
	}
}
