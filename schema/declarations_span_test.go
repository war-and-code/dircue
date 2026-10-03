package schema_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	publicschema "github.com/war-and-code/dircue/schema"
)

func TestDotnetInterfaceSourceSpansValidateInExportedSchemas(t *testing.T) {
	profileSchema, err := jsonschema.Compile("profile.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	declarationsSchema, err := jsonschema.Compile("declarations.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	project := `<Project>
  <PropertyGroup Condition="'$(Configuration)' == 'Release'">
    <OutputType>Exe</OutputType>
  </PropertyGroup>
</Project>
`
	if err := os.WriteFile(filepath.Join(root, "App.csproj"), []byte(project), 0600); err != nil {
		t.Fatal(err)
	}
	value := schemaOutput(t, []string{"analyze", "declarations", "--json", root})
	validateProfile(t, profileSchema, value)
	if err := publicschema.ValidateProfile(value); err != nil {
		t.Fatalf("public profile validator rejected CLI output: %v", err)
	}
	declarations := value["declarations"].(map[string]any)
	if err := declarationsSchema.Validate(declarations); err != nil {
		t.Fatalf("exported declarations schema rejected CLI component: %v", err)
	}
	projects := declarations["projects"].([]any)
	interfaces := projects[0].(map[string]any)["interfaces"].([]any)
	if len(interfaces) != 1 {
		t.Fatalf("expected one interface from the .NET project: %+v", interfaces)
	}
	iface := interfaces[0].(map[string]any)
	if iface["kind"] != "dotnet-application" || iface["start_line"] != float64(3) || iface["end_line"] != float64(3) {
		t.Fatalf("CLI did not preserve the .NET interface source span: %+v", iface)
	}

	for _, mutate := range []struct {
		name  string
		field string
		value any
	}{
		{name: "string line", field: "start_line", value: "3"},
		{name: "negative start", field: "start_line", value: float64(-1)},
		{name: "negative end", field: "end_line", value: float64(-1)},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			copyValue := cloneJSONValue(t, value)
			projects := copyValue["declarations"].(map[string]any)["projects"].([]any)
			interfaces := projects[0].(map[string]any)["interfaces"].([]any)
			interfaces[0].(map[string]any)[mutate.field] = mutate.value
			if err := profileSchema.Validate(copyValue); err == nil {
				t.Fatal("profile schema accepted an invalid source span")
			}
			if err := publicschema.ValidateProfile(copyValue); err == nil {
				t.Fatal("public profile validator accepted an invalid source span")
			}
			if err := declarationsSchema.Validate(copyValue["declarations"]); err == nil {
				t.Fatal("declarations schema accepted an invalid source span")
			}
		})
	}
}

func cloneJSONValue(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
