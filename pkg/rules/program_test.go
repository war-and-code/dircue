package rules

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

const simpleConfig = `{"schema_version":"1.0.0","rules":[{"id":"go-module","match":{"filenames":["go.mod"]}}]}`

func compileTest(t testing.TB, config string) *Program {
	t.Helper()
	p, err := Compile([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func encodedConfig(t testing.TB, rules []any) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"schema_version": ConfigVersion, "rules": rules})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func contentConfig(t testing.TB) *Program {
	return compileTest(t, `{"schema_version":"1.0.0","rules":[{"id":"config","match":{"extensions":[".xml"]}},{"id":"marker","match":{"extensions":[".xml"]},"content":{"contains_utf8":"<project>"}}]}`)
}

func TestCompileExactBytesAndImmutability(t *testing.T) {
	original := []byte(simpleConfig)
	p, err := Compile(original)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(original)
	if p.SHA256() != hex.EncodeToString(sum[:]) {
		t.Fatal("wrong exact-byte digest")
	}
	q := compileTest(t, " "+simpleConfig+"\n")
	if p.SHA256() == q.SHA256() {
		t.Fatal("whitespace must remain part of provenance")
	}
	for i := range original {
		original[i] = 'x'
	}
	ids := p.RuleIDs()
	ids[0] = "changed"
	decision, err := p.MatchMetadata(File{"src/go.mod", 0})
	if err != nil {
		t.Fatal(err)
	}
	if p.RuleCount() != 1 || decision.Matches[0].RuleID != "go-module" || p.RuleIDs()[0] != "go-module" {
		t.Fatal("caller mutation changed program")
	}
	decision.Matches[0].RuleID = "changed"
	next, _ := p.MatchMetadata(File{"src/go.mod", 0})
	if next.Matches[0].RuleID != "go-module" {
		t.Fatal("decision aliases program")
	}
}
func TestStrictConfigRejections(t *testing.T) {
	cases := map[string]string{
		"empty": "", "oversized": strings.Repeat(" ", MaxConfigBytes+1), "null": "null", "wrongversion": strings.Replace(simpleConfig, "1.0.0", "1", 1),
		"duplicate_root":    strings.Replace(simpleConfig, `"rules":`, `"schema_version":"1.0.0","rules":`, 1),
		"escaped_duplicate": strings.Replace(simpleConfig, `"rules":`, `"schema\u005fversion":"1.0.0","rules":`, 1),
		"case_alias":        strings.Replace(simpleConfig, `"rules"`, `"Rules"`, 1),
		"unknown":           strings.Replace(simpleConfig, `"rules":`, `"include":"elsewhere.json","rules":`, 1),
		"nested_unknown":    strings.Replace(simpleConfig, `"filenames":`, `"Filenames":["go.mod"],"filenames":`, 1),
		"no_rules":          `{"schema_version":"1.0.0","rules":[]}`,
		"null_rule":         `{"schema_version":"1.0.0","rules":[null]}`,
		"null_list":         strings.Replace(simpleConfig, `["go.mod"]`, `null`, 1),
		"empty_list":        strings.Replace(simpleConfig, `["go.mod"]`, `[]`, 1),
		"nonstring":         strings.Replace(simpleConfig, `"go.mod"`, `3`, 1),
		"no_selector":       strings.Replace(simpleConfig, `{"filenames":["go.mod"]}`, `{}`, 1),
		"duplicate_value":   strings.Replace(simpleConfig, `["go.mod"]`, `["go.mod","go.mod"]`, 1),
		"empty_id":          strings.Replace(simpleConfig, `"go-module"`, `""`, 1),
		"unpaired_high":     strings.Replace(simpleConfig, `"go.mod"`, `"\ud800"`, 1),
		"unpaired_low":      strings.Replace(simpleConfig, `"go.mod"`, `"\udc00"`, 1),
		"high_nonlow":       strings.Replace(simpleConfig, `"go.mod"`, `"\ud800\u0061"`, 1),
		"raw_utf8":          strings.Replace(simpleConfig, "go.mod", string([]byte{0xff}), 1),
		"trailing_document": simpleConfig + simpleConfig,
		"depth":             `{"schema_version":"1.0.0","rules":[[[[[[[[[[[1]]]]]]]]]]]}`,
		"absolute_name":     strings.Replace(simpleConfig, "go.mod", "/go.mod", 1),
		"windows_separator": strings.Replace(simpleConfig, "go.mod", `src\\go.mod`, 1),
		"control_selector":  strings.Replace(simpleConfig, "go.mod", `a\n`, 1),
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Compile([]byte(config))
			if !errors.Is(err, ErrConfig) {
				t.Fatalf("got %v", err)
			}
		})
	}
}
func TestUnicodeEscapes(t *testing.T) {
	valid := []string{`"\ud83d\ude00"`, `"\\ud800"`, `"\ufffd"`, `"é"`, `"\""`}
	invalid := []string{`"\ud800"`, `"\udc00"`, `"\ud800x"`, `"\ud800\ud800"`, `"\ud800\u"`}
	for _, value := range valid {
		if !strictJSON([]byte(value)) {
			t.Errorf("rejected valid %s", value)
		}
	}
	for _, value := range invalid {
		if strictJSON([]byte(value)) {
			t.Errorf("accepted invalid %s", value)
		}
	}
	p := compileTest(t, strings.Replace(simpleConfig, `"go.mod"`, `"\ud83d\ude00"`, 1))
	decision, err := p.MatchMetadata(File{"😀", 1})
	if err != nil || len(decision.Matches) != 1 {
		t.Fatalf("surrogate pair not preserved: %+v %v", decision, err)
	}
}
func TestConfigLimits(t *testing.T) {
	rule := func(id string, values []string) any {
		return map[string]any{"id": id, "match": map[string]any{"filenames": values}}
	}
	entries := make([]any, MaxRules)
	for i := range entries {
		entries[i] = rule(fmt.Sprintf("r%d", i), []string{"file"})
	}
	compileTest(t, encodedConfig(t, entries))
	tooMany := append(entries, rule("extra", []string{"file"}))
	if _, err := Compile([]byte(encodedConfig(t, tooMany))); !errors.Is(err, ErrConfig) {
		t.Fatal("accepted too many rules")
	}
	duplicate := append([]any{}, entries...)
	duplicate[1] = duplicate[0]
	if _, err := Compile([]byte(encodedConfig(t, duplicate))); !errors.Is(err, ErrConfig) {
		t.Fatal("accepted duplicate ID")
	}
	values := make([]string, MaxSelectorValues)
	for i := range values {
		values[i] = fmt.Sprintf("f%d", i)
	}
	compileTest(t, encodedConfig(t, []any{rule("r", values)}))
	if _, err := Compile([]byte(encodedConfig(t, []any{rule("r", append(values, "extra"))}))); !errors.Is(err, ErrConfig) {
		t.Fatal("accepted 65 selectors")
	}
	entries = make([]any, 64)
	for i := range entries {
		entries[i] = rule(fmt.Sprintf("r%d", i), values)
	}
	compileTest(t, encodedConfig(t, entries))
	if _, err := Compile([]byte(encodedConfig(t, append(entries, rule("extra", []string{"f"}))))); !errors.Is(err, ErrConfig) {
		t.Fatal("accepted 4097 total selectors")
	}
	for _, length := range []int{MaxLiteralBytes, MaxLiteralBytes + 1} {
		config := encodedConfig(t, []any{map[string]any{"id": "literal", "match": map[string]any{"filenames": []string{"file"}}, "content": map[string]any{"contains_utf8": strings.Repeat("x", length)}}})
		_, err := Compile([]byte(config))
		if (err != nil) != (length > MaxLiteralBytes) {
			t.Fatalf("literal length %d: %v", length, err)
		}
	}
	padded := simpleConfig + strings.Repeat(" ", MaxConfigBytes-len(simpleConfig))
	compileTest(t, padded)
}
func TestMetadataConjunctionSuffixAndCase(t *testing.T) {
	p := compileTest(t, `{"schema_version":"1.0.0","rules":[
 {"id":"typescript","match":{"filenames":["types.d.ts","other.d.ts"],"extensions":[".d.ts"],"path_prefixes":["src/","lib/"]}},
 {"id":"gradle","match":{"extensions":[".gradle.kts"]}},
 {"id":"ordinary","match":{"extensions":[".ts"]}},
 {"id":"literal","match":{"filenames":["*.go"]}},
 {"id":"prefix","match":{"path_prefixes":["vendor/acme/"]}}]}`)
	tests := []struct {
		path string
		ids  []string
	}{
		{"src/types.d.ts", []string{"ordinary", "typescript"}}, {"lib/other.d.ts", []string{"ordinary", "typescript"}},
		{"src/nomatch.d.ts", []string{"ordinary"}}, {"source/types.d.ts", []string{"ordinary"}},
		{"src/types.D.TS", []string{}}, {"build.gradle.kts", []string{"gradle"}},
		{"build.gradle.kt", []string{}}, {"*.go", []string{"literal"}}, {"main.go", []string{}},
		{"vendor/acme/file", []string{"prefix"}}, {"vendor/acme-other/file", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			d, err := p.MatchMetadata(File{tt.path, 1})
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for _, m := range d.Matches {
				ids = append(ids, m.RuleID)
			}
			if !reflect.DeepEqual(ids, tt.ids) {
				t.Fatalf("got %v want %v", ids, tt.ids)
			}
		})
	}
}
func TestInvalidPathsAndSizes(t *testing.T) {
	p := compileTest(t, simpleConfig)
	for _, name := range []string{"", ".", "..", "../file", "src/../../file", "/file", `C:/file`, `C:\file`, "src//file", "src/./file", "src/file/", string([]byte{0xff}), "a\x00b", strings.Repeat("x", MaxPathBytes+1)} {
		if _, err := p.MatchMetadata(File{name, 0}); !errors.Is(err, ErrFile) {
			t.Errorf("path %q accepted: %v", name, err)
		}
	}
	if _, err := p.MatchMetadata(File{"go.mod", -1}); !errors.Is(err, ErrFile) {
		t.Fatal("negative size accepted")
	}
}
func TestCompleteContentAndPrivacy(t *testing.T) {
	p := contentConfig(t)
	body := []byte("\ufeff<project>private token never emitted</project>")
	before := bytes.Clone(body)
	file := File{"p.xml", int64(len(body))}
	observations, err := p.MatchContent(file, body)
	if err != nil || len(observations) != 1 {
		t.Fatalf("%+v %v", observations, err)
	}
	sum := sha256.Sum256(body)
	if observations[0].SourceSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("digest does not bind all source bytes")
	}
	encoded, _ := json.Marshal(observations)
	if strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "project") {
		t.Fatal("source or literal leaked")
	}
	if !bytes.Equal(body, before) {
		t.Fatal("input mutated")
	}
	negatives, err := p.MatchContent(File{"p.xml", 0}, nil)
	if err != nil || len(negatives) != 0 {
		t.Fatal("empty complete negative failed")
	}
	if _, err := p.MatchContent(file, body[:len(body)-1]); !errors.Is(err, ErrIncomplete) {
		t.Fatal(err)
	}
	if _, err := p.MatchContent(File{"p.xml", 1}, []byte{0xff}); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatal(err)
	}
	if _, err := p.MatchContent(File{"p.xml", MaxContentBytes + 1}, nil); !errors.Is(err, ErrContentTooLarge) {
		t.Fatal(err)
	}
	if _, err := p.MatchContent(File{"p.go", 0}, nil); !errors.Is(err, ErrNotCandidate) {
		t.Fatal(err)
	}
	maximal := bytes.Repeat([]byte("x"), MaxContentBytes)
	copy(maximal[len(maximal)-9:], "<project>")
	if matches, err := p.MatchContent(File{"p.xml", MaxContentBytes}, maximal); err != nil || len(matches) != 1 {
		t.Fatalf("boundary match failed: %v", err)
	}
}
func TestProgramConcurrent(t *testing.T) {
	p := contentConfig(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				d, err := p.MatchMetadata(File{"a.xml", 9})
				if err != nil || !d.NeedsContent() {
					t.Error("metadata mismatch")
				}
				m, err := p.MatchContent(File{"a.xml", 9}, []byte("<project>"))
				if err != nil || len(m) != 1 {
					t.Error("content mismatch")
				}
			}
		}()
	}
	wg.Wait()
}
func FuzzCompile(f *testing.F) {
	f.Add([]byte(simpleConfig))
	f.Add([]byte(strings.Replace(simpleConfig, `"go.mod"`, `"\ud800"`, 1)))
	f.Fuzz(func(t *testing.T, data []byte) {
		before := bytes.Clone(data)
		p, err := Compile(data)
		if !bytes.Equal(before, data) {
			t.Fatal("mutated input")
		}
		if err != nil {
			return
		}
		if len(data) > MaxConfigBytes || p.RuleCount() < 1 || p.RuleCount() > MaxRules {
			t.Fatal("invalid successful bounds")
		}
		q, err := Compile(data)
		if err != nil || q.SHA256() != p.SHA256() || !reflect.DeepEqual(q.RuleIDs(), p.RuleIDs()) {
			t.Fatal("nondeterministic compile")
		}
		_, _ = p.MatchMetadata(File{"src/go.mod", 0})
	})
}
func BenchmarkMetadataUnmatched(b *testing.B) {
	p := compileTest(b, simpleConfig)
	file := File{"src/main.go", 1024}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.MatchMetadata(file)
	}
}
func BenchmarkCompoundMetadata(b *testing.B) {
	p := compileTest(b, `{"schema_version":"1.0.0","rules":[{"id":"type","match":{"extensions":[".d.ts"]}}]}`)
	file := File{"src/types.d.ts", 1024}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.MatchMetadata(file)
	}
}
