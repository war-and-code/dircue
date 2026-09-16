package packfile

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
)

func TestFSObjectReaderFileOwnership(t *testing.T) {
	header := []byte{'P', 'A', 'C', 'K', 0, 0, 0, 2, 0, 0, 0, 1}
	var packed bytes.Buffer
	packed.Write(header)
	packed.WriteByte(0x33)
	compressor := zlib.NewWriter(&packed)
	if _, err := io.WriteString(compressor, "abc"); err != nil {
		t.Fatal(err)
	}
	if err := compressor.Close(); err != nil {
		t.Fatal(err)
	}
	checksum := sha1.Sum(packed.Bytes())
	packed.Write(checksum[:])
	closeError := errors.New("injected pack close failure")
	for _, tc := range []struct {
		name       string
		content    []byte
		failure    bool
		closeError error
	}{
		{name: "valid ownership transfer", content: packed.Bytes()},
		{name: "invalid object header", content: header, failure: true, closeError: closeError},
		{name: "invalid zlib", content: append(append([]byte{}, header...), 0x33, 'b', 'a', 'd'), failure: true, closeError: closeError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "objects.pack"), tc.content, 0644); err != nil {
				t.Fatal(err)
			}
			fs := &packOwnershipFS{Filesystem: osfs.New(root), closeError: tc.closeError}
			t.Cleanup(func() {
				for _, file := range fs.files {
					if file.closes == 0 {
						_ = file.File.Close()
					}
				}
			})
			object := NewFSObject(plumbing.NewHash("0123456789012345678901234567890123456789"), plumbing.BlobObject, 12, 3, nil, fs, "objects.pack", cache.NewObjectLRUDefault(), 1)
			reader, err := object.Reader()
			if tc.failure {
				if err == nil || reader != nil {
					t.Fatalf("reader %v, error %v; expected invalid pack failure", reader, err)
				}
				if errors.Is(err, closeError) {
					t.Fatalf("close error replaced primary error: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if len(fs.files) != 1 || fs.files[0].closes != 0 {
					t.Fatal("pack closed before reader received ownership")
				}
				content, readErr := io.ReadAll(reader)
				closeErr := reader.Close()
				if readErr != nil || closeErr != nil || string(content) != "abc" {
					t.Fatalf("content %q read=%v close=%v", content, readErr, closeErr)
				}
			}
			if len(fs.files) != 1 || fs.files[0].closes != 1 {
				t.Fatalf("files opened=%d; close counts=%v", len(fs.files), fs.files)
			}
		})
	}
}

type packOwnershipFS struct {
	billy.Filesystem
	files      []*packOwnershipFile
	closeError error
}

func (fs *packOwnershipFS) Open(name string) (billy.File, error) {
	file, err := fs.Filesystem.Open(name)
	if err != nil {
		return nil, err
	}
	tracked := &packOwnershipFile{File: file, closeError: fs.closeError}
	fs.files = append(fs.files, tracked)
	return tracked, nil
}

type packOwnershipFile struct {
	billy.File
	closes     int
	closeError error
}

func (file *packOwnershipFile) Close() error {
	file.closes++
	if err := file.File.Close(); err != nil {
		return err
	}
	return file.closeError
}
