package declarations

import (
	"regexp"
	"unicode/utf8"
)

// ParseSwift reads Package.swift manifests. It never executes Swift code.
// Extracts the name from a static `name: "Literal"` argument.
func ParseSwift(name string, content []byte) *Document {
	d := NewDocument(name, "swift-package")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-swift-package", "Package.swift exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "swift-package-manifest-v1", State: "declared", Evidence: name})
	if m := swiftPackageNameRE.FindSubmatch(content); m != nil {
		n := string(m[1])
		if swiftNameOK(n) {
			d.Project.Name = n
			AddRequirement(d, Requirement{Kind: "swift-package-name", Value: n, State: "declared", Evidence: name})
		}
	} else {
		AddDiagnostic(d, "missing-swift-package-name", "A static name: \"Literal\" was not found in Package.swift; the file was not executed.")
	}
	return d
}

// Match `name: "SomeName"` allowing optional whitespace; stop at newline to avoid greedy cross-line matching.
var swiftPackageNameRE = regexp.MustCompile(`(?m)\bname:\s*"([A-Za-z0-9][A-Za-z0-9_.-]{0,127})"`)

func swiftNameOK(s string) bool {
	return s != "" && len(s) <= MaxStringBytes
}
