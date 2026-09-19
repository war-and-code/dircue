//go:build unix

package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestPackageReportRejectsSpecialFiles(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "report.pipe")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	regular := filepath.Join(root, "report.json")
	if err := os.WriteFile(regular, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "report.link")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{fifo, link, root} {
		var out, stderr bytes.Buffer
		if err := Execute(context.Background(), []string{"analyze", "packages", "--syft-report", name, "--json", root}, &out, &stderr); err == nil || out.Len() != 0 {
			t.Fatalf("special report input accepted: %s", name)
		}
	}
}
