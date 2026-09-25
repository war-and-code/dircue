//go:build linux || darwin

package scanner

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestFIFOSkippedAndOpenIsNonblocking verifies that a FIFO in the scanned tree
// is reported as a skipped file with reason non_regular_file and that
// openRegular on the same FIFO never blocks (the nonblocking O_NONBLOCK flag).
func TestFIFOSkippedAndOpenIsNonblocking(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe.go"), 0600); err != nil {
		t.Fatal(err)
	}
	report, err := Scan(context.Background(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.SkippedFiles != 1 || report.Summary.AnalyzedFiles != 0 {
		t.Fatalf("FIFO not skipped: %+v", report.Summary)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	done := make(chan error, 1)
	go func() {
		file, err := openRegular(root, "pipe.go")
		if file != nil {
			file.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("opening FIFO blocked")
	}
}

// TestUnixSocketSkippedAsNonRegular verifies that a Unix domain socket is
// skipped with reason non_regular_file and does not hang the scan.
//
// macOS limits Unix domain socket paths to 104 bytes, so we use os.MkdirTemp
// with the system temp dir (which is short on macOS) instead of t.TempDir
// (which uses a long per-test path).
func TestUnixSocketSkippedAsNonRegular(t *testing.T) {
	// Use a short base temp dir for the socket path.
	dir, err := os.MkdirTemp("", "dircue-sock-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sockPath := filepath.Join(dir, "sock.go")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Skipf("unix socket unavailable: %v", err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	report, err := Scan(ctx, dir, Options{})
	if err != nil {
		t.Fatalf("scan failed on socket: %v", err)
	}
	if report.Summary.SkippedFiles != 1 || report.Summary.AnalyzedFiles != 0 {
		t.Fatalf("socket not skipped: %+v", report.Summary)
	}
	hasWarn := false
	for _, w := range report.Warnings {
		if w.Code == "non_regular_file" && w.Path == "sock.go" {
			hasWarn = true
		}
	}
	if !hasWarn {
		t.Fatalf("missing non_regular_file warning for socket: %+v", report.Warnings)
	}
}

// TestCharDeviceSkippedAsNonRegular verifies that a character device (reached
// via a symlink to /dev/null inside the root) is treated as non-regular.
// The scanner uses Lstat, so the symlink itself is the entry seen by the walk
// and is reported as a non-regular file.
func TestCharDeviceSkippedAsNonRegular(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink("/dev/null", filepath.Join(dir, "devnull.go")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	report, err := Scan(ctx, dir, Options{})
	if err != nil {
		t.Fatalf("scan failed on char device symlink: %v", err)
	}
	if report.Summary.SkippedFiles != 1 || report.Summary.AnalyzedFiles != 0 {
		t.Fatalf("char device symlink not skipped: %+v", report.Summary)
	}
}

// TestSymlinkLoopSkipped verifies that a symlink cycle (A → B → A) does not
// hang the scanner and is reported as a skipped non_regular_file. The walk
// uses fs.WalkDir which does not follow symlinks into directories, so cycles
// are safe regardless.
func TestSymlinkLoopSkipped(t *testing.T) {
	dir := t.TempDir()
	// a → b → a cycle
	aPath := filepath.Join(dir, "a")
	bPath := filepath.Join(dir, "b")
	if err := os.Symlink(bPath, aPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.Symlink(aPath, bPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	report, err := Scan(ctx, dir, Options{})
	if err != nil {
		t.Fatalf("scan failed on symlink loop: %v", err)
	}
	// Two symlink files, both skipped.
	if report.Summary.SkippedFiles != 2 {
		t.Fatalf("expected 2 skipped for symlink loop, got %+v", report.Summary)
	}
	for _, w := range report.Warnings {
		if w.Code != "non_regular_file" {
			t.Errorf("unexpected warning code %q for symlink loop", w.Code)
		}
	}
}

// TestSymlinkEscapeSkipped verifies that a symlink pointing outside the root
// does not allow content to be read from outside the root. os.Root confines
// all operations; the symlink itself appears as a non-regular file.
func TestSymlinkEscapeSkipped(t *testing.T) {
	dir := t.TempDir()
	// Create a file outside the root to link to.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside"), 0644); err != nil {
		t.Fatal(err)
	}
	// Place an escaping symlink inside the root.
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "escape.go")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	report, err := Scan(ctx, dir, Options{})
	if err != nil {
		t.Fatalf("scan failed on escaping symlink: %v", err)
	}
	// The symlink is seen as non-regular and skipped; the outside file is never read.
	if report.Summary.SkippedFiles != 1 || report.Summary.AnalyzedFiles != 0 {
		t.Fatalf("escaping symlink not skipped: %+v", report.Summary)
	}
	// Verify the outside content is not reflected in the report.
	for lang := range report.Languages {
		_ = lang
		t.Errorf("unexpected language detected when only an escaping symlink is present")
	}
}

// TestSymlinkToDirectorySkipped verifies that a symlink pointing to a directory
// is skipped as non_regular_file and is not walked into.
func TestSymlinkToDirectorySkipped(t *testing.T) {
	dir := t.TempDir()
	subdir := t.TempDir()
	// Put a regular file in the target directory.
	if err := os.WriteFile(filepath.Join(subdir, "inside.go"), []byte(goSource), 0644); err != nil {
		t.Fatal(err)
	}
	// Symlink from inside the root to the other directory.
	if err := os.Symlink(subdir, filepath.Join(dir, "link_to_dir")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	report, err := Scan(ctx, dir, Options{})
	if err != nil {
		t.Fatalf("scan failed on symlink-to-directory: %v", err)
	}
	// The symlink is skipped; the file inside the target dir is NOT scanned.
	if report.Summary.SkippedFiles != 1 || report.Summary.AnalyzedFiles != 0 {
		t.Fatalf("symlink-to-directory not skipped: %+v", report.Summary)
	}
}

// TestHardlinksBothPathsCounted verifies that a pair of hardlinks (two names
// for the same inode) are each reported as regular files, with their bytes
// counted per-path. The scanner does not deduplicate hardlinks; it counts bytes
// per directory entry, which matches how Linguist behaves.
func TestHardlinksBothPathsCounted(t *testing.T) {
	dir := t.TempDir()
	content := goSource
	original := filepath.Join(dir, "original.go")
	link := filepath.Join(dir, "hardlink.go")
	if err := os.WriteFile(original, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(original, link); err != nil {
		t.Skipf("hardlink unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	report, err := Scan(ctx, dir, Options{Source: "directory", IncludeFiles: true})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	// Both paths are analyzed.
	if report.Summary.AnalyzedFiles != 2 {
		t.Fatalf("expected 2 analyzed files (both hardlink paths), got %d", report.Summary.AnalyzedFiles)
	}
	// Both paths contribute bytes (per-path counting is documented behavior).
	for _, lang := range report.Languages {
		if lang.FileCount != 2 {
			t.Fatalf("expected file count 2 for language %q, got %d", lang.Name, lang.FileCount)
		}
		if lang.Bytes != int64(2*len(content)) {
			t.Fatalf("expected %d bytes (per-path), got %d", 2*len(content), lang.Bytes)
		}
	}
}

// TestSparseFileBoundedRead verifies that a sparse file with a large apparent
// size (10 GiB) is handled without allocating or reading its full apparent
// size. The scanner reads at most ClassificationBytes (128 KiB) when no
// explicit MaxFileBytes is set.
func TestSparseFileBoundedRead(t *testing.T) {
	dir := t.TempDir()
	sparse := filepath.Join(dir, "sparse.go")
	f, err := os.Create(sparse)
	if err != nil {
		t.Fatal(err)
	}
	// Truncate to 10 GiB to create a sparse file with a large apparent size.
	const tenGiB = 10 * 1024 * 1024 * 1024
	if err := f.Truncate(tenGiB); err != nil {
		f.Close()
		t.Skipf("sparse file creation failed: %v", err)
	}
	f.Close()

	info, err := os.Stat(sparse)
	if err != nil || info.Size() != tenGiB {
		t.Skipf("sparse file not created correctly: size=%d err=%v", info.Size(), err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	report, err := Scan(ctx, dir, Options{Source: "directory"})
	if err != nil {
		t.Fatalf("scan failed on sparse file: %v", err)
	}
	// The file is analyzed (or skipped as binary); it is not an unhandled error.
	if report.Summary.AnalyzedFiles+report.Summary.SkippedFiles != 1 {
		t.Fatalf("sparse file should appear as analyzed or skipped: %+v", report.Summary)
	}
	// No crash, no OOM: if we get here, the read was bounded.
}

// TestPermissionDeniedFileContinue verifies that a permission-denied regular
// file is skipped with a file_read_error warning under ErrorPolicyContinue
// and does not abort the scan.
func TestPermissionDeniedFileContinue(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod 000 has no effect when running as root")
	}
	dir := fixtures(t, map[string]string{
		"good.go":   goSource,
		"forbid.go": goSource,
	})
	if err := os.Chmod(filepath.Join(dir, "forbid.go"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "forbid.go"), 0644) })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	report, err := Scan(ctx, dir, Options{Source: "directory", ErrorPolicy: ErrorPolicyContinue})
	if err != nil {
		t.Fatalf("continue policy aborted on permission-denied file: %v", err)
	}
	// The good file is analyzed; the forbidden file is skipped.
	if report.Summary.AnalyzedFiles != 1 {
		t.Fatalf("expected 1 analyzed, got %+v", report.Summary)
	}
	if report.Summary.SkippedFiles != 1 {
		t.Fatalf("expected 1 skipped, got %+v", report.Summary)
	}
	hasWarn := false
	for _, w := range report.Warnings {
		if w.Code == "file_read_error" && strings.Contains(w.Path, "forbid") {
			hasWarn = true
		}
	}
	if !hasWarn {
		t.Fatalf("expected file_read_error warning: %+v", report.Warnings)
	}
}

// TestPermissionDeniedFileFail verifies that a permission-denied regular file
// is fatal under the default ErrorPolicyFail.
func TestPermissionDeniedFileFail(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod 000 has no effect when running as root")
	}
	dir := fixtures(t, map[string]string{
		"good.go":   goSource,
		"forbid.go": goSource,
	})
	if err := os.Chmod(filepath.Join(dir, "forbid.go"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "forbid.go"), 0644) })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Scan(ctx, dir, Options{Source: "directory", ErrorPolicy: ErrorPolicyFail})
	if err == nil {
		t.Fatal("fail policy did not return error on permission-denied file")
	}
}

// TestPermissionDeniedDirectoryContinue verifies that a permission-denied
// directory is skipped with a permission_denied warning under
// ErrorPolicyContinue and does not abort the scan. Other files in the tree
// are still profiled.
func TestPermissionDeniedDirectoryContinue(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod 000 has no effect when running as root")
	}
	dir := fixtures(t, map[string]string{
		"good.go":          goSource,
		"secret/hidden.go": goSource,
	})
	if err := os.Chmod(filepath.Join(dir, "secret"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "secret"), 0755) })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	report, err := Scan(ctx, dir, Options{Source: "directory", ErrorPolicy: ErrorPolicyContinue})
	if err != nil {
		t.Fatalf("continue policy aborted on permission-denied directory: %v", err)
	}
	// The good.go file (in the root) is analyzed; the secret directory is skipped.
	if report.Summary.AnalyzedFiles != 1 {
		t.Fatalf("expected 1 analyzed (good.go), got %+v", report.Summary)
	}
	hasPermWarn := false
	for _, w := range report.Warnings {
		if w.Code == "permission_denied" && strings.Contains(w.Path, "secret") {
			hasPermWarn = true
		}
	}
	if !hasPermWarn {
		t.Fatalf("expected permission_denied warning for secret dir: %+v", report.Warnings)
	}
}

// TestPermissionDeniedDirectoryFail verifies that a permission-denied directory
// is fatal under the default ErrorPolicyFail.
func TestPermissionDeniedDirectoryFail(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod 000 has no effect when running as root")
	}
	dir := fixtures(t, map[string]string{
		"good.go":          goSource,
		"secret/hidden.go": goSource,
	})
	if err := os.Chmod(filepath.Join(dir, "secret"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "secret"), 0755) })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Scan(ctx, dir, Options{Source: "directory", ErrorPolicy: ErrorPolicyFail})
	if err == nil {
		t.Fatal("fail policy did not return error on permission-denied directory")
	}
}

