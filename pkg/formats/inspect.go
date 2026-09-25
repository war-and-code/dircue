package formats

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/war-and-code/dircue/internal/jsontext"
)

func extension(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".xml", ".xsd", ".xsl", ".xslt", ".svg", ".csproj", ".fsproj", ".vbproj", ".props", ".targets":
		return "xml"
	case ".json", ".ipynb":
		return "json"
	case ".zip", ".jar", ".war", ".ear", ".whl", ".nupkg", ".docx", ".xlsx", ".pptx":
		return "zip"
	case ".gz", ".tgz":
		return "gzip"
	case ".7z":
		return "7z"
	case ".pdf":
		return "pdf"
	case ".png":
		return "png"
	case ".jpg", ".jpeg":
		return "jpeg"
	case ".sqlite", ".sqlite3", ".db":
		return "sqlite"
	case ".exe", ".dll":
		return "pe"
	case ".wasm":
		return "wasm"
	case ".class":
		return "java_class"
	case ".tar":
		return "tar"
	case ".dylib":
		return "mach_o"
	case ".txt", ".md", ".csv", ".tsv", ".log":
		return "text"
	}
	return ""
}

// inspect emits fixed detector labels only; payload values, XML names and parser errors are never included.
func inspect(name string, data []byte, complete bool) Observation {
	out := Observation{Path: name, Evidence: []Evidence{}, Diagnostics: []string{}}
	add := func(format, basis, validation string) {
		out.Evidence = append(out.Evidence, Evidence{format, basis, validation})
	}
	hint := extension(name)
	if hint != "" {
		add(hint, "extension_hint", "")
	}
	if len(data) == 0 {
		if complete {
			add("empty", "complete_validation", "zero_bytes")
		}
		return out
	}
	for _, signature := range []struct {
		format string
		magic  []byte
	}{
		{"zip", []byte{'P', 'K', 3, 4}}, {"zip", []byte{'P', 'K', 5, 6}}, {"zip", []byte{'P', 'K', 7, 8}},
		{"gzip", []byte{0x1f, 0x8b, 8}}, {"7z", []byte{0x37, 0x7a, 0xbc, 0xaf, 0x27, 0x1c}},
		{"elf", []byte{0x7f, 'E', 'L', 'F'}}, {"pdf", []byte("%PDF-")},
		{"png", []byte{137, 80, 78, 71, 13, 10, 26, 10}}, {"jpeg", []byte{0xff, 0xd8, 0xff}},
		{"sqlite", []byte("SQLite format 3\x00")},
		{"wasm", []byte{'\x00', 'a', 's', 'm', '\x01', '\x00', '\x00', '\x00'}},
		{"mach_o", []byte{0xfe, 0xed, 0xfa, 0xce}}, {"mach_o", []byte{0xce, 0xfa, 0xed, 0xfe}},
		{"mach_o", []byte{0xfe, 0xed, 0xfa, 0xcf}}, {"mach_o", []byte{0xcf, 0xfa, 0xed, 0xfe}},
		{"mach_o_fat", []byte{0xbe, 0xba, 0xfe, 0xca}},
	} {
		if bytes.HasPrefix(data, signature.magic) {
			add(signature.format, "signature_match", "header_magic_only")
			break
		}
	}
	if len(data) >= 8 && bytes.Equal(data[:4], []byte{0xca, 0xfe, 0xba, 0xbe}) {
		minor := uint16(data[4])<<8 | uint16(data[5])
		major := uint16(data[6])<<8 | uint16(data[7])
		if (minor == 0 || minor == 0xffff) && major >= 45 && major <= 100 {
			add("java_class", "signature_match", "header_magic_and_plausible_version_only")
		} else {
			add("mach_o_fat", "signature_match", "header_magic_only")
		}
	}
	if len(data) >= 262 && bytes.Equal(data[257:262], []byte("ustar")) {
		add("tar", "signature_match", "header_magic_only")
	}
	// MZ alone identifies a DOS header. A PE match also requires its referenced signature inside the prefix.
	if bytes.HasPrefix(data, []byte("MZ")) {
		add("dos-executable", "signature_match", "header_magic_only")
		if len(data) >= 64 {
			n := uint64(data[60]) | uint64(data[61])<<8 | uint64(data[62])<<16 | uint64(data[63])<<24
			if n+4 <= uint64(len(data)) && bytes.Equal(data[n:n+4], []byte{'P', 'E', 0, 0}) {
				add("pe", "signature_match", "referenced_pe_magic_only")
			}
		}
	}
	textData := data
	// A prefix may end in the middle of a UTF-8 character. Only inspect its complete code points.
	if !complete {
		textData = completeUTF8Prefix(textData)
	}
	if utf8.Valid(textData) && len(textData) > 0 {
		plain := true
		for _, r := range string(textData) {
			if (r < 32 && r != '\n' && r != '\r' && r != '\t' && r != '\f') || r == 127 {
				plain = false
				break
			}
		}
		if plain {
			basis := "parsed_prefix"
			if complete {
				basis = "complete_validation"
			}
			add("text", basis, "utf8_without_disallowed_controls")
		}
	}
	trimmed := bytes.TrimSpace(data)
	xmlData := bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	xmlTrimmed := bytes.TrimSpace(xmlData)
	if hint == "xml" || bytes.HasPrefix(xmlTrimmed, []byte("<")) {
		basis, diagnostic := inspectXML(xmlData, complete)
		if basis != "" {
			add("xml", basis, "utf8_single_root_token_syntax_no_dtd")
		}
		if diagnostic != "" {
			out.Diagnostics = append(out.Diagnostics, diagnostic)
		}
	}
	if hint == "json" || bytes.HasPrefix(trimmed, []byte("{")) || bytes.HasPrefix(trimmed, []byte("[")) {
		basis, diagnostic := inspectJSON(data, complete)
		if basis != "" {
			add("json", basis, "utf8_json_syntax_duplicate_keys_allowed")
		}
		if diagnostic != "" {
			out.Diagnostics = append(out.Diagnostics, diagnostic)
		}
	}
	return out
}

