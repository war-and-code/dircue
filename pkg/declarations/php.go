package declarations

import (
	"regexp"
	"unicode/utf8"
)

// ParsePHP reads composer.json. It never executes PHP code.
// Extracts the package name from the "name" field (vendor/package form).
func ParsePHP(name string, content []byte) *Document {
	d := NewDocument(name, "php-composer")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-php-composer", "composer.json exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	raw, err := ValidateJSON(content)
	if err != nil {
		d.Parsed = false
		AddDiagnostic(d, "invalid-php-composer", "composer.json could not be parsed within JSON limits.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "composer-json-v2", State: "declared", Evidence: name})
	if n, ok := raw["name"].(string); ok && phpNameOK(n) {
		d.Project.Name = n
		AddRequirement(d, Requirement{Kind: "php-package-name", Value: n, State: "declared", Evidence: name})
	} else {
		AddDiagnostic(d, "missing-php-package-name", "composer.json has no supported static name field.")
	}
	if v, ok := raw["version"].(string); ok && len(v) <= MaxStringBytes {
		d.Project.Version = v
	}
	// Collect require entries as declared dependencies.
	if reqs, ok := raw["require"].(map[string]any); ok {
		for pkg := range reqs {
			if d.limited {
				break
			}
			if phpNameOK(pkg) || pkg == "php" {
				AddRequirement(d, Requirement{Kind: "php-dependency", Value: pkg, State: "declared", Evidence: name})
			}
		}
	}
	return d
}

var phpNameRE = regexp.MustCompile(`^[a-z0-9_-]+/[a-z0-9_.-]+$`)

func phpNameOK(s string) bool {
	return len(s) > 0 && len(s) <= MaxStringBytes && phpNameRE.MatchString(s)
}
