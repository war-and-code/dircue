package deployables

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func BenchmarkAspireAppHostParsing(b *testing.B) {
	var many strings.Builder
	many.WriteString("using Aspire.Hosting;\nvar builder = DistributedApplication.CreateBuilder(args);\n")
	const references = 64
	for i := 0; i < references; i++ {
		fmt.Fprintf(&many, "builder.AddProject<Projects.Service%d>(\"service-%d\");\n", i, i)
	}
	cases := []struct {
		name      string
		source    string
		wantRefs  int
		wantFound bool
		wantError bool
	}{
		{name: "valid-many-projects", source: many.String(), wantRefs: references, wantFound: true},
		{name: "ordinary-program", source: "using System;\nclass Program { static void Main() {} }\n"},
		{name: "no-aspire-keyword", source: "var value = 1;\n" + strings.Repeat("// ordinary source line\n", 256)},
		{name: "keyword-noise", source: "// DistributedApplication AddProject\nconst string value = \"Projects.Fake\";\n" + strings.Repeat("var n = 1;\n", 128)},
		{name: "malformed-directive", source: "#if FEATURE\nvar builder = DistributedApplication.CreateBuilder(args);\nbuilder.AddProject<Projects.Api>(\"api\");\n", wantError: true},
		{name: "local-alias-negative", source: "using DistributedApplication = Fake.App;\nvar builder = DistributedApplication.CreateBuilder(args);\nbuilder.AddProject<Projects.Api>(\"api\");\n"},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			sourceBytes := []byte(tc.source)
			defs, found, err := parseAspireAppHost("src/AppHost/Program.cs", sourceBytes)
			if (err != nil) != tc.wantError || found != tc.wantFound || len(defs) > 0 && len(defs[0].References) != tc.wantRefs {
				b.Fatalf("invalid benchmark fixture: found=%t defs=%+v err=%v", found, defs, err)
			}
			if tc.wantRefs == 0 && len(defs) != 0 {
				b.Fatalf("unexpected definitions in benchmark fixture: %+v", defs)
			}
			if tc.wantFound {
				if len(defs) != 1 || defs[0].Provider != "aspire-apphost" || defs[0].Name != "AppHost" || defs[0].Kind != "service" {
					b.Fatalf("unexpected positive definition identity: %+v", defs)
				}
				identity := defs[0]
				identity.Path = "src/AppHost/Program.cs"
				if stableID(identity) != "service:aspire-apphost:src/AppHost/Program.cs#AppHost" {
					b.Fatalf("unexpected stable definition ID: %q", stableID(identity))
				}
				wantRefs := make([]Reference, 0, tc.wantRefs)
				for i := 0; i < tc.wantRefs; i++ {
					name := fmt.Sprintf("Service%d", i)
					wantRefs = append(wantRefs, Reference{Kind: "aspire_project", Value: name, Qualification: "declared", Evidence: Evidence{Field: "AddProject", Value: name, Line: i + 3, Basis: "aspire-csharp-top-level-static"}})
				}
				if !reflect.DeepEqual(defs[0].References, wantRefs) {
					b.Fatalf("unexpected positive references:\n got: %#v\nwant: %#v", defs[0].References, wantRefs)
				}
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(sourceBytes)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _, _ = parseAspireAppHost("src/AppHost/Program.cs", sourceBytes)
			}
		})
	}
}
