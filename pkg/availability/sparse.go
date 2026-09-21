package availability

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io/fs"
	"strings"
	"unicode/utf8"
)

type SparseMetadataParse struct {
	Complete           bool
	Indications        []SparseIndication
	Diagnostics        []Diagnostic
	OmittedIndications int64
	OmittedDiagnostics int64
}

// InspectSparseConfig recognizes only local boolean indications relevant to
// sparse checkout. Includes and malformed relevant values make the result
// incomplete; include targets are never opened here.
func InspectSparseConfig(evidence string, content []byte, fullSize, maxBytes int64) SparseMetadataParse {
	result := SparseMetadataParse{Complete: true, Indications: []SparseIndication{}, Diagnostics: []Diagnostic{}}
	diagnostic := func(code, message string) {
		result.Complete = false
		if len(result.Diagnostics) < DefaultDiagnosticLimit {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Path: evidence, Code: code, Message: message})
		} else {
			result.OmittedDiagnostics++
		}
	}
	if maxBytes <= 0 {
		maxBytes = DefaultGitmodulesBytes
	}
	if fullSize > maxBytes {
		diagnostic("git-config-size-limit", fmt.Sprintf("The local Git configuration exceeds the %d byte metadata limit.", maxBytes))
		return result
	}
	if fullSize < 0 || int64(len(content)) != fullSize {
		diagnostic("incomplete-git-config", "The local Git configuration was incomplete or changed during inspection.")
		return result
	}
	values := map[string]bool{}
	section := ""
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 4096), DefaultStringBytes+1)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimSuffix(scanner.Text(), "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && !strings.HasSuffix(line, "]") {
			diagnostic("unsupported-git-config-syntax", "A local Git configuration section header could not be interpreted.")
			section = ""
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			body := strings.TrimSpace(line[1 : len(line)-1])
			if at := strings.IndexAny(body, " \t\""); at >= 0 {
				body = body[:at]
			}
			section = strings.ToLower(body)
			if section == "include" || section == "includeif" {
				diagnostic("unsupported-git-config-include", "Local Git configuration includes are not followed for sparse metadata.")
			}
			continue
		}
		at := strings.IndexAny(line, "= \t")
		key, value := line, ""
		if at >= 0 {
			if at == 0 {
				continue
			}
			key, value = line[:at], strings.TrimSpace(line[at:])
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if strings.HasPrefix(value, "=") {
			value = strings.TrimSpace(value[1:])
		}
		qualified := section + "." + key
		kind := ""
		switch qualified {
		case "core.sparsecheckout":
			kind = "sparse_checkout_enabled"
		case "core.sparsecheckoutcone":
			kind = "sparse_checkout_cone"
		case "index.sparse":
			kind = "sparse_index_config"
		case "extensions.worktreeconfig":
			kind = "worktree_config_enabled"
		default:
			continue
		}
		valueOK := true
		if value != "" {
			value, valueOK = parseConfigValue(value)
		}
		parsed, ok := parseGitBool(value)
		ok = ok && valueOK
		if !ok {
			diagnostic("invalid-sparse-config-value", "A sparse-checkout-related local Git configuration value is not a supported boolean.")
			continue
		}
		values[kind] = parsed
	}
	if scanner.Err() != nil {
		diagnostic("git-config-line-limit", "A local Git configuration line exceeds the supported metadata limit.")
	}
	for _, kind := range []string{"sparse_checkout_enabled", "sparse_checkout_cone", "sparse_index_config", "worktree_config_enabled"} {
		if values[kind] {
			result.Indications = append(result.Indications, SparseIndication{Kind: kind, Evidence: evidence, Supported: true})
		}
	}
	return result
}

func parseGitBool(value string) (bool, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "true", "yes", "on", "1":
		return true, true
	case "false", "no", "off", "0":
		return false, true
	default:
		return false, false
	}
}