func inspectJSON(data []byte, complete bool) (string, string) {
	// ValidUnicode also rejects incomplete quoted strings; a conservative prefix result is preferable to repaired text.
	if !jsontext.ValidUnicode(data) {
		return "", "json_invalid_or_incomplete_unicode"
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	depth, tokens, roots := 0, 0, 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			if complete && depth == 0 && roots == 1 && json.Valid(data) {
				return "complete_validation", ""
			}
			if !complete && tokens > 0 {
				return "parsed_prefix", "json_prefix_only"
			}
			return "", "json_invalid_syntax"
		}
		if err != nil {
			if !complete && tokens > 0 && errors.Is(err, io.ErrUnexpectedEOF) {
				return "parsed_prefix", "json_prefix_only"
			}
			return "", "json_invalid_or_incomplete_syntax"
		}
		tokens++
		if tokens > MaxTokens {
			return "", "json_token_limit"
		}
		if depth == 0 {
			roots++
			if roots > 1 {
				return "", "json_multiple_values"
			}
		}
		if d, ok := token.(json.Delim); ok {
			switch d {
			case '{', '[':
				depth++
				if depth > MaxDepth {
					return "", "json_depth_limit"
				}
			case '}', ']':
				depth--
			}
		}
	}
}

func inspectXML(data []byte, complete bool) (string, string) {
	if !utf8.Valid(data) {
		return "", "xml_invalid_or_incomplete_utf8"
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth, tokens, roots := 0, 0, 0
	for {
		offset := decoder.InputOffset()
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			if complete && roots == 1 && depth == 0 {
				return "complete_validation", ""
			}
			if !complete && roots == 1 {
				return "parsed_prefix", "xml_prefix_only"
			}
			return "", "xml_invalid_syntax"
		}
		if err != nil {
			if !complete && roots == 1 && strings.Contains(err.Error(), "unexpected EOF") {
				return "parsed_prefix", "xml_prefix_only"
			}
			return "", "xml_invalid_or_unsupported_syntax"
		}
		tokens++
		if tokens > MaxTokens {
			return "", "xml_token_limit"
		}
		switch t := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
				if roots > 1 {
					return "", "xml_multiple_roots"
				}
			}
			depth++
			if depth > MaxDepth {
				return "", "xml_depth_limit"
			}
			attrs := map[xml.Name]bool{}
			for _, a := range t.Attr {
				if attrs[a.Name] {
					return "", "xml_duplicate_attribute"
				}
				attrs[a.Name] = true
			}
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && len(bytes.Trim(data[offset:decoder.InputOffset()], " \t\r\n")) != 0 {
				return "", "xml_text_outside_root"
			}
		case xml.Directive:
			return "", "xml_dtd_or_directive_unsupported"
		case xml.ProcInst:
			if strings.EqualFold(t.Target, "xml") {
				if t.Target != "xml" || tokens != 1 {
					return "", "xml_invalid_declaration_position"
				}
				if !xmlDeclaration().Match(t.Inst) {
					return "", "xml_invalid_or_unsupported_declaration"
				}
			}
		}
	}
}

// completeUTF8Prefix removes only a genuinely incomplete terminal UTF-8 rune.
func completeUTF8Prefix(data []byte) []byte {
	for i := len(data) - 1; i >= 0 && i >= len(data)-utf8.UTFMax; i-- {
		if data[i]&0xc0 != 0x80 {
			if !utf8.FullRune(data[i:]) {
				return data[:i]
			}
			break
		}
	}
	return data
}

// XML 1.0 UTF-8 declarations are the only declaration profile supported here.
var xmlDeclaration = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`^version[ \t\r\n]*=[ \t\r\n]*(?:"1\.0"|'1\.0')(?:[ \t\r\n]+encoding[ \t\r\n]*=[ \t\r\n]*(?:"(?i:utf-8)"|'(?i:utf-8)'))?(?:[ \t\r\n]+standalone[ \t\r\n]*=[ \t\r\n]*(?:"(?:yes|no)"|'(?:yes|no)'))?[ \t\r\n]*$`)
})
