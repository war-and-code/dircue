package declarations

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// ParseHaskell reads *.cabal or stack.yaml manifests. It never executes Haskell code.
// *.cabal: extracts `name:` from the package stanza.
// stack.yaml: records a stack-project component (no name field; dir-derived).
func ParseHaskell(name string, content []byte) *Document {
	base := baseName(name)
	if strings.HasSuffix(base, ".cabal") {
		return parseCabal(name, content)
	}
	return parseStackYAML(name, content)
}

func parseCabal(name string, content []byte) *Document {
	d := NewDocument(name, "haskell-cabal")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-haskell-cabal", "*.cabal exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "cabal-file-static-v1", State: "declared", Evidence: name})
	if m := cabalNameRE.FindSubmatch(content); m != nil {
		n := strings.TrimSpace(string(m[1]))
		if haskellNameOK(n) {
			d.Project.Name = n
			AddRequirement(d, Requirement{Kind: "haskell-package-name", Value: n, State: "declared", Evidence: name})
		}
	} else {
		AddDiagnostic(d, "missing-haskell-package-name", "A static name: field was not found in *.cabal.")
	}
	if m := cabalVersionRE.FindSubmatch(content); m != nil {
		ver := strings.TrimSpace(string(m[1]))
		if len(ver) <= MaxStringBytes {
			d.Project.Version = ver
		}
	}
	return d
}

func parseStackYAML(name string, content []byte) *Document {
	d := NewDocument(name, "haskell-stack")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-haskell-stack", "stack.yaml exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "stack-yaml-static-v1", State: "declared", Evidence: name})
	AddDiagnostic(d, "haskell-stack-name-from-directory", "stack.yaml has no package name field; the component name derives from its directory.")
	return d
}

// cabalNameRE matches `name:   pkg-name` at the start of a cabal stanza header,
// optionally indented, case-insensitive because Cabal is case-insensitive for field names.
var cabalNameRE = regexp.MustCompile(`(?im)^name:\s*([A-Za-z][A-Za-z0-9_-]{0,128})`)
var cabalVersionRE = regexp.MustCompile(`(?im)^version:\s*([0-9][A-Za-z0-9._-]{0,63})`)

func haskellNameOK(s string) bool {
	return s != "" && len(s) <= MaxStringBytes
}
