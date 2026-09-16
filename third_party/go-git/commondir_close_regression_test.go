package git

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
)

func TestCommonDirectoryClosesMetadataFile(t *testing.T) {
	readError := errors.New("metadata read failed")
	closeError := errors.New("metadata close failed")
	for _, test := range []struct {
		name          string
		content       string
		readError     error
		closeError    error
		wantError     error
		wantDirectory bool
	}{
		{name: "relative common directory", content: ".\n", wantDirectory: true},
		{name: "empty metadata", content: ""},
		{name: "missing target", content: "missing-directory\n", wantError: ErrRepositoryIncomplete},
		{name: "read error", content: ".\n", readError: readError, wantError: readError},
		{name: "close error", content: ".\n", closeError: closeError, wantError: closeError},
		{name: "read error precedes close error", content: ".\n", readError: readError, closeError: closeError, wantError: readError},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "commondir"), []byte(test.content), 0o644); err != nil {
				t.Fatal(err)
			}
			fs := &trackedCommonDirFS{Filesystem: osfs.New(root), readError: test.readError, closeError: test.closeError}
			// Close leaked handles after checking ownership so an unfixed Windows
			// failure does not also prevent temporary-directory cleanup.
			t.Cleanup(func() {
				for _, file := range fs.files {
					if !file.closed {
						_ = file.File.Close()
					}
				}
			})
			directory, err := dotGitCommonDirectory(fs)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("got error %v, want %v", err, test.wantError)
			}
			if err == nil && (directory != nil) != test.wantDirectory {
				t.Errorf("directory present=%v, want %v", directory != nil, test.wantDirectory)
			}
			if len(fs.files) != 1 {
				t.Fatalf("opened %d files, want 1", len(fs.files))
			}
			file := fs.files[0]
			if !file.closed || file.closes != 1 {
				t.Fatalf("commondir file closed %d times, want exactly 1", file.closes)
			}
		})
	}
}

type trackedCommonDirFS struct {
	billy.Filesystem
	files      []*trackedCommonDirFile
	readError  error
	closeError error
}

func (fs *trackedCommonDirFS) Open(name string) (billy.File, error) {
	file, err := fs.Filesystem.Open(name)
	if err != nil {
		return nil, err
	}
	tracked := &trackedCommonDirFile{File: file, readError: fs.readError, closeError: fs.closeError}
	fs.files = append(fs.files, tracked)
	return tracked, nil
}

type trackedCommonDirFile struct {
	billy.File
	closed     bool
	closes     int
	readError  error
	closeError error
}

func (file *trackedCommonDirFile) Read(content []byte) (int, error) {
	if file.readError != nil {
		return 0, file.readError
	}
	return file.File.Read(content)
}
func (file *trackedCommonDirFile) Close() error {
	file.closes++
	file.closed = true
	if err := file.File.Close(); err != nil {
		return err
	}
	return file.closeError
}
