package formats

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func hasEvidence(o Observation, format, basis string) bool {
	for _, e := range o.Evidence {
		if e.Format == format && e.Basis == basis {
			return true
		}
	}
	return false
}
func hasDiagnostic(o Observation, code string) bool {
	for _, v := range o.Diagnostics {
		if v == code {
			return true
		}
	}
	return false
}
func TestInspectStructuredEvidence(t *testing.T) {
	for _, tt := range []struct {
		name, data, format, basis, diagnostic string
		complete                              bool
	}{
		{"a.json", `{"value":[true,null,1e999999999]}`, "json", "complete_validation", "", true},
		{"a.json", `{"a":1,"a":2}`, "json", "complete_validation", "", true},
		{"a.json", `"scalar"`, "json", "complete_validation", "", true},
		{"data", `{"valid":"yes"}`, "json", "complete_validation", "", true},
		{"a.json", `{"unfinished":[`, "json", "parsed_prefix", "json_prefix_only", false},
		{"a.json", `{"unfinished":[`, "json", "", "json_invalid_syntax", true},
		{"a.json", `{"x":"\ud800"}`, "json", "", "json_invalid_or_incomplete_unicode", true},
		{"a.json", "{\"x\":\"\xff\"}", "json", "", "json_invalid_or_incomplete_unicode", true},
		{"a.json", `{} {}`, "json", "", "json_multiple_values", true},
		{"a.xml", "<?xml version=\"1.0\"?><root><child/></root>", "xml", "complete_validation", "", true},
		{"a.xml", "<root><child/>", "xml", "parsed_prefix", "xml_prefix_only", false},
		{"a.xml", "<root><child/>", "xml", "", "xml_invalid_or_unsupported_syntax", true},
		{"a.xml", "<root/><other/>", "xml", "", "xml_multiple_roots", true},
		{"a.xml", "outside<root/>", "xml", "", "xml_text_outside_root", true},
		{"a.xml", "<root/>outside", "xml", "", "xml_text_outside_root", true},
		{"a.xml", `<!DOCTYPE x SYSTEM "file:///etc/passwd"><x/>`, "xml", "", "xml_dtd_or_directive_unsupported", true},
		{"a.xml", `<x a="1" a="2"/>`, "xml", "", "xml_duplicate_attribute", true},
		{"a.xml", `<x/><?xml version="1.0"?>`, "xml", "", "xml_invalid_declaration_position", true},
		{"a.xml", `<x>&notdefined;</x>`, "xml", "", "xml_invalid_or_unsupported_syntax", true},
	} {
		t.Run(tt.name+tt.data, func(t *testing.T) {
			o := inspect(tt.name, []byte(tt.data), tt.complete)
			if tt.basis != "" && !hasEvidence(o, tt.format, tt.basis) {
				t.Fatalf("missing evidence: %+v", o)
			}
			if tt.basis == "" && (hasEvidence(o, tt.format, "complete_validation") || hasEvidence(o, tt.format, "parsed_prefix")) {
				t.Fatalf("unexpected validation: %+v", o)
			}
			if tt.diagnostic != "" && !hasDiagnostic(o, tt.diagnostic) {
				t.Fatalf("missing diagnostic %s: %+v", tt.diagnostic, o)
			}
		})
	}
}
func TestXMLDeclarationAndWhitespaceProfile(t *testing.T) {
	for _, valid := range []string{`<?xml version="1.0"?><x/>`, `<?xml version='1.0' encoding='utf-8' standalone='yes'?><x/>`, "\t\r\n <x/> \r\n"} {
		if !hasEvidence(inspect("x.xml", []byte(valid), true), "xml", "complete_validation") {
			t.Fatalf("valid profile rejected: %q", valid)
		}
	}
	for _, invalid := range []string{"\u00a0<x/>", "<x/>\u00a0", "<x/><![CDATA[ ]]>", "&#32;<x/>", "<x/>&#32;", `<?xml?><x/>`, `<?xml version="1.1"?><x/>`, `<?xml version="1.0" garbage="yes"?><x/>`, `<?xml version="1.0" version="1.0"?><x/>`, " <?xml version=\"1.0\"?><x/>", `<?xml version="1.0"?><?xml version="1.0"?><x/>`, `<!-- hi --><?xml version="1.0"?><x/>`} {
		if hasEvidence(inspect("x.xml", []byte(invalid), true), "xml", "complete_validation") {
			t.Fatalf("invalid profile accepted: %q", invalid)
		}
	}
}

