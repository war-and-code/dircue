package componentmap

import (
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
)

// A parent found by directory position is a local edge only when the child's
// declared parent coordinates name that POM.
func TestMavenParentEdgeRequiresMatchingCoordinates(t *testing.T) {
	for _, tc := range []struct {
		name, parent, version string
		wantCoverage          string
		wantReason            string
	}{
		{"matching coordinates", "org.example:root:1.0", "1.0", "complete", ""},
		{"different artifact", "org.springframework.boot:spring-boot-starter-parent:3.3.0", "1.0", "", "parent_coordinates_mismatch"},
		{"case differs", "org.example:Root:1.0", "1.0", "", "parent_coordinates_mismatch"},
		{"different version", "org.example:root:2.0", "1.0", "", "parent_version_mismatch"},
		{"property version on target", "org.example:root:1.0", "${revision}", "complete", ""},
		{"property coordinates on child", "${parent.group}:root:1.0", "1.0", "partial", ""},
		{"property version on child", "org.example:root:${revision}", "1.0", "partial", ""},
		{"coordinates left to Maven 4 inference", "::", "1.0", "partial", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := &declarations.Report{Status: "complete", Projects: []declarations.Project{
				{ID: "pom.xml", Root: ".", Kind: "maven", Requirements: []declarations.Requirement{
					{Kind: "maven-groupId", Value: "org.example", State: "declared"},
					{Kind: "maven-artifactId", Value: "root", State: "declared"},
					{Kind: "maven-version", Value: tc.version, State: "declared"},
				}},
				{ID: "app/pom.xml", Root: "app", Kind: "maven",
					Requirements: []declarations.Requirement{
						{Kind: "maven-parent", Value: tc.parent, State: "declared"},
						{Kind: "maven-artifactId", Value: "app", State: "declared"},
					},
					References: []declarations.Reference{{Kind: "parent", Value: "../pom.xml", Target: "pom.xml", TargetStatus: "present", State: "declared", Evidence: "app/pom.xml"}},
				},
			}}
			f := Build(report)
			coverage := ""
			for _, r := range f.Relationships {
				if r.DeclarationKind == "parent" {
					coverage = r.Coverage
				}
			}
			reason := ""
			for _, q := range f.QualifiedReferences {
				if q.DeclarationKind == "parent" {
					reason = q.Reason
				}
			}
			if coverage != tc.wantCoverage || reason != tc.wantReason {
				t.Fatalf("parent edge coverage = %q, qualified reason = %q; want %q, %q", coverage, reason, tc.wantCoverage, tc.wantReason)
			}
		})
	}
}
