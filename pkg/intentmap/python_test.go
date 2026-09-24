package intentmap

import (
	"testing"
)

// TestParsePythonFlaskApp verifies Flask application detection.
func TestParsePythonFlaskApp(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		content string
		wantLen int
		wantVar string
	}{
		{
			name:    "canonical app = Flask(__name__)",
			path:    "app.py",
			content: "from flask import Flask\napp = Flask(__name__)\n",
			wantLen: 1,
			wantVar: "app",
		},
		{
			name:    "application variable name",
			path:    "wsgi.py",
			content: "from flask import Flask\napplication = Flask(__name__)\n",
			wantLen: 1,
			wantVar: "application",
		},
		{
			name:    "with type annotation",
			path:    "app.py",
			content: "from flask import Flask\napp: Flask = Flask(__name__)\n",
			wantLen: 1,
			wantVar: "app",
		},
		{
			name:    "indented assignment is not matched",
			path:    "app.py",
			content: "def create_app():\n    app = Flask(__name__)\n    return app\n",
			wantLen: 0,
		},
		{
			name:    "class subclass not matched",
			path:    "app.py",
			content: "class MyApp(Flask):\n    pass\n",
			wantLen: 0,
		},
		{
			name:    "test file excluded by path",
			path:    "tests/unit/test_app.py",
			content: "app = Flask(__name__)\n",
			wantLen: 0,
		},
		{
			name:    "test file excluded by prefix",
			path:    "test/conftest.py",
			content: "app = Flask(__name__)\n",
			wantLen: 0,
		},
		{
			name:    "_test.py suffix excluded",
			path:    "myapp_test.py",
			content: "app = Flask(__name__)\n",
			wantLen: 0,
		},
		{
			name:    "no Flask construction",
			path:    "app.py",
			content: "from flask import Flask\n# no app here\n",
			wantLen: 0,
		},
		{
			name:    "only first occurrence returned",
			path:    "multi.py",
			content: "app1 = Flask(__name__)\napp2 = Flask(__name__)\n",
			wantLen: 1,
			wantVar: "app1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obs := parsePythonFlaskApp(tt.path, []byte(tt.content))
			if len(obs) != tt.wantLen {
				t.Errorf("parsePythonFlaskApp(%q) got %d observations, want %d", tt.path, len(obs), tt.wantLen)
				return
			}
			if tt.wantLen == 0 {
				return
			}
			got := obs[0]
			if got.Kind != KindInterface {
				t.Errorf("got Kind=%q, want %q", got.Kind, KindInterface)
			}
			if got.Properties["interface_kind"] != "flask_application" {
				t.Errorf("got interface_kind=%q, want flask_application", got.Properties["interface_kind"])
			}
			if got.Name != tt.wantVar {
				t.Errorf("got Name=%q, want %q", got.Name, tt.wantVar)
			}
			if got.State != "declared" {
				t.Errorf("got State=%q, want declared", got.State)
			}
			if got.Basis != "code_syntax" {
				t.Errorf("got Basis=%q, want code_syntax", got.Basis)
			}
			if got.Path != tt.path {
				t.Errorf("got Path=%q, want %q", got.Path, tt.path)
			}
			if got.StartLine == 0 {
				t.Errorf("got StartLine=0, want > 0")
			}
		})
	}
}

// TestSQLiteImportCapability verifies that sqlite3 import produces datastore:sqlite.
func TestSQLiteImportCapability(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		content    string
		wantCap    string
		wantNoCaps []string
	}{
		{
			name:    "top-level import sqlite3",
			path:    "helpers/db_sqlite.py",
			content: "import sqlite3\n\ncon = sqlite3.connect('db.sqlite3')\n",
			wantCap: "datastore:relational",
		},
		{
			name:    "from sqlite3 import",
			path:    "helpers/db.py",
			content: "from sqlite3 import connect\n",
			wantCap: "datastore:relational",
		},
		{
			name:       "indented import not detected",
			path:       "helpers/db.py",
			content:    "def setup():\n    import sqlite3\n    return sqlite3.connect(':memory:')\n",
			wantNoCaps: []string{"datastore:relational"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obs := parsePythonImports(tt.path, []byte(tt.content))
			capNames := make(map[string]bool)
			for _, o := range obs {
				if o.Kind == KindCapability {
					capNames[o.Name] = true
				}
			}
			if tt.wantCap != "" && !capNames[tt.wantCap] {
				t.Errorf("parsePythonImports(%q) did not produce capability %q; got %v", tt.path, tt.wantCap, capNames)
			}
			for _, noCap := range tt.wantNoCaps {
				if capNames[noCap] {
					t.Errorf("parsePythonImports(%q) unexpectedly produced capability %q", tt.path, noCap)
				}
			}
		})
	}
}

// TestSQLiteCatalogEntry verifies catalog entries for SQLite.
func TestSQLiteCatalogEntry(t *testing.T) {
	cases := []struct {
		kind  string
		value string
	}{
		{"python-import", "sqlite3"},
		{"python-import", "aiosqlite"},
		{"python-dependency", "aiosqlite"},
		{"python-build-requirement", "aiosqlite"},
	}
	for _, tc := range cases {
		caps := capabilitiesFor(tc.kind, tc.value)
		found := false
		for _, c := range caps {
			if c == "datastore:relational" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("capabilitiesFor(%q, %q) did not return datastore:relational; got %v", tc.kind, tc.value, caps)
		}
	}
}
