package reportdiff

import (
	"bytes"
	"context"
	"crypto/sha256"
	"dircue/pkg/declarations"
	"dircue/pkg/discovery"
	"dircue/pkg/profile"
	"dircue/pkg/projects"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func baseJSON(t *testing.T) []byte {
	t.Helper()
	data, err := json.Marshal(emptyProfile())
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestLoadSupportedVersionsAndExactByteIdentity(t *testing.T) {
	for _, version := range []string{"1.0.0", "1.1.0", "1.2.0", "1.3.0", "1.4.0"} {
		p := emptyProfile()
		p.SchemaVersion = version
		switch version {
		case "1.1.0":
			p.Metrics = &profile.MetricsReport{Engine: "scc", EngineVersion: "4.1.0", Status: "complete", Scope: "source", Source: "directory", MaxFileBytes: 16777216, Languages: []profile.LanguageMetrics{}, Directories: []profile.DirectoryMetrics{}, Skipped: []profile.MetricSkip{}}
		case "1.2.0":
			p.Projects = projects.New("directory", "").Finish()
		case "1.3.0":
			p.Discovery = discovery.New("directory", "", 100000).Finish()
		case "1.4.0":
			var err error
			p.Declarations, err = declarations.New("directory", "", 0).Finish(context.Background())
			if err != nil {
				t.Fatal(err)
			}
		}
		input, _ := json.Marshal(p)
		input = append([]byte(" \n"), input...)
		snapshot, err := Load(bytes.NewReader(input))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(input)
		if snapshot.sha256 != hex.EncodeToString(digest[:]) || snapshot.bytes != int64(len(input)) {
			t.Fatal("digest did not cover exact saved bytes")
		}
	}
}

func TestLoadRejectsMalformedAmbiguousAndUnsupportedReports(t *testing.T) {
	valid := string(baseJSON(t))
	for name, input := range map[string]string{
		"duplicate top key":   strings.Replace(valid, `"schema_version":"1.0.0"`, `"schema_version":"1.0.0","schema_version":"1.0.0"`, 1),
		"escaped duplicate":   strings.Replace(valid, `"schema_version":"1.0.0"`, `"schema_version":"1.0.0","schema_\u0076ersion":"1.0.0"`, 1),
		"case alias":          strings.Replace(valid, `"schema_version"`, `"Schema_Version"`, 1),
		"nested alias":        strings.Replace(valid, `"scanned_files"`, `"Scanned_Files"`, 1),
		"nested duplicate":    strings.Replace(valid, `"scanned_files":0`, `"scanned_files":0,"scanned_files":1`, 1),
		"unknown field":       strings.Replace(valid, `"scanned_files":0`, `"scanned_files":0,"surprise":1`, 1),
		"missing required":    strings.Replace(valid, `"scanned_files":0,`, "", 1),
		"negative count":      strings.Replace(valid, `"scanned_files":0`, `"scanned_files":-1`, 1),
		"fraction count":      strings.Replace(valid, `"scanned_files":0`, `"scanned_files":0.5`, 1),
		"overflow count":      strings.Replace(valid, `"scanned_files":0`, `"scanned_files":9223372036854775808`, 1),
		"null array":          strings.Replace(valid, `"languages":[]`, `"languages":null`, 1),
		"surrogate":           strings.Replace(valid, `"root":"/unopened/source"`, `"root":"\ud800"`, 1),
		"unsupported version": strings.Replace(valid, "1.0.0", "9.0.0", 1),
		"trailing value":      valid + ` {}`,
		"language-only":       `{"Go":{"size":100,"percentage":"100.00"}}`,
		"array":               `[]`,
		"empty":               ``,
		"invalid utf8":        string([]byte{0xff}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(strings.NewReader(input)); err == nil {
				t.Fatalf("accepted %s", name)
			}
		})
	}
	if _, err := Load(nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("nil reader accepted")
	}
}

func TestLoadRejectsOversizeDeepAndNodeHeavyJSON(t *testing.T) {
	for name, input := range map[string]string{
		"bytes":  strings.Repeat(" ", MaxInputBytes+1),
		"depth":  strings.Repeat("[", MaxJSONDepth+2) + "0" + strings.Repeat("]", MaxJSONDepth+2),
		"string": `{"x":"` + strings.Repeat("a", MaxStringBytes+1) + `"}`,
		"nodes":  "[" + strings.Repeat("0,", MaxJSONNodes) + "0]",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(strings.NewReader(input)); !errors.Is(err, ErrLimit) {
				t.Fatalf("expected limit, got %v", err)
			}
		})
	}
}

func TestLoadSchemaCannotUnderstateOptionalCapabilities(t *testing.T) {
	p := declarationProfile(t, map[string]string{"go.mod": "module example.org/app\ngo 1.24.0\n"})
	p.SchemaVersion = "1.0.0"
	data, _ := json.Marshal(p)
	if _, err := Load(bytes.NewReader(data)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("old schema accepted new module: %v", err)
	}
}

func TestCompareRejectsConstructedUnvalidatedSnapshots(t *testing.T) {
	if _, err := Compare(&Snapshot{}, &Snapshot{}); !errors.Is(err, ErrInvalid) {
		t.Fatal("unvalidated snapshot accepted")
	}
}

func FuzzSavedReportLoad(f *testing.F) {
	input, _ := json.Marshal(emptyProfile())
	f.Add(input)
	f.Add([]byte(`{"schema_version":"1.0.0","schema_version":"1.4.0"}`))
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > MaxInputBytes+1 {
			return
		}
		snapshot, err := Load(bytes.NewReader(input))
		if err != nil {
			return
		}
		report, err := Compare(snapshot, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		for _, module := range report.Modules {
			if module.Counts.Changed+module.Counts.Added+module.Counts.Removed != 0 {
				t.Fatal("snapshot differs from itself")
			}
		}
		output, err := json.Marshal(report)
		if err != nil || len(output) > MaxOutputBytes {
			t.Fatal("invalid or unbounded comparison output")
		}
	})
}
