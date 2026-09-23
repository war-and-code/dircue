//go:build unix

package providerjoin_test

import (
	"context"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"dircue/pkg/providerjoin"
)

func TestAttachmentFIFORejectedWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := providerjoin.Join(ctx, providerjoin.Input{}, []providerjoin.Attachment{{Kind: "syft-json", Path: path}}, providerjoin.Options{})
	if err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("FIFO error = %v", err)
	}
}
