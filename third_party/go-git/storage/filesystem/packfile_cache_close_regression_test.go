package filesystem

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing/cache"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing/format/idxfile"
)

// Cache construction does not parse object contents. Empty files isolate the
// descriptor ownership transition from unrelated pack decoding behavior.
func TestPackfileCacheInsertionFailureClosesNewFile(t *testing.T) {
	root := t.TempDir()
	first := plumbing.NewHash("1111111111111111111111111111111111111111")
	second := plumbing.NewHash("2222222222222222222222222222222222222222")
	for _, hash := range []plumbing.Hash{first, second} {
		filename := filepath.Join(root, "objects", "pack", "pack-"+hash.String()+".pack")
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	closeError := errors.New("evicted descriptor close failed")
	fs := &trackedSizeFS{Filesystem: osfs.New(root), closeError: closeError}
	t.Cleanup(func() {
		for _, file := range fs.files {
			if !file.closed {
				_ = file.File.Close()
			}
		}
	})
	storage := NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), Options{MaxOpenDescriptors: 1})
	defer storage.Close()
	if _, err := storage.packfile(idxfile.NewMemoryIndex(), first); err != nil {
		t.Fatal(err)
	}
	got, err := storage.packfile(idxfile.NewMemoryIndex(), second)
	if !errors.Is(err, closeError) {
		t.Fatalf("got error=%v; want original eviction error", err)
	}
	if got != nil {
		t.Error("failed insertion returned an unowned packfile")
	}
	if len(fs.files) != 2 {
		t.Fatalf("opened %d files, want 2", len(fs.files))
	}
	for i, file := range fs.files {
		if file.closes != 1 || !file.closed {
			t.Errorf("file %d closed %d times, want exactly once", i, file.closes)
		}
	}
	if len(storage.packfiles) != 0 {
		t.Fatalf("failed replacement retained cache entries: %d", len(storage.packfiles))
	}
}
