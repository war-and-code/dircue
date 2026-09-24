package filesystem

import (
	"bytes"
	"compress/zlib"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing/cache"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing/format/objfile"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing/format/packfile"
)

func TestEncodedObjectSizeClosesLooseFile(t *testing.T) {
	closeError := errors.New("file close failed")
	for _, test := range []struct {
		name         string
		content      []byte
		closeError   error
		wantSize     int64
		wantError    error
		wantAnyError bool
	}{
		{name: "valid object", content: compressLooseObject(t, "blob 3\x00abc"), wantSize: 3},
		{name: "invalid zlib", content: []byte("not zlib"), wantAnyError: true},
		{name: "invalid header", content: compressLooseObject(t, "blob bad\x00abc"), wantError: objfile.ErrHeader},
		{name: "close error", content: compressLooseObject(t, "blob 3\x00abc"), closeError: closeError, wantError: closeError},
		{name: "header error precedes close error", content: compressLooseObject(t, "blob bad\x00abc"), closeError: closeError, wantError: objfile.ErrHeader},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			hash := plumbing.NewHash("0123456789012345678901234567890123456789")
			filename := filepath.Join(root, "objects", "01", hash.String()[2:])
			if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filename, test.content, 0o644); err != nil {
				t.Fatal(err)
			}
			fs := &trackedSizeFS{Filesystem: osfs.New(root), closeError: test.closeError}
			// Release leaked handles after the assertion so an unfixed Windows
			// failure reports the leak, without also failing TempDir cleanup.
			t.Cleanup(func() {
				for _, file := range fs.files {
					if !file.closed {
						_ = file.File.Close()
					}
				}
			})
			storage := NewStorage(fs, cache.NewObjectLRUDefault())
			size, err := storage.EncodedObjectSize(hash)
			if test.wantAnyError {
				if err == nil {
					t.Fatal("expected invalid zlib error")
				}
			} else if !errors.Is(err, test.wantError) {
				t.Fatalf("got error %v, want %v", err, test.wantError)
			}
			if err == nil && size != test.wantSize {
				t.Errorf("got size %d, want %d", size, test.wantSize)
			}
			if len(fs.files) != 1 {
				t.Fatalf("opened %d files, want 1", len(fs.files))
			}
			file := fs.files[0]
			if !file.closed || file.closes != 1 {
				t.Fatalf("loose object file closed %d times, want exactly 1", file.closes)
			}
		})
	}
}

func TestLargeLooseObjectReaderRecordsReopenedBytes(t *testing.T) {
	root := t.TempDir()
	hash := plumbing.NewHash("0123456789012345678901234567890123456789")
	content := "blob 12\x00hello world\n"
	filename := filepath.Join(root, "objects", "01", hash.String()[2:])
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, compressLooseObject(t, content), 0o644); err != nil {
		t.Fatal(err)
	}
	metrics := &packfile.ReadMetrics{}
	storage := NewStorageWithOptions(osfs.New(root), cache.NewObjectLRUDefault(), Options{
		LargeObjectThreshold: 1,
		ReadMetrics:          metrics,
	})
	object, err := storage.EncodedObject(plumbing.BlobObject, hash)
	if err != nil {
		t.Fatal(err)
	}
	before := metrics.Snapshot().LooseBytesRead
	reader, err := object.Reader()
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello world\n" {
		t.Fatalf("got %q", got)
	}
	after := metrics.Snapshot().LooseBytesRead
	if after <= before {
		t.Fatalf("lazy reader bytes did not advance: before=%d after=%d", before, after)
	}
}

func compressLooseObject(t *testing.T, content string) []byte {
	t.Helper()
	var result bytes.Buffer
	writer := zlib.NewWriter(&result)
	if _, err := writer.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

type trackedSizeFS struct {
	billy.Filesystem
	files      []*trackedSizeFile
	closeError error
}

func (fs *trackedSizeFS) Open(name string) (billy.File, error) {
	file, err := fs.Filesystem.Open(name)
	if err != nil {
		return nil, err
	}
	tracked := &trackedSizeFile{File: file, closeError: fs.closeError}
	fs.files = append(fs.files, tracked)
	return tracked, nil
}

type trackedSizeFile struct {
	billy.File
	closed     bool
	closes     int
	closeError error
}

func (file *trackedSizeFile) Close() error {
	file.closes++
	file.closed = true
	if err := file.File.Close(); err != nil {
		return err
	}
	return file.closeError
}
