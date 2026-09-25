package treehash

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/war-and-code/dircue/third_party/go-git/plumbing/filemode"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing/format/index"
)

const (
	maxMetadataBytes = 64 << 10
	maxIndexBytes    = 256 << 20
)

// readSmall reads a bounded regular file inside root.
func readSmall(root *os.Root, name string, limit int64) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", name)
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", name, limit)
	}
	f, err := openRegular(root, name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", name, limit)
	}
	return data, nil
}

// gitDirFor returns the root-relative Git directory for a working tree at dir
// (root-relative, "." for the root). A ".git" file's gitdir pointer is followed
// only when it stays inside the selected root.
func gitDirFor(root *os.Root, dir string) (string, bool) {
	dotgit := path.Join(dir, ".git")
	info, err := root.Lstat(dotgit)
	if err != nil {
		return "", false
	}
	if info.IsDir() {
		return dotgit, true
	}
	if !info.Mode().IsRegular() {
		return "", false
	}
	data, err := readSmall(root, dotgit, maxMetadataBytes)
	if err != nil {
		return "", false
	}
	line := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
	target, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return "", false
	}
	target = strings.TrimSpace(target)
	if target == "" || path.IsAbs(target) || strings.Contains(target, "\\") {
		return "", false
	}
	resolved := path.Clean(path.Join(dir, target))
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return "", false
	}
	return resolved, true
}

// commonDirFor follows a worktree's "commondir" pointer, confined to root.
func commonDirFor(root *os.Root, gitDir string) string {
	data, err := readSmall(root, path.Join(gitDir, "commondir"), maxMetadataBytes)
	if err != nil {
		return gitDir
	}
	target := strings.TrimSpace(string(data))
	if target == "" || path.IsAbs(target) {
		return gitDir
	}
	resolved := path.Clean(path.Join(gitDir, target))
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return gitDir
	}
	return resolved
}

var errUnresolved = errors.New("repository HEAD is not resolvable")

// resolveHEAD returns the commit checked out in the Git directory, reading
// HEAD, loose refs and packed-refs as data. It never runs Git.
func resolveHEAD(root *os.Root, gitDir string, format Format) (string, error) {
	data, err := readSmall(root, path.Join(gitDir, "HEAD"), maxMetadataBytes)
	if err != nil {
		return "", errUnresolved
	}
	head := strings.TrimSpace(string(data))
	common := commonDirFor(root, gitDir)
	for depth := 0; depth < 8; depth++ {
		ref, symbolic := strings.CutPrefix(head, "ref:")
		if !symbolic {
			if validObjectID(head, format) {
				return strings.ToLower(head), nil
			}
			return "", errUnresolved
		}
		ref = strings.TrimSpace(ref)
		if !strings.HasPrefix(ref, "refs/") || strings.Contains(ref, "..") {
			return "", errUnresolved
		}
		next, found := "", false
		for _, dir := range []string{gitDir, common} {
			if loose, err := readSmall(root, path.Join(dir, ref), maxMetadataBytes); err == nil {
				next, found = strings.TrimSpace(string(loose)), true
				break
			}
		}
		if !found {
			packed, err := readSmall(root, path.Join(common, "packed-refs"), maxIndexBytes)
			if err != nil {
				return "", errUnresolved
			}
			for _, line := range strings.Split(string(packed), "\n") {
				id, name, ok := strings.Cut(strings.TrimSpace(line), " ")
				if ok && name == ref {
					next, found = id, true
					break
				}
			}
			if !found {
				return "", errUnresolved
			}
		}
		head = next
	}
	return "", errUnresolved
}

func validObjectID(v string, format Format) bool {
	if len(v) != format.hexLen() {
		return false
	}
	for _, c := range v {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// repositoryIndex is the subset of a root repository's index that changes
// what "git add -A" records: tracked paths are never ignored, uninitialized
// submodules keep their recorded gitlink, and sparse entries stay recorded.
type repositoryIndex struct {
	tracked      map[string]bool
	blobs        map[string]string // path -> indexed blob ID (stage 0)
	trackedDirs  map[string]bool
	gitlinks     map[string]string
	skipWorktree int
}

// readRepositoryIndex decodes $GIT_DIR/index for a SHA-1 repository. It is
// bounded and returns an error for anything it cannot interpret exactly.
func readRepositoryIndex(root *os.Root, gitDir string) (idx *repositoryIndex, err error) {
	config, _ := readSmall(root, path.Join(commonDirFor(root, gitDir), "config"), maxMetadataBytes)
	lower := bytes.ToLower(config)
	if bytes.Contains(lower, []byte("objectformat")) && bytes.Contains(lower, []byte("sha256")) {
		return nil, errors.New("repository object format is not SHA-1")
	}
	data, err := readSmall(root, path.Join(gitDir, "index"), maxIndexBytes)
	if err != nil {
		return nil, err
	}
	defer func() {
		if r := recover(); r != nil {
			idx, err = nil, fmt.Errorf("index decode failed: %v", r)
		}
	}()
	var decoded index.Index
	if err := index.NewDecoder(bytes.NewReader(data)).Decode(&decoded); err != nil {
		return nil, err
	}
	out := &repositoryIndex{tracked: map[string]bool{}, blobs: map[string]string{}, trackedDirs: map[string]bool{}, gitlinks: map[string]string{}}
	for _, e := range decoded.Entries {
		name := e.Name
		out.tracked[name] = true
		// Stage 0 is a normal, merged entry. go-git names stage 1 "Merged",
		// but its decoder stores the raw stage bits, so compare with zero.
		if e.Stage == 0 && e.Mode != filemode.Submodule {
			out.blobs[name] = e.Hash.String()
		}
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			if out.trackedDirs[dir] {
				break
			}
			out.trackedDirs[dir] = true
		}
		if e.Mode == filemode.Submodule {
			out.gitlinks[name] = e.Hash.String()
		}
		if e.SkipWorktree {
			out.skipWorktree++
		}
	}
	return out, nil
}
