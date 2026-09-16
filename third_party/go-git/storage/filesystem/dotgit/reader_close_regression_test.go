package dotgit

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
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/objfile"
)

func TestEncodedObjectReaderFileOwnership(t *testing.T) {
	closeError := errors.New("injected file close failure")
	for _, tc := range []struct {
		name       string
		content    []byte
		typ        plumbing.ObjectType
		size       int64
		wantError  error
		anyError   bool
		closeError error
	}{
		{name: "valid ownership transfer", content: compressedReaderObject(t, "blob 3\x00abc"), typ: plumbing.BlobObject, size: 3},
		{name: "valid close error", content: compressedReaderObject(t, "blob 3\x00abc"), typ: plumbing.BlobObject, size: 3, closeError: closeError},
		{name: "invalid zlib", content: []byte("not zlib"), typ: plumbing.BlobObject, size: 3, anyError: true, closeError: closeError},
		{name: "invalid header", content: compressedReaderObject(t, "blob bad\x00abc"), typ: plumbing.BlobObject, size: 3, wantError: objfile.ErrHeader, closeError: closeError},
		{name: "type mismatch", content: compressedReaderObject(t, "blob 3\x00abc"), typ: plumbing.TreeObject, size: 3, wantError: objfile.ErrHeader, closeError: closeError},
		{name: "size mismatch", content: compressedReaderObject(t, "blob 3\x00abc"), typ: plumbing.BlobObject, size: 4, wantError: objfile.ErrHeader, closeError: closeError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			hash := plumbing.NewHash("0123456789012345678901234567890123456789")
			filename := filepath.Join(root, "objects", "01", hash.String()[2:])
			if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filename, tc.content, 0644); err != nil {
				t.Fatal(err)
			}
			fs := &readerOwnershipFS{Filesystem: osfs.New(root), closeError: tc.closeError}
			t.Cleanup(func() {
				for _, file := range fs.files {
					if file.closes == 0 {
						_ = file.File.Close()
					}
				}
			})
			object := NewEncodedObject(New(fs), hash, tc.typ, tc.size)
			reader, err := object.Reader()
			if tc.anyError || tc.wantError != nil {
				if err == nil || reader != nil {
					t.Fatalf("got reader %v, error %v; expected failure", reader, err)
				}
				if errors.Is(err, closeError) {
					t.Fatalf("close error replaced primary error: %v", err)
				}
				if tc.wantError != nil && !errors.Is(err, tc.wantError) {
					t.Fatalf("got %v, want %v", err, tc.wantError)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if len(fs.files) != 1 || fs.files[0].closes != 0 {
					t.Fatal("file closed before ownership reached reader")
				}
				content, readErr := io.ReadAll(reader)
				closeErr := reader.Close()
				if readErr != nil || string(content) != "abc" {
					t.Fatalf("content %q, error %v", content, readErr)
				}
				if !errors.Is(closeErr, tc.closeError) {
					t.Fatalf("close error %v, want %v", closeErr, tc.closeError)
				}
			}
			if len(fs.files) != 1 || fs.files[0].closes != 1 {
				t.Fatalf("files opened=%d; close counts=%v", len(fs.files), fs.files)
			}
		})
	}
}

func compressedReaderObject(t *testing.T, value string) []byte {
	t.Helper()
	var result bytes.Buffer
	writer := zlib.NewWriter(&result)
	if _, err := io.WriteString(writer, value); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

type readerOwnershipFS struct {
	billy.Filesystem
	files      []*readerOwnershipFile
	closeError error
}

func (fs *readerOwnershipFS) Open(name string) (billy.File, error) {
	file, err := fs.Filesystem.Open(name)
	if err != nil {
		return nil, err
	}
	tracked := &readerOwnershipFile{File: file, closeError: fs.closeError}
	fs.files = append(fs.files, tracked)
	return tracked, nil
}

type readerOwnershipFile struct {
	billy.File
	closes     int
	closeError error
}

func (file *readerOwnershipFile) Close() error {
	file.closes++
	if err := file.File.Close(); err != nil {
		return err
	}
	return file.closeError
}
