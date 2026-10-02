package declarations

import "testing"

func TestDotnetOutputTypeIsRetainedAsLaunchInterfaceWithSpan(t *testing.T) {
	d := Parse("src/App/App.csproj", []byte(`<Project>
  <PropertyGroup Condition="'$(Configuration)' == 'Release'"><OutputType>Exe</OutputType></PropertyGroup>
</Project>`))
	if d == nil || len(d.Project.Interfaces) != 1 {
		t.Fatalf("declaration: %+v", d)
	}
	i := d.Project.Interfaces[0]
	if i.Kind != "dotnet-application" || i.Name != "App" || i.Target != "Exe" || i.State != "conditional" || i.Condition == "" || i.StartLine != 2 || i.EndLine != 2 {
		t.Fatalf("interface: %+v", i)
	}
}