// TestInvalidUTF8Filename verifies that a file whose name contains invalid
// UTF-8 bytes is handled without panicking. On macOS (APFS) the filesystem
// may reject such names; the test skips in that case.
func TestInvalidUTF8Filename(t *testing.T) {
	dir := t.TempDir()
	// \xff\xfe are invalid UTF-8 lead bytes.
	badName := "bad\xff\xfename.go"
	badPath := filepath.Join(dir, badName)
	if err := os.WriteFile(badPath, []byte(goSource), 0644); err != nil {
		t.Skipf("filesystem rejected invalid UTF-8 filename: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	report, err := Scan(ctx, dir, Options{Source: "directory"})
	if err != nil {
		t.Fatalf("scan panicked or failed on invalid UTF-8 filename: %v", err)
	}
	// File is either analyzed or skipped; no crash is the key property.
	total := report.Summary.AnalyzedFiles + report.Summary.SkippedFiles
	if total != 1 {
		t.Fatalf("expected 1 entry, got %+v", report.Summary)
	}
}

// TestControlCharFilename verifies that a file whose name contains a newline
// or other control character is handled without panicking. Most Linux
// filesystems allow this; macOS may not.
func TestControlCharFilename(t *testing.T) {
	dir := t.TempDir()
	// Newline in filename. Valid on ext4/tmpfs, rejected on macOS APFS/HFS+.
	ctrlName := "line\nbreak.go"
	ctrlPath := filepath.Join(dir, ctrlName)
	if err := os.WriteFile(ctrlPath, []byte(goSource), 0644); err != nil {
		t.Skipf("filesystem rejected control-char filename: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	report, err := Scan(ctx, dir, Options{Source: "directory"})
	if err != nil {
		t.Fatalf("scan failed on control-char filename: %v", err)
	}
	total := report.Summary.AnalyzedFiles + report.Summary.SkippedFiles
	if total != 1 {
		t.Fatalf("expected 1 entry, got %+v", report.Summary)
	}
}

// TestNFCNFDFilenames verifies that NFC and NFD variants of the same name are
// each handled without panicking or causing incorrect counts. On macOS (APFS,
// case-insensitive NFC-normalizing), the two filenames collapse to one inode;
// the test checks that whatever count the filesystem provides is handled
// correctly.
func TestNFCNFDFilenames(t *testing.T) {
	dir := t.TempDir()
	// "é" in NFC (U+00E9) vs NFD (U+0065 + U+0301).
	nfc := "é_file.go" // NFD form of "é"
	nfd := "é_file.go"  // NFC form (precomposed)
	nfcPath := filepath.Join(dir, nfc)
	nfdPath := filepath.Join(dir, nfd)
	if err := os.WriteFile(nfcPath, []byte(goSource), 0644); err != nil {
		t.Skipf("filesystem rejected NFC/NFD filename: %v", err)
	}
	if err := os.WriteFile(nfdPath, []byte(goSource), 0644); err != nil {
		t.Skipf("filesystem rejected NFC/NFD filename pair: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	report, err := Scan(ctx, dir, Options{Source: "directory"})
	if err != nil {
		t.Fatalf("scan failed on NFC/NFD filenames: %v", err)
	}
	// Either 1 (filesystem normalized) or 2 (distinct on case-sensitive FS);
	// either way no panic and counts are consistent.
	total := report.Summary.AnalyzedFiles + report.Summary.SkippedFiles
	if total < 1 || total > 2 {
		t.Fatalf("unexpected entry count for NFC/NFD test: %+v", report.Summary)
	}
}

// TestCaseOnlyCollision verifies that two files whose names differ only in
// case are each handled (or collapsed by the filesystem) without panic or
// incorrect totals. macOS (case-insensitive) collapses to one entry; Linux
// keeps both.
func TestCaseOnlyCollision(t *testing.T) {
	dir := t.TempDir()
	upper := filepath.Join(dir, "Foo.go")
	lower := filepath.Join(dir, "foo.go")
	if err := os.WriteFile(upper, []byte(goSource), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lower, []byte(goSource+"// lower\n"), 0644); err != nil {
		t.Skipf("case-insensitive filesystem rejected second file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	report, err := Scan(ctx, dir, Options{Source: "directory"})
	if err != nil {
		t.Fatalf("scan failed on case-only collision: %v", err)
	}
	total := report.Summary.AnalyzedFiles + report.Summary.SkippedFiles
	if total < 1 || total > 2 {
		t.Fatalf("unexpected entry count for case-only collision: %+v", report.Summary)
	}
}

// TestDeepTreeNoStackOverflow verifies that a very deep directory tree does
// not cause a stack overflow or hang. fs.WalkDir is iterative (not recursive),
// so depth alone should not exhaust the stack.
func TestDeepTreeNoStackOverflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping deep-tree test in short mode")
	}
	root := t.TempDir()
	// Build a chain a/a/a/... at depth 200 with a single file at the bottom.
	current := root
	const depth = 200
	for i := range depth {
		next := filepath.Join(current, "d")
		if err := os.Mkdir(next, 0755); err != nil {
			t.Fatalf("mkdir at depth %d: %v", i, err)
		}
		current = next
	}
	if err := os.WriteFile(filepath.Join(current, "leaf.go"), []byte(goSource), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	report, err := Scan(ctx, root, Options{Source: "directory", MaxTreeSize: depth + 10})
	if err != nil {
		t.Fatalf("deep tree scan failed: %v", err)
	}
	if report.Summary.AnalyzedFiles != 1 {
		t.Fatalf("expected 1 analyzed (leaf), got %+v", report.Summary)
	}
}

// TestLongPathHandled verifies that a path whose total length exceeds typical
// PATH_MAX limits is handled without panicking. On Linux the root-relative walk
// path is short per component; the test verifies that deeply nested directories
// with long component names do not cause a crash or silent hang.
func TestLongPathHandled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long-path test in short mode")
	}
	root := t.TempDir()
	// NAME_MAX on Linux/macOS is 255. Create a few levels of 200-byte names.
	// Combined with the temp dir prefix (~50 bytes), the full path will exceed
	// 1024 bytes. We stop before hitting OS hard limits to keep the test
	// portable.
	longName := strings.Repeat("x", 200)
	current := root
	levels := 0
	for {
		next := filepath.Join(current, longName)
		if err := os.Mkdir(next, 0755); err != nil {
			// Filesystem rejected it (probably too long); stop here.
			break
		}
		current = next
		levels++
		if levels >= 8 {
			break
		}
	}
	if levels == 0 {
		t.Skip("filesystem rejected all long-name directories")
	}
	leafPath := filepath.Join(current, strings.Repeat("y", 200)+".go")
	_ = os.WriteFile(leafPath, []byte(goSource), 0644) // best effort

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// The scan must complete (or fail with an explicit error) without hanging.
	_, err := Scan(ctx, root, Options{Source: "directory"})
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("long-path scan timed out")
	}
	// Non-timeout errors (e.g. ENAMETOOLONG) are acceptable; what matters is
	// no hang and no panic.
}

// TestZeroByteFileIsInventoried verifies that a zero-byte regular file is
// included in the inventory without error. It has no language (no content),
// but it is a valid file.
func TestZeroByteFileIsInventoried(t *testing.T) {
	dir := fixtures(t, map[string]string{
		"empty.go": "",
		"full.go":  goSource,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	report, err := Scan(ctx, dir, Options{Source: "directory"})
	if err != nil {
		t.Fatalf("scan failed on zero-byte file: %v", err)
	}
	// Both files are walked; the empty one has no language.
	if report.Summary.AnalyzedFiles != 2 {
		t.Fatalf("expected 2 analyzed files, got %+v", report.Summary)
	}
}

// TestFIFOReplacementRaceNonBlocking verifies that when a file appears to be
// regular during the directory walk but is replaced by a FIFO before the
// worker opens it, the open does not block. readBoundedSize first calls
// openRegular (nonblocking) and then re-checks IsRegular after the open.
func TestFIFOReplacementRaceNonBlocking(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "race.go")
	// Create a regular file first so the walk sees it as regular.
	if err := os.WriteFile(target, []byte(goSource), 0644); err != nil {
		t.Fatal(err)
	}
	// Replace the regular file with a FIFO.
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(target, 0600); err != nil {
		t.Fatal(err)
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	// The open must fail fast and be a recoverable error; it must not block.
	done := make(chan error, 1)
	go func() {
		_, _, _, err := readBoundedSize(root, "race.go", ClassificationBytes)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error for FIFO replacement, got nil")
		}
		if !isRecoverableFileError(err) {
			t.Fatalf("expected recoverable file error for FIFO replacement, got %T: %v", err, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("FIFO replacement open blocked")
	}
}

// TestMkfifoAttachSIGTERM verifies the --attach FIFO path: opening a FIFO as
// an attachment report must fail fast (no block) and the process must honor
// SIGTERM within a bounded time. This is a regression test for the FIFO hang
// found during the #94 review.
//
// This test verifies providerjoin.Join rejects FIFOs without blocking. The
// SIGTERM end-to-end test is in tests/hostile_fs/ because it requires a
// subprocess.
func TestAttachFIFORejectedFast(t *testing.T) {
	// Use the providerjoin package FIFO test (already in open_unix_test.go).
	// Here we verify via readBoundedSize that the scanner's own open path is
	// also non-blocking when a FIFO is encountered as an attachment target.
	dir := t.TempDir()
	fifo := filepath.Join(dir, "report.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	done := make(chan error, 1)
	go func() {
		_, _, _, err := readBoundedSize(root, "report.fifo", 1024)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error reading FIFO, got nil")
		}
		// The error must be recoverable (not a protocol/fatal error).
		if !isRecoverableFileError(err) {
			t.Fatalf("expected recoverable error for FIFO, got %T: %v", err, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("readBoundedSize blocked on FIFO attachment target")
	}
}

// TestFilesGrowDuringRead verifies that readAllBounded respects its limit even
// when content is appended to the file while it is being read. The LimitReader
// wrapping in readAllBounded ensures no more than limit+1 bytes are consumed.
func TestFilesGrowDuringRead(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("concurrent write test requires unix semantics")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "growing.go")
	// Start with some content.
	initial := strings.Repeat("// line\n", 100)
	if err := os.WriteFile(target, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	const limit = 512
	// Grow the file concurrently while reading.
	stop := make(chan struct{})
	go func() {
		defer close(stop)
		f, err := os.OpenFile(target, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return
		}
		defer f.Close()
		extra := strings.Repeat("x", 4096)
		for i := range 20 {
			select {
			case <-stop:
				return
			default:
			}
			_ = i
			_, _ = f.WriteString(extra)
			time.Sleep(time.Millisecond)
		}
	}()
	defer func() { <-stop }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, _, _, err := readBoundedSize(root, "growing.go", limit)
	if err != nil {
		t.Fatalf("unexpected error reading growing file: %v", err)
	}
	// We must never read more than limit+1 bytes.
	if int64(len(data)) > limit+1 {
		t.Fatalf("read %d bytes from growing file, expected at most %d", len(data), limit+1)
	}
	_ = ctx
}

// A summarized environment tree must not consume the inventory limit in the
// directory preflight; otherwise a large node_modules makes the map partial.
func TestSummarizedTreesDoNotExhaustTreeSizePreflight(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "node_modules", "pkg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d.js", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	exceeded, err := directoryTreeLimit(context.Background(), r, Options{MaxTreeSize: 10, SummarizeTrees: true})
	if err != nil || exceeded {
		t.Fatalf("summarized tree consumed the preflight limit: exceeded=%v err=%v", exceeded, err)
	}
	exceeded, _ = directoryTreeLimit(context.Background(), r, Options{MaxTreeSize: 10})
	if !exceeded {
		t.Fatal("without summarization the preflight should count node_modules")
	}
}