func TestDepthAndNoContentDisclosure(t *testing.T) {
	for _, data := range []string{strings.Repeat("[", 65) + strings.Repeat("]", 65), strings.Repeat("<x>", 65) + strings.Repeat("</x>", 65)} {
		o := inspect("blob", []byte(data), true)
		if len(o.Diagnostics) == 0 {
			t.Fatalf("missing depth diagnostic: %+v", o)
		}
	}
	for _, data := range []string{`{"secret-name":"secret-value"}`, `<secret-name>secret-value</secret-name>`, `{"invalid":"secret-value"!}`, `<secret-name secret-attribute="secret-value">`} {
		o := inspect("blob", []byte(data), true)
		b, _ := json.Marshal(o)
		if bytes.Contains(b, []byte("secret")) {
			t.Fatalf("payload disclosed: %s", b)
		}
	}
}
func TestSignaturesAndHintsDoNotValidateArchives(t *testing.T) {
	for _, tt := range []struct{ name, data, format string }{
		{"misleading.xml", "PK\x03\x04", "zip"}, {"x.jar", "PK\x05\x06", "zip"}, {"x", "\x1f\x8b\x08", "gzip"}, {"x", "\x7fELF", "elf"}, {"x", "%PDF-", "pdf"}, {"x", "SQLite format 3\x00", "sqlite"}, {"x", "\x89PNG\r\n\x1a\n", "png"}, {"x", "\xff\xd8\xff", "jpeg"}, {"x", "7z\xbc\xaf\x27\x1c", "7z"},
	} {
		o := inspect(tt.name, []byte(tt.data), true)
		if !hasEvidence(o, tt.format, "signature_match") || hasEvidence(o, tt.format, "complete_validation") {
			t.Fatalf("signature: %+v", o)
		}
	}
	o := inspect("not-really.zip", []byte("hello"), true)
	if !hasEvidence(o, "zip", "extension_hint") || hasEvidence(o, "zip", "signature_match") {
		t.Fatalf("hint: %+v", o)
	}
	pe := make([]byte, 100)
	copy(pe, "MZ")
	pe[60] = 80
	copy(pe[80:], "PE\x00\x00")
	if !hasEvidence(inspect("x", pe, true), "pe", "signature_match") {
		t.Fatal("PE header missing")
	}
	pe[60] = 255
	if hasEvidence(inspect("x", pe, true), "pe", "signature_match") {
		t.Fatal("out-of-bounds PE header accepted")
	}
}
func TestTextScope(t *testing.T) {
	for _, tt := range []struct {
		data           []byte
		complete, want bool
	}{
		{[]byte("hello"), true, true}, {[]byte("hello"), false, true}, {[]byte("hello\xff"), false, false}, {[]byte("hello\x00"), true, false}, {[]byte("hello\xe2\x82"), false, true}, {[]byte("hello\xe2\x82"), true, false}, {[]byte{}, true, false},
	} {
		o := inspect("x", tt.data, tt.complete)
		got := hasEvidence(o, "text", "complete_validation") || hasEvidence(o, "text", "parsed_prefix")
		if got != tt.want {
			t.Fatalf("data=%x complete=%v %+v", tt.data, tt.complete, o)
		}
	}
}
func FuzzInspect(f *testing.F) {
	for _, s := range []string{"", `{"a":[1,true,null]}`, `{"x":"\ud800"}`, `<x><y/></x>`, `<!DOCTYPE x [<!ENTITY x "y">]><x/>`, "PK\x03\x04", "\xff", strings.Repeat("[", 65)} {
		f.Add(s, true)
	}
	f.Fuzz(func(t *testing.T, s string, complete bool) {
		if len(s) > int(MaxFileBytes) {
			t.Skip()
		}
		name := "input.json"
		if strings.HasPrefix(s, "<") {
			name = "input.xml"
		}
		a := inspect(name, []byte(s), complete)
		b := inspect(name, []byte(s), complete)
		if !reflect.DeepEqual(a, b) {
			t.Fatal("nondeterministic")
		}
		out, err := json.Marshal(a)
		if err != nil || !json.Valid(out) || len(out) > 4096 {
			t.Fatalf("invalid or unbounded output: %v %d", err, len(out))
		}
		for _, e := range a.Evidence {
			if e.Basis == "complete_validation" && !complete {
				t.Fatal("prefix promoted to validation")
			}
			if e.Format == "json" && e.Basis == "complete_validation" && !json.Valid([]byte(s)) {
				t.Fatal("invalid JSON accepted")
			}
		}
	})
}
