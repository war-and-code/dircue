package cli

import "testing"

func TestCountLinesMatchesLinguistByteEncodingSemantics(t *testing.T) {
	encode := func(bom []byte, width int, bigEndian bool, text string) []byte {
		result := append([]byte(nil), bom...)
		for _, value := range []byte(text) {
			encoded := make([]byte, width)
			if bigEndian {
				encoded[width-1] = value
			} else {
				encoded[0] = value
			}
			result = append(result, encoded...)
		}
		return result
	}

	tests := []struct {
		name        string
		content     []byte
		lines, sloc int
	}{
		{"empty", nil, 0, 0},
		{"one LF", []byte("\n"), 0, 0},
		{"one CR", []byte("\r"), 0, 0},
		{"one CRLF", []byte("\r\n"), 0, 0},
		{"ASCII blank line", []byte("a\n\n"), 2, 1},
		{"UTF-16LE", encode([]byte{0xff, 0xfe}, 2, false, "a\n\n"), 3, 1},
		{"UTF-16BE", encode([]byte{0xfe, 0xff}, 2, true, "a\n\n"), 2, 2},
		{"UTF-32LE", encode([]byte{0xff, 0xfe, 0, 0}, 4, false, "a\n\n"), 3, 1},
		{"UTF-32BE", encode([]byte{0, 0, 0xfe, 0xff}, 4, true, "a\n\n"), 2, 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lines, sloc := countLines(test.content)
			if lines != test.lines || sloc != test.sloc {
				t.Fatalf("got %d lines/%d sloc, want %d/%d", lines, sloc, test.lines, test.sloc)
			}
		})
	}
}