// InspectGitIndex reports exact sparse flags from SHA-1 Git index versions 2
// and 3. Version 4 path compression and other object formats remain unknown.
// It never expands a sparse directory or evaluates checkout patterns.
func InspectGitIndex(evidence string, content []byte, fullSize, maxBytes int64) SparseMetadataParse {
	result := SparseMetadataParse{Complete: true, Indications: []SparseIndication{}, Diagnostics: []Diagnostic{}}
	indications := maxHeap[sparseValue]{}
	finish := func() SparseMetadataParse {
		result.Indications = unwrapSparse(sortedHeap(indications))
		return result
	}
	diagnostic := func(code, message string) {
		result.Complete = false
		if len(result.Diagnostics) < DefaultDiagnosticLimit {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Path: evidence, Code: code, Message: message})
		} else {
			result.OmittedDiagnostics++
		}
	}
	indication := func(value SparseIndication) {
		full := len(indications) >= DefaultEvidenceLimit
		retain(&indications, sparseValue{value}, DefaultEvidenceLimit)
		if full {
			result.Complete = false
			result.OmittedIndications++
		}
	}
	fail := func(code, message string) SparseMetadataParse {
		diagnostic(code, message)
		return finish()
	}
	if maxBytes <= 0 {
		maxBytes = DefaultCheckoutMetadataBytes
	}
	if fullSize > maxBytes {
		return fail("git-index-size-limit", fmt.Sprintf("The Git index exceeds the %d byte metadata limit.", maxBytes))
	}
	if fullSize < 0 || int64(len(content)) != fullSize {
		return fail("incomplete-git-index", "The Git index was incomplete or changed during inspection.")
	}
	if len(content) < 12+sha1.Size || string(content[:4]) != "DIRC" {
		return fail("invalid-git-index", "The Git index header is invalid.")
	}
	version := binary.BigEndian.Uint32(content[4:8])
	if version != 2 && version != 3 {
		return fail("unsupported-git-index-version", fmt.Sprintf("Git index version %d is not interpreted for sparse metadata.", version))
	}
	want := content[len(content)-sha1.Size:]
	got := sha1.Sum(content[:len(content)-sha1.Size])
	if string(want) != string(got[:]) {
		return fail("invalid-git-index-checksum", "The Git index checksum is invalid or uses an unsupported object format.")
	}
	count := binary.BigEndian.Uint32(content[8:12])
	if count > 1_000_000 {
		return fail("git-index-entry-limit", "The Git index entry count exceeds the supported metadata limit.")
	}
	end := len(content) - sha1.Size
	offset := 12
	for range count {
		start := offset
		if offset+62 > end {
			return fail("invalid-git-index", "A Git index entry is truncated.")
		}
		mode := binary.BigEndian.Uint32(content[offset+24 : offset+28])
		flags := binary.BigEndian.Uint16(content[offset+60 : offset+62])
		offset += 62
		extended := flags&0x4000 != 0
		skipWorktree := false
		if extended {
			if offset+2 > end {
				return fail("invalid-git-index", "An extended Git index entry is truncated.")
			}
			extendedFlags := binary.BigEndian.Uint16(content[offset : offset+2])
			skipWorktree = extendedFlags&0x4000 != 0
			offset += 2
		}
		nameEnd := offset
		for nameEnd < end && content[nameEnd] != 0 {
			nameEnd++
		}
		if nameEnd == end {
			return fail("invalid-git-index", "A Git index path is unterminated.")
		}
		name := string(content[offset:nameEnd])
		if len(name) > DefaultStringBytes || !utf8.ValidString(name) || !fs.ValidPath(strings.TrimSuffix(name, "/")) || strings.ContainsAny(name, "\\:") || hasControl(name) {
			return fail("unsupported-git-index-path", "A Git index path cannot be represented as bounded normalized UTF-8 evidence.")
		}
		offset = nameEnd + 1
		padding := (8 - (offset-start)%8) % 8
		if offset+padding > end {
			return fail("invalid-git-index", "Git index entry padding is truncated.")
		}
		offset += padding
		if skipWorktree {
			kind := "skip_worktree"
			clean := strings.TrimSuffix(name, "/")
			if mode == 0040000 && strings.HasSuffix(name, "/") {
				kind = "sparse_directory"
			}
			indication(SparseIndication{Kind: kind, Path: clean, Evidence: evidence, Supported: true})
		}
	}
	for offset < end {
		if offset+8 > end {
			return fail("invalid-git-index-extension", "A Git index extension header is truncated.")
		}
		signature := string(content[offset : offset+4])
		size := int(binary.BigEndian.Uint32(content[offset+4 : offset+8]))
		offset += 8
		if size < 0 || size > end-offset {
			return fail("invalid-git-index-extension", "A Git index extension body is truncated.")
		}
		if signature == "sdir" {
			indication(SparseIndication{Kind: "sparse_index_extension", Evidence: evidence, Supported: true})
		} else if signature[0] >= 'a' && signature[0] <= 'z' {
			diagnostic("unsupported-git-index-extension", "The Git index contains an unsupported mandatory extension.")
		}
		offset += size
	}
	return finish()
}

func (c *Collector) AddSparseMetadata(parsed SparseMetadataParse) {
	if !c.mutable() {
		return
	}
	c.MarkCheckoutMetadataInspected()
	for _, indication := range parsed.Indications {
		c.AddSparseIndication(indication)
	}
	for _, diagnostic := range parsed.Diagnostics {
		c.AddDiagnostic(diagnostic)
	}
	if parsed.OmittedIndications > 0 {
		c.report.Coverage.OmittedEvidence += parsed.OmittedIndications
		c.partial("sparse_evidence_limit", parsed.OmittedIndications)
	}
	if parsed.OmittedDiagnostics > 0 {
		c.report.Coverage.OmittedDiagnostics += parsed.OmittedDiagnostics
		c.partial("sparse_diagnostic_limit", parsed.OmittedDiagnostics)
	}
	if !parsed.Complete {
		c.MarkCheckoutMetadataIncomplete("sparse_metadata_incomplete")
	}
}
