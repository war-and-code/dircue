package declarations

import (
	"regexp"
	"unicode/utf8"
)

// ParseScala reads build.sbt manifests. It never executes Scala/sbt code.
// Extracts the project name from a static `name := "Literal"` assignment.
func ParseScala(name string, content []byte) *Document {
	d := NewDocument(name, "scala-sbt")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-scala-sbt", "build.sbt exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "build-sbt-static-v1", State: "declared", Evidence: name})
	if m := sbtNameRE.FindSubmatch(content); m != nil {
		n := string(m[1])
		if sbtNameOK(n) {
			d.Project.Name = n
			AddRequirement(d, Requirement{Kind: "scala-project-name", Value: n, State: "declared", Evidence: name})
		}
	} else {
		AddDiagnostic(d, "missing-scala-project-name", "A static name := \"Literal\" was not found in build.sbt; the file was not executed.")
	}
	if m := sbtVersionRE.FindSubmatch(content); m != nil {
		ver := string(m[1])
		if len(ver) <= MaxStringBytes {
			d.Project.Version = ver
		}
	}
	return d
}

var sbtNameRE = regexp.MustCompile(`(?m)^\s*(?:ThisBuild\s*/\s*)?name\s*:=\s*"([^"]{1,128})"`)
var sbtVersionRE = regexp.MustCompile(`(?m)^\s*(?:ThisBuild\s*/\s*)?version\s*:=\s*"([^"]{1,64})"`)

func sbtNameOK(s string) bool {
	return s != "" && len(s) <= MaxStringBytes
}
