//go:build linux || darwin

package scanner

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"dircue/pkg/rules"
)

func TestRulesFIFOIsNotOpened(t *testing.T) {
	root := fixtures(t, map[string]string{"real.txt": "MAGIC"})
	if err := syscall.Mkfifo(filepath.Join(root, "pipe.txt"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := Scan(ctx, root, rulesOptions(t, true))
	if err != nil || r.Rules.InventoryFiles != 1 || r.Rules.Omissions[rules.NonRegularFile] != 1 || r.Rules.TotalMatches != 1 {
		t.Fatalf("FIFO: %+v %v", r, err)
	}
}
