package capabilities_test

import (
	"reflect"
	"testing"

	"github.com/war-and-code/dircue/pkg/capabilities"
)

func TestDircueDescriptorIsSmallDeterministicRegistry(t *testing.T) {
	a, b := capabilities.Dircue("0.8.0-test"), capabilities.Dircue("0.8.0-test")
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("descriptor changed between calls")
	}
	if len(a.Modules) != 9 {
		t.Fatalf("unexpected planner registry size: %d", len(a.Modules))
	}
	for _, m := range a.Modules {
		if m.Command.ArgvPrefix[0] != "dircue" || m.Command.ArgvPrefix[1] != "analyze" {
			t.Fatalf("non-dircue command: %+v", m)
		}
	}
}

func TestDescriptorRejectsDuplicateOrUnsortedModules(t *testing.T) {
	d := capabilities.Dircue("test")
	d.Modules[1].ID = d.Modules[0].ID
	if d.Validate() == nil {
		t.Fatal("accepted duplicate capability")
	}
}

func TestDescriptorRejectsQuestionCollisionAndUnsafeArgv(t *testing.T) {
	d := capabilities.Dircue("test")
	d.Modules[1].Question = d.Modules[0].Question
	if d.Validate() == nil {
		t.Fatal("accepted ambiguous question")
	}
	d = capabilities.Dircue("test")
	d.Modules[0].Command.ArgvPrefix = []string{"dircue", "analyze", "metrics", "--json", "{source}"}
	if d.Validate() == nil {
		t.Fatal("accepted noncanonical argv")
	}
}
