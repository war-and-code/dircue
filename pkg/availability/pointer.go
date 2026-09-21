package availability

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const currentLFSVersion = "https://git-lfs.github.com/spec/v1"

var (
	oidPattern       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	extPattern       = regexp.MustCompile(`^ext-([0-9]+)-([A-Za-z0-9_]+)$`)
	keyPattern       = regexp.MustCompile(`^[a-z0-9.-]+$`)
	knownLFSVersions = map[string]bool{
		"http://git-media.io/v/2":           true,
		"https://hawser.github.com/spec/v1": true,
		currentLFSVersion:                   true,
	}
)

type PointerInspection struct {
	Kind                string
	Reason              string
	Version             string
	OIDAlgorithm        string
	OIDDigest           string
	DeclaredObjectBytes int64
	Canonical           bool
	ExtensionCount      int
}

// InspectLFSPointer recognizes the bounded Git LFS pointer representation.
// OIDDigest identifies the referenced LFS object; it is not a digest computed
// from the selected file or its Git blob.
func InspectLFSPointer(content []byte, fullSize int64) PointerInspection {
	like := pointerLike(content)
	if fullSize >= PointerSizeCutoff {
		if like {
			return PointerInspection{Kind: "pointer_like", Reason: "size_cutoff"}
		}
		return PointerInspection{}
	}
	if fullSize < 0 || int64(len(content)) != fullSize {
		if like {
			return PointerInspection{Kind: "pointer_like", Reason: "incomplete_content"}
		}
		return PointerInspection{}
	}
	if len(content) == 0 || !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		if like {
			return PointerInspection{Kind: "pointer_like", Reason: "invalid_text"}
		}
		return PointerInspection{}
	}
	parsed, reason := parsePointer(content)
	if reason != "" {
		if like {
			return PointerInspection{Kind: "pointer_like", Reason: reason}
		}
		return PointerInspection{}
	}
	return parsed
}

type pointerLine struct{ key, value string }

func parsePointer(content []byte) (PointerInspection, string) {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 {
		return PointerInspection{}, "invalid_header"
	}
	rawLines := bytes.Split(trimmed, []byte{'\n'})
	lines := make([]pointerLine, 0, len(rawLines))
	for _, raw := range rawLines {
		if len(raw) == 0 { // git-lfs' non-strict decoder ignores blank lines.
			continue
		}
		at := bytes.IndexByte(raw, ' ')
		if at <= 0 || at == len(raw)-1 {
			return PointerInspection{}, "invalid_line"
		}
		key, value := string(raw[:at]), string(raw[at+1:])
		if !keyPattern.MatchString(key) || strings.ContainsAny(value, "\r\n") {
			return PointerInspection{}, "invalid_line"
		}
		lines = append(lines, pointerLine{key, value})
	}
	if len(lines) < 3 || lines[0].key != "version" {
		return PointerInspection{}, "invalid_header"
	}
	if !knownLFSVersions[lines[0].value] {
		return PointerInspection{}, "unsupported_version"
	}
	inspection := PointerInspection{Kind: "valid_pointer", Version: lines[0].value}
	seen := map[string]bool{"version": true}
	extPriorities := map[int]bool{}
	previous := ""
	for _, line := range lines[1:] {
		if seen[line.key] {
			return PointerInspection{}, "duplicate_key"
		}
		seen[line.key] = true
		if previous != "" && line.key <= previous {
			return PointerInspection{}, "keys_out_of_order"
		}
		previous = line.key
		switch line.key {
		case "oid":
			parts := strings.SplitN(line.value, ":", 2)
			if len(parts) != 2 || parts[0] != "sha256" || !oidPattern.MatchString(parts[1]) {
				return PointerInspection{}, "invalid_oid"
			}
			inspection.OIDAlgorithm, inspection.OIDDigest = parts[0], parts[1]
		case "size":
			size, err := strconv.ParseInt(line.value, 10, 64)
			if err != nil || size < 0 || strconv.FormatInt(size, 10) != line.value {
				return PointerInspection{}, "invalid_size"
			}
			inspection.DeclaredObjectBytes = size
		default:
			match := extPattern.FindStringSubmatch(line.key)
			if match == nil {
				return PointerInspection{}, "unsupported_key"
			}
			priority, _ := strconv.Atoi(match[1])
			if extPriorities[priority] {
				return PointerInspection{}, "duplicate_extension_priority"
			}
			extPriorities[priority] = true
			parts := strings.SplitN(line.value, ":", 2)
			if len(parts) != 2 || parts[0] != "sha256" || !oidPattern.MatchString(parts[1]) {
				return PointerInspection{}, "invalid_extension_oid"
			}
			inspection.ExtensionCount++
		}
	}
	if inspection.OIDDigest == "" || !seen["size"] {
		return PointerInspection{}, "missing_required_key"
	}
	inspection.Canonical = string(content) == canonicalPointer(inspection, lines)
	return inspection, ""
}

func canonicalPointer(p PointerInspection, lines []pointerLine) string {
	// Git LFS passes empty files through unchanged; its canonical encoder emits
	// no pointer text for a zero-byte object.
	if p.DeclaredObjectBytes == 0 {
		return ""
	}
	exts := make([]pointerLine, 0, p.ExtensionCount)
	for _, line := range lines {
		if strings.HasPrefix(line.key, "ext-") {
			exts = append(exts, line)
		}
	}
	sort.Slice(exts, func(i, j int) bool { return exts[i].key < exts[j].key })
	var b strings.Builder
	fmt.Fprintf(&b, "version %s\n", currentLFSVersion)
	for _, ext := range exts {
		fmt.Fprintf(&b, "%s %s\n", ext.key, ext.value)
	}
	fmt.Fprintf(&b, "oid sha256:%s\nsize %d\n", p.OIDDigest, p.DeclaredObjectBytes)
	return b.String()
}

func pointerLike(content []byte) bool {
	content = content[:min(len(content), int(PointerSizeCutoff))]
	return bytes.HasPrefix(content, []byte("version https://git-lfs.github.com/spec/")) ||
		bytes.HasPrefix(content, []byte("version https://hawser.github.com/spec/")) ||
		bytes.HasPrefix(content, []byte("version http://git-media.io/")) ||
		(bytes.HasPrefix(content, []byte("version ")) && bytes.Contains(content, []byte("\noid sha256:")))
}
