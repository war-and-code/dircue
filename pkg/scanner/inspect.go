package scanner

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf16"

	enry "github.com/go-enry/go-enry/v2"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// DetectLanguage uses Enry's default strategy sequence and reports the stage
// that resolved the language. The labels match Linguist CLI diagnostics.
func DetectLanguage(filename string, content []byte) (string, string) {
	if isBinaryContent(content) || len(content) == 0 {
		return "", ""
	}
	names := []string{"Modeline", "Filename", "Shebang", "Extension", "XML", "Manpage", "Heuristics", "Classifier"}
	var languages []string
	last := ""
	for i, strategy := range enry.DefaultStrategies {
		candidates := strategy(filename, content, languages)
		if len(candidates) == 0 {
			continue
		}
		languages = candidates
		last = names[i]
		if len(candidates) == 1 {
			return candidates[0], last
		}
	}
	if len(languages) > 0 {
		return languages[0], last
	}
	return "", ""
}

// Inspection contains a bounded source view suitable for single-file CLI
// diagnostics. Size is the complete byte size; Git Content is at most 128 KiB,
// while a Git-free file of at most 1 MiB includes its full content for line counts.
type Inspection struct {
	Path          string
	Size          int64
	Content       []byte
	Language      string
	Strategy      string
	Generated     bool
	Vendored      bool
	Documentation bool
	Binary        bool
}

