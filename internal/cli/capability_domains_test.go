package cli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/capabilities"
	"github.com/war-and-code/dircue/schema"
)

func TestCLIContractPublishesFiniteCommandScopedFlagValues(t *testing.T) {
	out, stderr, err := invoke("capabilities", "--cli", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("capabilities: stderr=%q err=%v", stderr, err)
	}
	var contract cliContract
	if err := json.Unmarshal([]byte(out), &contract); err != nil {
		t.Fatal(err)
	}
	commands := make(map[string]cliCommandContract, len(contract.Commands))
	for _, command := range contract.Commands {
		commands[strings.Join(command.Path, " ")] = command
	}
	values := func(command, name string) []string {
		t.Helper()
		for _, flag := range commands[command].Flags {
			if flag.Name == name {
				return flag.AllowedValues
			}
		}
		t.Fatalf("%s does not catalog --%s", command, name)
		return nil
	}

	if got, want := values("dircue capabilities", "schema"), schema.Names(); !slices.Equal(got, want) {
		t.Fatalf("schema values = %v, want %v", got, want)
	}
	registry := capabilities.Dircue(Version)
	var modules, questions, inputs []string
	for _, module := range registry.Modules {
		modules = append(modules, module.ID)
		questions = append(questions, module.Question)
		for _, input := range module.RequiredInputs {
			if input != "source" && input != "project" {
				inputs = append(inputs, input)
			}
		}
	}
	slices.Sort(modules)
	slices.Sort(questions)
	slices.Sort(inputs)
	inputs = slices.Compact(inputs)
	for _, tc := range []struct {
		name string
		want []string
	}{{"module", modules}, {"question", questions}, {"input", inputs}} {
		if got := values("dircue plan", tc.name); !slices.Equal(got, tc.want) {
			t.Errorf("plan --%s values = %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := values("dircue analyze metrics", "metrics-scope"); !slices.Equal(got, []string{"source", "text"}) {
		t.Fatalf("metrics scope values = %v", got)
	}
}
