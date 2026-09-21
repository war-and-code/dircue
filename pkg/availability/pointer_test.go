package availability

import (
	"strings"
	"testing"
)

const testOID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestInspectLFSPointerCanonicalAndNoncanonical(t *testing.T) {
	canonical := "version https://git-lfs.github.com/spec/v1\noid sha256:" + testOID + "\nsize 123\n"
	got := InspectLFSPointer([]byte(canonical), int64(len(canonical)))
	if got.Kind != "valid_pointer" || !got.Canonical || got.OIDDigest != testOID || got.DeclaredObjectBytes != 123 || got.OIDAlgorithm != "sha256" {
		t.Fatalf("canonical pointer: %+v", got)
	}
	withoutNewline := strings.TrimSuffix(canonical, "\n")
	got = InspectLFSPointer([]byte(withoutNewline), int64(len(withoutNewline)))
	if got.Kind != "valid_pointer" || got.Canonical {
		t.Fatalf("noncanonical pointer: %+v", got)
	}
	legacy := strings.Replace(canonical, currentLFSVersion, "https://hawser.github.com/spec/v1", 1)
	got = InspectLFSPointer([]byte(legacy), int64(len(legacy)))
	if got.Kind != "valid_pointer" || got.Canonical || got.Version != "https://hawser.github.com/spec/v1" {
		t.Fatalf("legacy pointer: %+v", got)
	}
	zero := strings.Replace(canonical, "size 123", "size 0", 1)
	got = InspectLFSPointer([]byte(zero), int64(len(zero)))
	if got.Kind != "valid_pointer" || got.Canonical || got.DeclaredObjectBytes != 0 {
		t.Fatalf("zero-size noncanonical pointer: %+v", got)
	}
}

func TestInspectLFSPointerExtensionsAndCounterexamples(t *testing.T) {
	extended := "version https://git-lfs.github.com/spec/v1\next-0-zip sha256:" + testOID + "\noid sha256:" + strings.Repeat("a", 64) + "\nsize 1\n"
	got := InspectLFSPointer([]byte(extended), int64(len(extended)))
	if got.Kind != "valid_pointer" || !got.Canonical || got.ExtensionCount != 1 {
		t.Fatalf("extended pointer: %+v", got)
	}
	cases := []struct {
		name, content, reason string
		size                  int64
	}{
		{"short-oid", strings.Replace(extended, strings.Repeat("a", 64), "abc", 1), "invalid_oid", 0},
		{"uppercase-oid", strings.Replace(extended, testOID, strings.ToUpper(testOID), 1), "invalid_extension_oid", 0},
		{"duplicate", "version " + currentLFSVersion + "\noid sha256:" + testOID + "\noid sha256:" + testOID + "\nsize 1\n", "duplicate_key", 0},
		{"order", "version " + currentLFSVersion + "\nsize 1\noid sha256:" + testOID + "\n", "keys_out_of_order", 0},
		{"unknown-key", "version " + currentLFSVersion + "\nfuture value\noid sha256:" + testOID + "\nsize 1\n", "unsupported_key", 0},
		{"oversize", "version " + currentLFSVersion + "\noid sha256:" + testOID + "\nsize 1\n" + strings.Repeat("x", 1024), "size_cutoff", 1100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			size := tc.size
			if size == 0 {
				size = int64(len(tc.content))
			}
			got := InspectLFSPointer([]byte(tc.content)[:min(len(tc.content), int(PointerSizeCutoff))], size)
			if got.Kind != "pointer_like" || got.Reason != tc.reason {
				t.Fatalf("got %+v", got)
			}
		})
	}
	ordinary := "This document mentions oid sha256:" + testOID + " and Git LFS."
	if got := InspectLFSPointer([]byte(ordinary), int64(len(ordinary))); got.Kind != "" {
		t.Fatalf("ordinary text classified as pointer: %+v", got)
	}
}

func TestInspectLFSPointerNeverUsesPartialMetadata(t *testing.T) {
	pointer := "version " + currentLFSVersion + "\noid sha256:" + testOID + "\nsize 9\n"
	got := InspectLFSPointer([]byte(pointer[:60]), int64(len(pointer)))
	if got.Kind != "pointer_like" || got.Reason != "incomplete_content" || got.OIDDigest != "" {
		t.Fatalf("partial pointer: %+v", got)
	}
}
