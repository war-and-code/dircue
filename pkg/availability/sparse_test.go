package availability

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

func testIndex(version uint32, name string, mode uint32, skip bool, extension string) []byte {
	data := make([]byte, 12)
	copy(data, "DIRC")
	binary.BigEndian.PutUint32(data[4:8], version)
	binary.BigEndian.PutUint32(data[8:12], 1)
	entry := make([]byte, 62)
	binary.BigEndian.PutUint32(entry[24:28], mode)
	flags := uint16(len(name))
	if skip {
		flags |= 0x4000
	}
	binary.BigEndian.PutUint16(entry[60:62], flags)
	data = append(data, entry...)
	if skip {
		data = append(data, 0x40, 0x00)
	}
	data = append(data, []byte(name)...)
	data = append(data, 0)
	for (len(data)-12)%8 != 0 {
		data = append(data, 0)
	}
	if extension != "" {
		data = append(data, []byte(extension)...)
		data = append(data, 0, 0, 0, 0)
	}
	sum := sha1.Sum(data)
	return append(data, sum[:]...)
}

func testSparseIndexEntries(count int) []byte {
	data := make([]byte, 12)
	copy(data, "DIRC")
	binary.BigEndian.PutUint32(data[4:8], 3)
	binary.BigEndian.PutUint32(data[8:12], uint32(count))
	for i := count - 1; i >= 0; i-- {
		start := len(data)
		name := fmt.Sprintf("cold/%05d", i)
		entry := make([]byte, 62)
		binary.BigEndian.PutUint32(entry[24:28], 0100644)
		binary.BigEndian.PutUint16(entry[60:62], uint16(len(name))|0x4000)
		data = append(data, entry...)
		data = append(data, 0x40, 0x00)
		data = append(data, name...)
		data = append(data, 0)
		for (len(data)-start)%8 != 0 {
			data = append(data, 0)
		}
	}
	sum := sha1.Sum(data)
	return append(data, sum[:]...)
}

func TestInspectGitIndexSparseEvidence(t *testing.T) {
	content := testIndex(3, "cold/", 0040000, true, "sdir")
	got := InspectGitIndex(".git/index", content, int64(len(content)), DefaultCheckoutMetadataBytes)
	if !got.Complete || len(got.Diagnostics) != 0 || len(got.Indications) != 2 {
		t.Fatalf("parse: %+v", got)
	}
	if got.Indications[0].Kind != "sparse_index_extension" || got.Indications[1].Kind != "sparse_directory" || got.Indications[1].Path != "cold" {
		t.Fatalf("indications: %+v", got.Indications)
	}
}

func TestInspectGitIndexUnsupportedOrCorruptIsUnknown(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		code string
	}{
		{"v4", testIndex(4, "cold/file", 0100644, true, ""), "unsupported-git-index-version"},
		{"mandatory-extension", testIndex(3, "cold/file", 0100644, true, "abcd"), "unsupported-git-index-extension"},
		{"checksum", func() []byte { b := testIndex(3, "cold/file", 0100644, true, ""); b[len(b)-1] ^= 1; return b }(), "invalid-git-index-checksum"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := InspectGitIndex(".git/index", tc.data, int64(len(tc.data)), DefaultCheckoutMetadataBytes)
			if got.Complete || len(got.Diagnostics) == 0 || got.Diagnostics[0].Code != tc.code {
				t.Fatalf("got: %+v", got)
			}
		})
	}
}

func TestInspectSparseConfigRecognizesBooleansWithoutFollowingIncludes(t *testing.T) {
	content := "[core]\n sparseCheckout = true\n sparseCheckoutCone = \"yes\" # comment\n[index]\n sparse = on\n[include]\n path = ../outside\n"
	got := InspectSparseConfig(".git/config", []byte(content), int64(len(content)), DefaultGitmodulesBytes)
	if got.Complete || len(got.Indications) != 3 || len(got.Diagnostics) != 1 || got.Diagnostics[0].Code != "unsupported-git-config-include" {
		t.Fatalf("config: %+v", got)
	}
	for _, indication := range got.Indications {
		if indication.Path != "" || !indication.Supported {
			t.Fatalf("indication: %+v", indication)
		}
	}
}

func TestSparseParsersBoundIntermediateOutputsAndPropagateOmissions(t *testing.T) {
	var config strings.Builder
	for i := 0; i < DefaultDiagnosticLimit+500; i++ {
		fmt.Fprintf(&config, "[include %q]\n", fmt.Sprintf("x%d", i))
	}
	parsedConfig := InspectSparseConfig(".git/config", []byte(config.String()), int64(config.Len()), DefaultGitmodulesBytes)
	if parsedConfig.Complete || len(parsedConfig.Diagnostics) != DefaultDiagnosticLimit || parsedConfig.OmittedDiagnostics != 500 {
		t.Fatalf("config diagnostic cap: diagnostics=%d omitted=%d complete=%t", len(parsedConfig.Diagnostics), parsedConfig.OmittedDiagnostics, parsedConfig.Complete)
	}

	index := testSparseIndexEntries(DefaultEvidenceLimit + 500)
	parsedIndex := InspectGitIndex(".git/index", index, int64(len(index)), DefaultCheckoutMetadataBytes)
	if parsedIndex.Complete || len(parsedIndex.Indications) != DefaultEvidenceLimit || parsedIndex.OmittedIndications != 500 {
		t.Fatalf("index indication cap: indications=%d omitted=%d complete=%t", len(parsedIndex.Indications), parsedIndex.OmittedIndications, parsedIndex.Complete)
	}
	if parsedIndex.Indications[0].Path != "cold/00000" || parsedIndex.Indications[len(parsedIndex.Indications)-1].Path != "cold/04095" {
		t.Fatalf("index cap did not retain lexical prefix: first=%q last=%q", parsedIndex.Indications[0].Path, parsedIndex.Indications[len(parsedIndex.Indications)-1].Path)
	}
	c := New(Options{})
	c.AddSparseMetadata(parsedIndex)
	report, err := c.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Coverage.OmittedEvidence != 500 || report.Omissions["sparse_evidence_limit"] != 500 || report.Coverage.CheckoutMetadataComplete {
		t.Fatalf("collector omission propagation: %+v", report)
	}
}
