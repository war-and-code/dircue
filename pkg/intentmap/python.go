package intentmap

import (
	"bytes"
	"regexp"
	"strings"
)

// Python framework entry-point detection.
//
// Flask application detection uses two signal tiers:
//
//  1. Module-level assignment: a variable at column 0 is assigned the result
//     of a Flask(...) constructor call.  This is the canonical Flask idiom:
//
//         app = Flask(__name__)
//         application = Flask(__name__)
//
//     The variable name is extracted and used as the interface name.
//
//  2. The file must not be under a test path.  Flask apps are sometimes
//     constructed in test fixtures, but those constructions do not represent
//     the primary application entry point.
//
// Only the first occurrence per file is retained; that is sufficient to
// declare the primary entry point.  Both Python 2-style and Python 3-style
// declarations are recognised. Decorators and class-level assignments are
// not matched because they are indented in typical usage.
//
// Coverage is always partial because the launch mechanism (gunicorn,
// uwsgi, flask run, a shell script) is not statically parsed.

var (
	// flaskAppAssign matches a module-level variable = Flask(...) assignment.
	// The variable name is in capture group 1.
	// Pattern: at the start of a line (^), an identifier ([A-Za-z_][A-Za-z0-9_]*),
	// optional whitespace, =, optional whitespace, Flask(, then anything.
	// The Flask( may be preceded by optional type annotation (:...) before the =.
	flaskAppAssign = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_]*)\s*(?::[^=]*)?\s*=\s*Flask\s*\(`)
)

// parsePythonFlaskApp detects module-level Flask application declarations in
// Python source files. It returns at most one KindInterface observation per
// file. Test paths are excluded to avoid false positives from test fixtures.
//
// A module-level Flask(...) assignment is a strong static signal: it is the
// idiom used by Flask apps to register the WSGI application object. Shell-
// script launch commands (flask run, gunicorn) are out of scope for this
// detector because shell files are not parsed.
func parsePythonFlaskApp(filePath string, content []byte) []Observation {
	// Skip test sources. Flask app objects constructed in test files are
	// fixtures, not the primary application entry point.
	lower := strings.ToLower(filePath)
	if strings.Contains(lower, "/tests/") ||
		strings.HasPrefix(lower, "tests/") ||
		strings.Contains(lower, "/test/") ||
		strings.HasPrefix(lower, "test/") ||
		strings.HasSuffix(lower, "_test.py") ||
		strings.HasPrefix(lower, "conftest.py") {
		return nil
	}

	m := flaskAppAssign.FindSubmatchIndex(content)
	if m == nil {
		return nil
	}
	// m[2]:m[3] is the variable name capture group.
	varName := string(content[m[2]:m[3]])
	if varName == "" {
		return nil
	}

	// Count the line number of the match start.
	line := 1 + bytes.Count(content[:m[0]], []byte("\n"))

	return []Observation{
		{
			Kind:      KindInterface,
			Name:      varName,
			State:     "declared",
			Basis:     "code_syntax",
			Path:      filePath,
			StartLine: line,
			EndLine:   line,
			Properties: map[string]string{
				"interface_kind": "flask_application",
			},
		},
	}
}
