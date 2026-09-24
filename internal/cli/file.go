package cli

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/war-and-code/dircue/pkg/scanner"
)

//go:embed mimedata/ext_mime.db
var mimeDatabase string

var mimeTypes = sync.OnceValue(func() map[string]string {
	result := make(map[string]string)
	for line := range strings.SplitSeq(mimeDatabase, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			result[fields[0]] = fields[1]
		}
	}
	return result
})

func fileMIME(filename string) string {
	extension := strings.TrimPrefix(filepath.Ext(filename), ".")
	if value := mimeTypes()[extension]; value != "" {
		return value
	}
	if value := mimeTypes()[strings.ToLower(extension)]; value != "" {
		return value
	}
	return "text/plain"
}

type fileOutput struct {
	Lines     int     `json:"lines"`
	SLOC      int     `json:"sloc"`
	Type      string  `json:"type"`
	MIME      string  `json:"mime_type"`
	Language  *string `json:"language"`
	Large     bool    `json:"large"`
	Generated bool    `json:"generated"`
	Vendored  bool    `json:"vendored"`
}

func runFile(cmd *cobra.Command, filename string, opts *options) error {
	blob, err := scanner.Inspect(cmd.Context(), filename, scanner.Options{Source: opts.source, Revision: opts.revision, MaxFileBytes: opts.maxFileBytes, IncludeStrategies: opts.strategies})
	if err != nil {
		return err
	}
	result := fileOutput{Type: "Text", MIME: fileMIME(filename), Large: blob.Size > 1<<20, Generated: blob.Generated, Vendored: blob.Vendored}
	if blob.Binary {
		result.Type = "Binary"
		switch strings.ToLower(filepath.Ext(filename)) {
		case ".png", ".jpg", ".jpeg", ".gif":
			result.Type = "Image"
		}
	}
	if blob.Language != "" {
		result.Language = &blob.Language
	}
	if !result.Large && !blob.Binary {
		result.Lines, result.SLOC = countLines(blob.Content)
	}
	out := cmd.OutOrStdout()
	if opts.json {
		return json.NewEncoder(out).Encode(map[string]fileOutput{filename: result})
	}
	if _, err := fmt.Fprintf(out, "%s: %d lines (%d sloc)\n  type:      %s\n  mime type: %s\n  language:  %s\n", terminalValue(filename), result.Lines, result.SLOC, result.Type, result.MIME, blob.Language); err != nil {
		return err
	}
	if opts.strategies && blob.Language != "" && blob.Strategy != "" {
		if _, err := fmt.Fprintf(out, "  strategy:  %s\n", blob.Strategy); err != nil {
			return err
		}
	}
	for _, notice := range []struct {
		enabled bool
		text    string
	}{{result.Large, "  blob is too large to be shown"}, {result.Generated, "  appears to be generated source code"}, {result.Vendored, "  appears to be a vendored file"}} {
		if notice.enabled {
			if _, err := fmt.Fprintln(out, notice.text); err != nil {
				return err
			}
		}
	}
	return nil
}

// Linguist keeps blob data as bytes. It removes one trailing ASCII line ending,
// then splits on CRLF, CR, and LF encoded in the detected character encoding.
// Its whitespace test also runs against the resulting byte slices. These steps
// reproduce Linguist's UTF-16/32 line counts.
func countLines(content []byte) (lines, sloc int) {
	content = chompBytes(content)
	if len(content) == 0 {
		return 0, 0
	}
	newlines := encodedNewlines(content)
	for {
		end, width := nextNewline(content, newlines)
		if end < 0 {
			end = len(content)
		}
		lines++
		if len(bytes.Trim(content[:end], " \t\r\n\v\f")) > 0 {
			sloc++
		}
		if width == 0 {
			return lines, sloc
		}
		content = content[end+width:]
	}
}

func chompBytes(content []byte) []byte {
	if bytes.HasSuffix(content, []byte{'\r', '\n'}) {
		return content[:len(content)-2]
	}
	if len(content) > 0 && (content[len(content)-1] == '\r' || content[len(content)-1] == '\n') {
		return content[:len(content)-1]
	}
	return content
}

func encodedNewlines(content []byte) [][]byte {
	switch {
	case bytes.HasPrefix(content, []byte{0xff, 0xfe, 0, 0}):
		return [][]byte{{'\r', 0, 0, 0, '\n', 0, 0, 0}, {'\r', 0, 0, 0}, {'\n', 0, 0, 0}}
	case bytes.HasPrefix(content, []byte{0, 0, 0xfe, 0xff}):
		return [][]byte{{0, 0, 0, '\r', 0, 0, 0, '\n'}, {0, 0, 0, '\r'}, {0, 0, 0, '\n'}}
	case bytes.HasPrefix(content, []byte{0xff, 0xfe}):
		return [][]byte{{'\r', 0, '\n', 0}, {'\r', 0}, {'\n', 0}}
	case bytes.HasPrefix(content, []byte{0xfe, 0xff}):
		return [][]byte{{0, '\r', 0, '\n'}, {0, '\r'}, {0, '\n'}}
	default:
		return [][]byte{{'\r', '\n'}, {'\r'}, {'\n'}}
	}
}

func nextNewline(content []byte, newlines [][]byte) (offset, width int) {
	offset = -1
	for _, newline := range newlines {
		candidate := bytes.Index(content, newline)
		if candidate >= 0 && (offset < 0 || candidate < offset) {
			offset, width = candidate, len(newline)
		}
	}
	return offset, width
}
