package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"

	"dircue/pkg/capabilities"
	"dircue/schema"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

func TestCapabilityDefaultDescriptorBytesRemainUnchanged(t *testing.T) {
	// The CLI wraps the descriptor with a sibling `views` array so an agent
	// can discover --cli/--guide/--schema from the default output. Every other
	// key remains byte-identical to json.Marshal(capabilities.Dircue).
	out, stderr, err := invoke("capabilities", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q err=%v", out, stderr, err)
	}
	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &wrapped); err != nil {
		t.Fatal(err)
	}
	views, ok := wrapped["views"]
	if !ok {
		t.Fatal("capabilities --json is missing the additive views field")
	}
	delete(wrapped, "views")
	remaining, err := json.Marshal(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	var expectedMap map[string]json.RawMessage
	original, err := json.Marshal(capabilities.Dircue(Version))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(original, &expectedMap); err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(expectedMap)
	if err != nil {
		t.Fatal(err)
	}
	if string(remaining) != string(want) {
		t.Fatalf("descriptor bytes drifted after removing views:\n got=%s\nwant=%s", remaining, want)
	}
	var parsedViews []struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Argv        []string `json:"argv"`
	}
	if err := json.Unmarshal(views, &parsedViews); err != nil {
		t.Fatal(err)
	}
	wantNames := []string{"planner", "cli", "guide", "schema"}
	if len(parsedViews) != len(wantNames) {
		t.Fatalf("view count = %d, want %d", len(parsedViews), len(wantNames))
	}
	for i, view := range parsedViews {
		if view.Name != wantNames[i] || len(view.Argv) < 2 || view.Argv[0] != "dircue" || view.Argv[1] != "capabilities" || view.Description == "" {
			t.Fatalf("view[%d] = %+v", i, view)
		}
	}
}

func TestCLIContractDerivesActualCommandsFlagsAndDefaultValues(t *testing.T) {
	out, stderr, err := invoke("capabilities", "--cli", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("%s %v", stderr, err)
	}
	var contract cliContract
	if err := json.Unmarshal([]byte(out), &contract); err != nil {
		t.Fatal(err)
	}
	if len(contract.Commands) != 24 || contract.Kind != "dircue-cli-capabilities" {
		t.Fatalf("commands=%d kind=%s", len(contract.Commands), contract.Kind)
	}
	flagLine := regexp.MustCompile(`(?m)^\s+(?:-[A-Za-z0-9],\s+)?--([a-z0-9-]+)(?:\s|$)`)
	for _, command := range contract.Commands {
		help, stderr, err := invoke(append(append([]string{}, command.Path[1:]...), "--help")...)
		if err != nil || stderr != "" {
			t.Fatalf("%v: %s %v", command.Path, stderr, err)
		}
		if !strings.Contains(help, command.Usage) {
			t.Errorf("usage missing from help: %s", command.Usage)
		}
		seen := map[string]bool{}
		for _, flag := range command.Flags {
			if seen[flag.Name] || !strings.Contains(help, "--"+flag.Name) {
				t.Errorf("%v: duplicate or undocumented flag %s", command.Path, flag.Name)
			}
			seen[flag.Name] = true
			if flag.Name == "json" && flag.Default != "false" {
				t.Errorf("default reflects invocation value: %+v", flag)
			}
		}
		for _, match := range flagLine.FindAllStringSubmatch(help, -1) {
			if !seen[match[1]] {
				t.Errorf("%v: help advertises --%s but the CLI catalog omits it", command.Path, match[1])
			}
		}
		if len(command.Path) == 2 && command.Path[1] == "plan" && (seen["source"] || !seen["module"] || !seen["json"]) {
			t.Fatalf("plan flag scope: %+v", command)
		}
	}
	for _, output := range contract.OutputContracts {
		if output.ID == "languages-file" && (output.Schema != "" || output.SchemaScope != "unavailable") {
			t.Fatal("single-file falsely mapped to directory schema")
		}
	}
	for _, resource := range contract.SchemaResources {
		if resource.Name == "hotspots" && (resource.Scope != "profile component" || resource.Pointer != "/structure/hotspots") {
			t.Fatal("component schema mislabeled")
		}
	}
	again, _, err := invoke("--json", "capabilities", "--cli")
	if err != nil || again != out {
		t.Fatal("CLI metadata depends on invocation order or mutable values")
	}
}

