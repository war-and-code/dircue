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

func TestReportAndRulesRejectSpecialFiles(t *testing.T) {
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
		for _, mode := range []struct{ command, flag string }{{"packages", "--syft-report"}, {"rules", "--rules-file"}} {
			var out, stderr bytes.Buffer
			if err := Execute(context.Background(), []string{"analyze", mode.command, mode.flag, name, "--json", root}, &out, &stderr); err == nil || out.Len() != 0 {
				t.Fatalf("special %s input accepted: %s", mode.command, name)
			}
		}
	}
}