// Inspect reads a committed blob when a Git repository is discovered, otherwise
// a regular local file. It never follows a target symlink or executes Git hooks.
func Inspect(ctx context.Context, filename string, opts Options) (out *Inspection, returnErr error) {
	if opts.MaxFileBytes < 0 || opts.MaxFileBytes == int64(^uint64(0)>>1) {
		return nil, fmt.Errorf("max file bytes must be between zero and MaxInt64-1")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	full, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	if opts.Source == "" {
		opts.Source = "auto"
	}
	if opts.Source != "auto" && opts.Source != "git" && opts.Source != "directory" {
		return nil, fmt.Errorf("unknown source %q", opts.Source)
	}
	if opts.Revision != "" && opts.Tree != "" {
		return nil, errors.New("revision and tree are mutually exclusive")
	}
	if strings.Contains(opts.Revision, ":") {
		return nil, errors.New("revision must select a commit, not a rev:path expression")
	}
	if opts.Source == "directory" && (opts.Revision != "" || opts.Tree != "") {
		return nil, fmt.Errorf("revision and tree require Git source")
	}
	snapshot, err := openGitSnapshot(ctx, filepath.Dir(full), opts, true, 1)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := snapshot.close(); returnErr == nil && closeErr != nil {
			out = nil
			returnErr = fmt.Errorf("close Git storage: %w", closeErr)
		}
	}()
	var attrs overrides
	var content []byte
	var size int64
	var relative string
	if snapshot != nil {
		canonicalParent, resolveErr := filepath.EvalSymlinks(filepath.Dir(full))
		if resolveErr != nil {
			return nil, resolveErr
		}
		canonicalRoot, resolveErr := filepath.EvalSymlinks(snapshot.root)
		if resolveErr != nil {
			return nil, resolveErr
		}
		relative, err = filepath.Rel(canonicalRoot, filepath.Join(canonicalParent, filepath.Base(full)))
		if err != nil {
			return nil, err
		}
		relative = filepath.ToSlash(relative)
		entry, err := snapshot.findEntry(relative)
		if err != nil {
			return nil, fmt.Errorf("file %s is absent from Git revision: %w", relative, err)
		}
		if entry.Mode != filemode.Regular && entry.Mode != filemode.Executable && entry.Mode != filemode.Deprecated {
			return nil, fmt.Errorf("inspect %s: not a regular Git file", relative)
		}
		blob, err := object.GetBlob(snapshot.storage, entry.Hash)
		if err != nil {
			return nil, fmt.Errorf("read Git blob %s: %w", relative, err)
		}
		if opts.MaxFileBytes > 0 && blob.Size > opts.MaxFileBytes {
			return nil, fmt.Errorf("file %s exceeds %d byte limit", relative, opts.MaxFileBytes)
		}
		content, size, err = blobRead(blob, relative, ClassificationBytes)
		if err != nil {
			return nil, err
		}
		var rules []attributeRule
		attributeRuleCount := len(snapshot.infoRules)
		parents := strings.Split(path.Dir(relative), "/")
		scope := ""
		for depth := 0; depth <= len(parents); depth++ {
			if depth > 0 {
				if parents[depth-1] == "." {
					continue
				}
				scope = path.Join(scope, parents[depth-1])
			}
			attrPath := path.Join(scope, ".gitattributes")
			entry, readErr := snapshot.findEntry(attrPath)
			if errors.Is(readErr, object.ErrEntryNotFound) || errors.Is(readErr, object.ErrDirectoryNotFound) {
				continue
			}
			if readErr != nil {
				return nil, fmt.Errorf("inspect attribute file %s: %w", attrPath, readErr)
			}
			if entry.Mode != filemode.Regular && entry.Mode != filemode.Executable && entry.Mode != filemode.Deprecated {
				continue
			}
			blob, readErr := object.GetBlob(snapshot.storage, entry.Hash)
			if readErr != nil {
				return nil, readErr
			}
			if blob.Size > maxAttributesBytes {
				return nil, fmt.Errorf("attribute file %s exceeds 1 MiB", attrPath)
			}
			data, _, readErr := blobRead(blob, attrPath, maxAttributesBytes)
			if readErr != nil {
				return nil, readErr
			}
			parsed, _, exceeded := parseGitAttributesBounded(attrPath, data, maxAttributeRules-attributeRuleCount)
			if exceeded {
				return nil, fmt.Errorf("attribute rules exceed %d rule limit", maxAttributeRules)
			}
			attributeRuleCount += len(parsed)
			rules = append(rules, parsed...)
		}
		attrs, err = resolveAttributesContext(ctx, relative, append(rules, snapshot.infoRules...))
		if err != nil {
			return nil, err
		}
	} else {
		root, err := os.OpenRoot(filepath.Dir(full))
		if err != nil {
			return nil, err
		}
		defer root.Close()
		relative = filepath.Base(full)
		info, err := root.Lstat(relative)
		if err != nil {
			return nil, err
		}
		size = info.Size()
		if opts.MaxFileBytes > 0 && size > opts.MaxFileBytes {
			return nil, fmt.Errorf("file %s exceeds %d byte limit", relative, opts.MaxFileBytes)
		}
		limit := int64(1 << 20)
		if opts.MaxFileBytes > 0 {
			limit = min(limit, opts.MaxFileBytes)
		}
		var tooLarge bool
		content, tooLarge, size, err = readBoundedSize(root, relative, limit)
		if err == nil && opts.MaxFileBytes > 0 && (size > opts.MaxFileBytes || (limit == opts.MaxFileBytes && tooLarge)) {
			return nil, fmt.Errorf("file %s exceeds %d byte limit", relative, opts.MaxFileBytes)
		}
		if err != nil {
			return nil, err
		}
		if len(content) > 1<<20 {
			content = content[:ClassificationBytes]
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := &Inspection{Path: relative, Size: size, Content: content, Binary: isBinaryContent(content)}
	result.Language, result.Strategy = DetectLanguage(path.Base(relative), content)
	result.Language, result.Strategy = applyLanguageOverride(result.Language, result.Strategy, attrs)
	result.Generated = overrideBool(attrs.generated, enry.IsGenerated(relative, content))
	result.Vendored = overrideBool(attrs.vendored, enry.IsVendor(relative))
	result.Documentation = overrideBool(attrs.documentation, enry.IsDocumentation(relative))
	return result, nil
}

func applyLanguageOverride(language, strategy string, attrs overrides) (string, string) {
	if !attrs.languageSet {
		return language, strategy
	}
	if strategy == "" {
		strategy = "Unknown"
	}
	action := "overridden"
	if language == attrs.language {
		action = "confirmed"
	}
	return attrs.language, strategy + " (" + action + " by .gitattributes)"
}

// TextContent decodes UTF-16/32 with a BOM, which is text despite embedded NULs.
// It returns the original slice for other encodings. Enry otherwise uses
// Git's NUL-byte heuristic; decode these unambiguous encodings before detection.
func TextContent(content []byte) []byte {
	var order binary.ByteOrder
	offset := 2
	width := 2
	if bytes.HasPrefix(content, []byte{0xff, 0xfe, 0, 0}) {
		order = binary.LittleEndian
		width = 4
		offset = 4
	} else if bytes.HasPrefix(content, []byte{0, 0, 0xfe, 0xff}) {
		order = binary.BigEndian
		width = 4
		offset = 4
	} else if bytes.HasPrefix(content, []byte{0xff, 0xfe}) {
		order = binary.LittleEndian
	} else if bytes.HasPrefix(content, []byte{0xfe, 0xff}) {
		order = binary.BigEndian
	} else {
		return content
	}
	if width == 4 {
		var out []rune
		for i := offset; i+3 < len(content); i += 4 {
			out = append(out, rune(order.Uint32(content[i:i+4])))
		}
		return []byte(string(out))
	}
	values := make([]uint16, 0, (len(content)-offset)/2)
	for i := offset; i+1 < len(content); i += 2 {
		values = append(values, order.Uint16(content[i:i+2]))
	}
	return []byte(string(utf16.Decode(values)))
}

func hasWideTextBOM(content []byte) bool {
	return bytes.HasPrefix(content, []byte{0, 0, 0xfe, 0xff}) ||
		bytes.HasPrefix(content, []byte{0xff, 0xfe, 0, 0}) ||
		bytes.HasPrefix(content, []byte{0xfe, 0xff}) ||
		bytes.HasPrefix(content, []byte{0xff, 0xfe})
}

// isBinaryContent follows Charlock Holmes 0.7.9's binary preflight, used by
// Linguist 9.7. Callers implementing Git LazyBlob semantics supply its bounded
// 128 KiB data; FileBlob's detector scans at most 1 MiB for NUL bytes even when
// given a larger file. Known image signatures are binary, while PostScript and
// BOM-marked UTF-16/32 bypass the NUL heuristic as text.
func isBinaryContent(content []byte) bool {
	if bytes.HasPrefix(content, []byte("%!PS-Adobe-")) {
		return false
	}
	for _, signature := range [][]byte{
		{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'},
		{'G', 'I', 'F', '8', '7', 'a'},
		{'G', 'I', 'F', '8', '9', 'a'},
		{'%', 'P', 'D', 'F', '-'},
		{0xff, 0xd8, 0xff},
	} {
		if bytes.HasPrefix(content, signature) {
			return true
		}
	}
	if hasWideTextBOM(content) {
		return false
	}
	const binaryScanBytes = 1 << 20
	return bytes.IndexByte(content[:min(len(content), binaryScanBytes)], 0) >= 0
}

var lfsSizePattern = regexp.MustCompile(`\nsize [0-9]+\n?$`)

func isLFSPointer(content []byte) bool {
	return bytes.HasPrefix(content, []byte("version https://git-lfs.github.com/spec/v1\n")) && bytes.Contains(content, []byte("\noid sha256:")) && lfsSizePattern.Match(content)
}