func TestCLIContractDisclosesIgnoredHelpFlagsAndPlainTextOutput(t *testing.T) {
	helpText, stderr, err := invoke("help", "--json", "plan")
	if err != nil || stderr != "" || json.Valid([]byte(helpText)) || !strings.Contains(helpText, "Usage:") {
		t.Fatalf("help --json behavior: stdout=%q stderr=%q err=%v", helpText, stderr, err)
	}

	out, stderr, err := invoke("capabilities", "--cli", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("CLI catalog: %s %v", stderr, err)
	}
	var contract cliContract
	if err := json.Unmarshal([]byte(out), &contract); err != nil {
		t.Fatal(err)
	}
	var help *cliCommandContract
	for i := range contract.Commands {
		if strings.Join(contract.Commands[i].Path, " ") == "dircue help" {
			help = &contract.Commands[i]
			break
		}
	}
	if help == nil {
		t.Fatal("CLI catalog omits the help command")
	}
	if !strings.Contains(strings.Join(help.Restrictions, " "), "Help always emits plain text") ||
		!strings.Contains(strings.Join(help.Restrictions, " "), "capabilities --cli --json") {
		t.Fatalf("help output restriction is incomplete: %+v", help.Restrictions)
	}
	sawJSON := false
	for _, flag := range help.Flags {
		if !flag.Inherited {
			continue
		}
		if !strings.Contains(flag.Description, "Ignored by help") {
			t.Errorf("inherited help flag --%s is not marked ignored: %q", flag.Name, flag.Description)
		}
		if flag.Name == "json" {
			sawJSON = true
			if flag.Type != "bool" || !strings.Contains(flag.Description, "help always emits plain text") || !strings.Contains(flag.Description, "capabilities --guide --json") {
				t.Errorf("help --json contract is inaccurate: %+v", flag)
			}
		}
	}
	if !sawJSON {
		t.Fatal("help command lost its compatibility --json flag")
	}
}

func TestExplicitCapabilityViewsRejectConflictsAndPropagateWriters(t *testing.T) {
	for _, args := range [][]string{
		{"capabilities", "--cli", "--guide"}, {"capabilities", "--guide", "--schema", "profile"},
		{"capabilities", "--cli=false"}, {"capabilities", "--guide=false"},
		{"capabilities", "--schema", ""}, {"capabilities", "--schema", "../../secret"},
		{"capabilities", "--cli", "--workers", "2"},
	} {
		out, stderr, err := invoke(args...)
		if err == nil || out != "" || stderr != "" {
			t.Fatalf("invalid view %v: %q %q %v", args, out, stderr, err)
		}
	}
	for _, args := range [][]string{
		{"capabilities", "--cli"}, {"capabilities", "--cli", "--json"},
		{"capabilities", "--guide"}, {"capabilities", "--guide", "--json"},
		{"capabilities", "--schema", "profile"},
	} {
		var stderr bytes.Buffer
		if err := Execute(t.Context(), args, ergonomicFailWriter{}, &stderr); err == nil {
			t.Fatalf("writer failure lost: %v", args)
		}
	}
	var stderr bytes.Buffer
	if err := Execute(t.Context(), []string{"capabilities", "--schema", "profile"}, capabilityShortWriter{}, &stderr); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write: %v", err)
	}
}

type capabilityShortWriter struct{}

func (capabilityShortWriter) Write([]byte) (int, error) { return 0, nil }

func TestCapabilityJSONViewsConformToTheirOfflineSchemas(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"cli-capabilities", []string{"capabilities", "--cli", "--json"}},
		{"guide", []string{"capabilities", "--guide", "--json"}},
	} {
		raw, err := schema.Export(tc.name)
		if err != nil {
			t.Fatal(err)
		}
		compiler := jsonschema.NewCompiler()
		compiler.LoadURL = func(url string) (io.ReadCloser, error) { return nil, fmt.Errorf("unexpected schema IO: %s", url) }
		if err := compiler.AddResource("https://test.invalid/schema.json", bytes.NewReader(raw)); err != nil {
			t.Fatal(err)
		}
		compiled, err := compiler.Compile("https://test.invalid/schema.json")
		if err != nil {
			t.Fatal(err)
		}
		out, stderr, err := invoke(tc.args...)
		if err != nil || stderr != "" {
			t.Fatalf("%s %v", stderr, err)
		}
		var value any
		if err := json.Unmarshal([]byte(out), &value); err != nil {
			t.Fatal(err)
		}
		if err := compiled.Validate(value); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
}

func TestGuideTextAndJSONShareSafeExamples(t *testing.T) {
	out, stderr, err := invoke("capabilities", "--guide", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("%s %v", stderr, err)
	}
	var guide cliGuide
	if err := json.Unmarshal([]byte(out), &guide); err != nil {
		t.Fatal(err)
	}
	plain, _, err := invoke("capabilities", "--guide")
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range guide.Sections {
		if !strings.Contains(plain, section.Guidance) {
			t.Fatalf("guide drift: %s", section.Title)
		}
		for _, argv := range section.Examples {
			if !strings.Contains(plain, strings.Join(argv, " ")) {
				t.Fatalf("missing example: %v", argv)
			}
		}
	}
	if !strings.Contains(plain, "inert argv") || !strings.Contains(plain, "not a sandbox") || !strings.Contains(plain, "no bundled schema") {
		t.Fatalf("missing boundaries: %s", plain)
	}
}
