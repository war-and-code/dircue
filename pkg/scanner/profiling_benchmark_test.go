package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/war-and-code/dircue/pkg/detectors"
	"github.com/war-and-code/dircue/pkg/profile"
)

// This harness only reads externally prepared fixtures. It is disabled unless
// explicitly selected and configured; ordinary go test performs no setup or I/O.
// See tests/profiling/README.md before collecting or interpreting measurements.
var profilingFirstScanUsed atomic.Bool

func BenchmarkProfileScanner(b *testing.B) {
	b.StopTimer()
	root := os.Getenv("DIRCUE_PROFILE_ROOT")
	if root == "" {
		b.Skip("set DIRCUE_PROFILE_ROOT to an existing, independently verified fixture")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		b.Fatal(err)
	}
	source := profilingEnv("SOURCE", "directory")
	if source != "git" && source != "directory" {
		b.Fatal("DIRCUE_PROFILE_SOURCE must be git or directory; auto would obscure the scenario")
	}
	initialization := profilingEnv("INIT", "warm")
	if initialization != "warm" && initialization != "first-scan" {
		b.Fatal("DIRCUE_PROFILE_INIT must be warm or first-scan")
	}
	if initialization == "first-scan" {
		if b.N != 1 || profilingTestFlag("run") != "^$" || profilingTestFlag("bench") != "^BenchmarkProfileScanner$" || profilingTestFlag("count") != "1" || !profilingFirstScanUsed.CompareAndSwap(false, true) {
			b.Fatal("first-scan requires a fresh process and -run='^$' -bench='^BenchmarkProfileScanner$' -benchtime=1x -count=1")
		}
	}
	mode := profilingEnv("MODE", "languages")
	if mode != "languages" && mode != "all" {
		b.Fatal("DIRCUE_PROFILE_MODE must be languages or all")
	}
	opts := Options{
		Source: source, Revision: os.Getenv("DIRCUE_PROFILE_REVISION"),
		Workers:      profilingInt(b, "WORKERS", 1, 1, 1024),
		MaxTreeSize:  profilingInt(b, "MAX_TREE_SIZE", DefaultMaxTreeSize, 1, 100_000_000),
		IncludeFiles: profilingInt(b, "INCLUDE_FILES", 0, 0, 1) == 1,
	}
	if source == "git" && (len(opts.Revision) != 40 || !profilingIsHex(opts.Revision)) {
		b.Fatal("Git scenarios require DIRCUE_PROFILE_REVISION as a pinned 40-character commit SHA")
	}
	if source == "directory" && opts.Revision != "" {
		b.Fatal("directory scenarios must not set DIRCUE_PROFILE_REVISION")
	}
	if mode == "all" {
		opts.Detectors = detectors.Default()
	}
	fixtureID := profilingRequired(b, "FIXTURE_ID")
	runID := profilingRequired(b, "RUN_ID")
	if filepath.Base(runID) != runID || strings.ContainsAny(runID, `/\`) {
		b.Fatal("DIRCUE_PROFILE_RUN_ID must be a filename component")
	}
	outputDir := profilingRequired(b, "OUTPUT_DIR")
	outputDir, err = filepath.Abs(outputDir)
	if err != nil {
		b.Fatal(err)
	}
	profilingOutsideFixture(b, root, outputDir)
	for _, name := range []string{"cpuprofile", "memprofile", "mutexprofile", "blockprofile", "trace"} {
		if output := profilingTestFlag(name); output != "" {
			if !filepath.IsAbs(output) {
				b.Fatalf("-test.%s must use an absolute artifact path", name)
			}
			profilingOutsideFixture(b, root, output)
		}
	}
	// These receipts are created outside the measured process. Their content is
	// hashed only after timing; source files are never preloaded by this harness.
	receiptPaths := map[string]string{
		"fixture": profilingRequired(b, "FIXTURE_RECEIPT"),
		"build":   profilingRequired(b, "BUILD_RECEIPT"),
		"host":    profilingRequired(b, "HOST_RECEIPT"),
	}
	for _, receipt := range receiptPaths {
		info, err := os.Stat(receipt)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
			b.Fatalf("receipt %s must be an existing regular file no larger than 8 MiB: %v", receipt, err)
		}
		profilingOutsideFixture(b, root, receipt)
	}
	mutexFraction := profilingInt(b, "MUTEX_FRACTION", 0, 0, 1_000_000)
	blockRate := profilingInt(b, "BLOCK_RATE_NS", 0, 0, 1_000_000_000)
	if mutexFraction > 0 {
		previous := runtime.SetMutexProfileFraction(mutexFraction)
		b.Cleanup(func() { runtime.SetMutexProfileFraction(previous) })
	}
	if blockRate > 0 {
		runtime.SetBlockProfileRate(blockRate)
		b.Cleanup(func() { runtime.SetBlockProfileRate(0) })
	}
	expected := os.Getenv("DIRCUE_PROFILE_EXPECTED_SHA256")
	if expected != "" && (len(expected) != 64 || !profilingIsHex(expected)) {
		b.Fatal("DIRCUE_PROFILE_EXPECTED_SHA256 must be a SHA-256 digest")
	}
	var baseline *profile.Report
	var resultDigest string
	check := func(report *profile.Report) {
		b.Helper()
		if report.Summary.ScannedFiles == 0 {
			b.Fatal("empty or tree-limited scenario cannot serve as a scanner performance baseline")
		}
		if baseline != nil && !profilingReportsEqual(baseline, report) {
			b.Fatal("result changed during measurement")
		}
		if baseline == nil {
			baseline = report
		}
	}
	ctx := context.Background()
	if initialization == "warm" {
		pprof.Do(ctx, pprof.Labels("phase", "warmup", "source", source), func(ctx context.Context) {
			report, err := Scan(ctx, root, opts)
			if err != nil {
				b.Fatal(err)
			}
			check(report)
		})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var report *profile.Report
		var scanErr error
		b.StartTimer()
		pprof.Do(ctx, pprof.Labels("phase", "scan", "source", source, "mode", mode), func(ctx context.Context) {
			report, scanErr = Scan(ctx, root, opts)
		})
		b.StopTimer()
		if scanErr != nil {
			b.Fatal(scanErr)
		}
		pprof.Do(ctx, pprof.Labels("phase", "validation"), func(context.Context) { check(report) })
	}
	// The full digest is produced once, after scanning. Per-iteration validation
	// compares sorted results without serializing or allocating another payload.
	resultDigest = profilingReportDigest(b, baseline)
	if expected != "" && resultDigest != expected {
		b.Fatalf("independent expected-result digest mismatch: got %s, want %s", resultDigest, expected)
	}
	// Go's MB/s metric uses attributed language bytes, not storage bytes read.
	b.SetBytes(baseline.Summary.LanguageBytes)
	b.ReportMetric(float64(baseline.Summary.ScannedFiles), "files/op")
	b.ReportMetric(float64(baseline.Summary.LanguageBytes), "language-B/op")
	b.ReportMetric(float64(len(baseline.Warnings)), "warnings/op")
	buildInfo, _ := debug.ReadBuildInfo()
	receipts := map[string]any{}
	for name, path := range receiptPaths {
		receipts[name] = map[string]string{"path": path, "sha256": profilingFileDigest(b, path)}
	}
	fingerprint := map[string]any{
		"schema_version": 1, "run_id": runID, "fixture_id": fixtureID,
		"recorded_at": time.Now().UTC().Format(time.RFC3339Nano), "root": root,
		"source": source, "revision": opts.Revision, "mode": mode,
		"workers": opts.Workers, "max_tree_size": opts.MaxTreeSize, "include_files": opts.IncludeFiles,
		"initialization": initialization, "os_cache": profilingEnv("OS_CACHE", "uncontrolled"),
		"iterations": b.N, "measured_elapsed_ns": b.Elapsed().Nanoseconds(),
		"result_sha256": resultDigest, "external_expected_sha256": expected,
		"summary": baseline.Summary, "warnings": baseline.Warnings,
		"go_version": runtime.Version(), "goos": runtime.GOOS, "goarch": runtime.GOARCH,
		"logical_cpus": runtime.NumCPU(), "gomaxprocs": runtime.GOMAXPROCS(0),
		"gogc": os.Getenv("GOGC"), "gomemlimit": os.Getenv("GOMEMLIMIT"),
		"go_flags": os.Getenv("GOFLAGS"), "build_info": buildInfo, "receipts": receipts,
		"memory_sample_rate": runtime.MemProfileRate,
		"mutex_fraction":     runtime.SetMutexProfileFraction(-1), "requested_block_rate_ns": blockRate,
		"test_flags": map[string]string{
			"benchtime": profilingTestFlag("benchtime"), "count": profilingTestFlag("count"),
			"cpuprofile": profilingTestFlag("cpuprofile"), "memprofile": profilingTestFlag("memprofile"),
			"mutexprofile": profilingTestFlag("mutexprofile"), "blockprofile": profilingTestFlag("blockprofile"),
			"blockprofilerate": profilingTestFlag("blockprofilerate"),
		},
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		b.Fatal(err)
	}
	file, err := os.CreateTemp(outputDir, runID+"-"+source+"-*.json")
	if err != nil {
		b.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	writeErr, closeErr := encoder.Encode(fingerprint), file.Close()
	if writeErr != nil {
		b.Fatal(writeErr)
	}
	if closeErr != nil {
		b.Fatal(closeErr)
	}
	b.Logf("scenario fingerprint: %s; result sha256: %s", file.Name(), resultDigest)
}

func profilingEnv(name, fallback string) string {
	if value := os.Getenv("DIRCUE_PROFILE_" + name); value != "" {
		return value
	}
	return fallback
}

func profilingRequired(b *testing.B, name string) string {
	b.Helper()
	value := os.Getenv("DIRCUE_PROFILE_" + name)
	if value == "" {
		b.Fatalf("DIRCUE_PROFILE_%s is required; see tests/profiling/README.md", name)
	}
	return value
}

func profilingInt(b *testing.B, name string, fallback, minimum, maximum int) int {
	b.Helper()
	value, err := strconv.Atoi(profilingEnv(name, strconv.Itoa(fallback)))
	if err != nil || value < minimum || value > maximum {
		b.Fatalf("DIRCUE_PROFILE_%s must be between %d and %d", name, minimum, maximum)
	}
	return value
}

func profilingTestFlag(name string) string {
	if value := flag.Lookup("test." + name); value != nil {
		return value.Value.String()
	}
	return ""
}

func profilingIsHex(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil
}

func profilingReportDigest(b *testing.B, report *profile.Report) string {
	b.Helper()
	// Ignore the scan root so results can be compared across directories. All
	// other serialized fields, including evidence and warnings, are hashed.
	copy := *report
	copy.Root = ""
	encoded, err := json.Marshal(copy)
	if err != nil {
		b.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

func profilingReportsEqual(a, b *profile.Report) bool {
	if a.SchemaVersion != b.SchemaVersion || a.Summary != b.Summary || len(a.Languages) != len(b.Languages) || !slices.Equal(a.Warnings, b.Warnings) {
		return false
	}
	for i, language := range a.Languages {
		other := b.Languages[i]
		if language.Name != other.Name || language.Bytes != other.Bytes || language.Percentage != other.Percentage || language.FileCount != other.FileCount || language.FirstFile != other.FirstFile || !slices.Equal(language.Files, other.Files) {
			return false
		}
	}
	return profilingFindingsEqual(a.Ecosystems, b.Ecosystems) && profilingFindingsEqual(a.Frameworks, b.Frameworks) && profilingFindingsEqual(a.Layouts, b.Layouts)
}

func profilingFindingsEqual(a, b []profile.Finding) bool {
	if len(a) != len(b) {
		return false
	}
	for i, finding := range a {
		other := b[i]
		if finding.Kind != other.Kind || finding.Name != other.Name || finding.Root != other.Root || finding.Detector != other.Detector || !slices.Equal(finding.Evidence, other.Evidence) {
			return false
		}
	}
	return true
}

func profilingFileDigest(b *testing.B, path string) string {
	b.Helper()
	file, err := os.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer file.Close()
	const maxReceiptBytes = 8 << 20
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, maxReceiptBytes+1))
	if err != nil || written > maxReceiptBytes {
		b.Fatalf("receipt %s is unreadable or exceeds 8 MiB: %v", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func profilingOutsideFixture(b *testing.B, root, output string) {
	b.Helper()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		b.Fatal(err)
	}
	// Resolve the existing ancestor so a symlinked output parent cannot put an
	// artifact inside the input tree. The not-yet-created suffix remains lexical.
	ancestor, suffix := output, ""
	var canonicalOutput string
	for {
		canonicalOutput, err = filepath.EvalSymlinks(ancestor)
		if err == nil {
			canonicalOutput = filepath.Join(canonicalOutput, suffix)
			break
		}
		if !os.IsNotExist(err) || filepath.Dir(ancestor) == ancestor {
			b.Fatal(err)
		}
		suffix = filepath.Join(filepath.Base(ancestor), suffix)
		ancestor = filepath.Dir(ancestor)
	}
	relative, err := filepath.Rel(canonicalRoot, canonicalOutput)
	if err != nil || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		b.Fatalf("profile artifact %s must be outside the scanned root", output)
	}
}
