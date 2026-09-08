package scanner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"dircue/pkg/profile"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

type gitSnapshot struct {
	// go-git packed-object/index caches mutate during reads and are not concurrency safe.
	objectMu     sync.Mutex
	root         string
	tree         *object.Tree
	repo         *git.Repository
	maxTreeSize  int
	infoRules    []attributeRule
	infoWarnings []profile.Warning
}

const maxGitTreeDepth = 1024

type gitTreeFrame struct {
	tree *object.Tree
	base string
	next int
}

type gitTreeIterator struct {
	repo  *git.Repository
	stack []gitTreeFrame
}

func newGitTreeIterator(repo *git.Repository, tree *object.Tree) *gitTreeIterator {
	return &gitTreeIterator{repo: repo, stack: []gitTreeFrame{{tree: tree}}}
}

// Next walks stored tree objects without requiring names that go-git can write
// to a worktree. Git permits tabs, newlines, and other controls in tree names.
// We validate path components and look up objects by hash; stored names are
// never passed to filesystem APIs.
func (w *gitTreeIterator) Next() (string, object.TreeEntry, error) {
	for len(w.stack) > 0 {
		current := &w.stack[len(w.stack)-1]
		if current.next == len(current.tree.Entries) {
			w.stack = w.stack[:len(w.stack)-1]
			continue
		}
		entry := current.tree.Entries[current.next]
		current.next++
		if err := validateGitTreeComponent(entry.Name); err != nil {
			return "", object.TreeEntry{}, err
		}
		filename := entry.Name
		if current.base != "" {
			filename = current.base + "/" + entry.Name
		}
		if entry.Mode == filemode.Dir {
			if len(w.stack) > maxGitTreeDepth {
				return "", object.TreeEntry{}, object.ErrMaxTreeDepth
			}
			tree, err := w.repo.TreeObject(entry.Hash)
			if err != nil {
				return "", object.TreeEntry{}, err
			}
			w.stack = append(w.stack, gitTreeFrame{tree: tree, base: filename})
		}
		return filename, entry, nil
	}
	return "", object.TreeEntry{}, io.EOF
}

func validateGitTreeComponent(name string) error {
	if name == "" || strings.Contains(name, "/") || strings.IndexByte(name, 0) >= 0 || !utf8.ValidString(name) {
		return fmt.Errorf("unsafe Git tree component %q", name)
	}
	// Backslash is a valid Git filename byte on Unix, but treating its segments
	// as structural here also prevents a stored path becoming a traversal path
	// if a consumer later interprets the report on Windows.
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '\\' })
	if len(parts) == 0 {
		return fmt.Errorf("unsafe Git tree component %q", name)
	}
	for _, part := range parts {
		if part == "." || part == ".." || strings.EqualFold(part, ".git") || strings.EqualFold(part, "git~1") {
			return fmt.Errorf("unsafe Git tree component %q", name)
		}
	}
	return nil
}

func (s *gitSnapshot) findEntry(filename string) (*object.TreeEntry, error) {
	if filename == "" || strings.HasPrefix(filename, "/") || strings.HasSuffix(filename, "/") {
		return nil, fmt.Errorf("unsafe Git path %q", filename)
	}
	parts := strings.Split(filename, "/")
	tree := s.tree
	for i, part := range parts {
		if err := validateGitTreeComponent(part); err != nil {
			return nil, err
		}
		var found *object.TreeEntry
		for j := range tree.Entries {
			if tree.Entries[j].Name == part {
				found = &tree.Entries[j]
				break
			}
		}
		if found == nil {
			return nil, object.ErrEntryNotFound
		}
		if i == len(parts)-1 {
			return found, nil
		}
		if found.Mode != filemode.Dir {
			return nil, object.ErrDirectoryNotFound
		}
		if i >= maxGitTreeDepth {
			return nil, object.ErrMaxTreeDepth
		}
		var err error
		tree, err = s.repo.TreeObject(found.Hash)
		if err != nil {
			return nil, err
		}
	}
	return nil, object.ErrEntryNotFound
}

// exceedsTreeLimit counts tree entries without resolving blob sizes, allocating
// read closures, or parsing attributes. Trees at the limit return an empty
// report before any files are classified.
func (s *gitSnapshot) exceedsTreeLimit(ctx context.Context) (bool, error) {
	stack := []gitTreeFrame{{tree: s.tree}}
	count := 0
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		current := &stack[len(stack)-1]
		if current.next == len(current.tree.Entries) {
			stack = stack[:len(stack)-1]
			continue
		}
		entry := current.tree.Entries[current.next]
		current.next++
		if err := validateGitTreeComponent(entry.Name); err != nil {
			return false, err
		}
		if entry.Mode == filemode.Dir {
			if len(stack) > maxGitTreeDepth {
				return false, object.ErrMaxTreeDepth
			}
			tree, err := s.repo.TreeObject(entry.Hash)
			if err != nil {
				return false, err
			}
			stack = append(stack, gitTreeFrame{tree: tree})
			continue
		}
		count++
		if count >= s.maxTreeSize {
			return true, nil
		}
	}
	return false, nil
}

func openGitSnapshot(ctx context.Context, directory string, opts Options, discover bool) (*gitSnapshot, error) {
	if opts.Source == "directory" {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repo, err := git.PlainOpenWithOptions(directory, &git.PlainOpenOptions{DetectDotGit: discover, EnableDotGitCommonDir: true})
	if errors.Is(err, git.ErrRepositoryNotExists) && opts.Source == "auto" && opts.Revision == "" {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open Git repository: %w", err)
	}
	// PlainOpen's default storage eagerly materializes every complete object.
	// Reopen using a bounded threshold so large non-delta objects are streamed.
	if storage, ok := repo.Storer.(*filesystem.Storage); ok {
		bounded := filesystem.NewStorageWithOptions(storage.Filesystem(), cache.NewObjectLRUDefault(), filesystem.Options{LargeObjectThreshold: ClassificationBytes})
		wt, wtErr := repo.Worktree()
		if wtErr == nil {
			repo, err = git.Open(bounded, wt.Filesystem)
		} else {
			repo, err = git.Open(bounded, nil)
		}
		if err != nil {
			return nil, fmt.Errorf("open bounded Git storage: %w", err)
		}
	}
	revision := opts.Revision
	if revision == "" {
		revision = "HEAD"
	}
	hash, err := repo.ResolveRevision(plumbing.Revision(revision))
	if err != nil {
		// An initialized but uncommitted directory has useful source even though
		// there is no Git tree. Explicit Git/revision requests still fail.
		if opts.Source == "auto" && opts.Revision == "" && errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("resolve Git revision %q: %w", revision, err)
	}
	commit, err := repo.CommitObject(*hash)
	if err != nil {
		return nil, fmt.Errorf("resolve Git commit: %w", err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("read Git tree: %w", err)
	}
	root := directory
	if discover {
		if wt, err := repo.Worktree(); err == nil {
			root = wt.Filesystem.Root()
		}
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	snapshot := &gitSnapshot{root: root, tree: tree, repo: repo, maxTreeSize: opts.MaxTreeSize}
	if storage, ok := repo.Storer.(*filesystem.Storage); ok {
		files, err := attributeRoot(storage.Filesystem().Root())
		if err != nil {
			return nil, err
		}
		defer files.Close()
		info, err := files.Lstat("info/attributes")
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("inspect Git info attributes: %w", err)
		}
		if err == nil {
			if !info.Mode().IsRegular() || info.Size() > maxAttributesBytes {
				snapshot.infoWarnings = append(snapshot.infoWarnings, profile.Warning{Path: ".git/info/attributes", Code: "unsupported_gitattributes", Message: "non-regular or oversized attribute file ignored"})
			} else {
				// The same confined, no-follow/nonblocking regular-file reader
				// used for checkout files prevents symlink escape and FIFO races.
				data, tooLarge, err := readBounded(files, "info/attributes", maxAttributesBytes)
				if err != nil {
					return nil, fmt.Errorf("read Git info attributes: %w", err)
				}
				if tooLarge {
					return nil, fmt.Errorf("Git info attributes exceeds 1 MiB")
				}
				var exceeded bool
				snapshot.infoRules, snapshot.infoWarnings, exceeded = parseGitAttributesBounded(".gitattributes", data, maxAttributeRules)
				if exceeded {
					return nil, fmt.Errorf("attribute rules exceed %d rule limit", maxAttributeRules)
				}
				for i := range snapshot.infoWarnings {
					snapshot.infoWarnings[i].Path = ".git/info/attributes"
				}
			}
		}
	}
	return snapshot, nil
}

func blobRead(blob *object.Blob, filename string, limit int64) ([]byte, int64, error) {
	reader, err := blob.Reader()
	if err != nil {
		return nil, 0, fmt.Errorf("read Git blob %s: %w", filename, err)
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, limit))
	if err != nil {
		return nil, 0, fmt.Errorf("read Git blob %s: %w", filename, err)
	}
	return data, blob.Size, nil
}

func (s *gitSnapshot) walk(ctx context.Context, jobs chan<- job, send func(result) bool) error {
	exceeded, err := s.exceedsTreeLimit(ctx)
	if err != nil {
		return fmt.Errorf("walk Git tree: %w", err)
	}
	if exceeded {
		if !send(result{warnings: []profile.Warning{{Path: ".", Code: "tree_size_limit", Message: fmt.Sprintf("Git tree has at least %d entries; language analysis omitted", s.maxTreeSize)}}}) {
			return ctx.Err()
		}
		return nil
	}
	var entries []job
	var rules []attributeRule
	warnings := slices.Clone(s.infoWarnings)
	walker := newGitTreeIterator(s.repo, s.tree)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		filename, entry, err := walker.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("walk Git tree: %w", err)
		}
		if entry.Mode == filemode.Dir {
			continue
		}
		if entry.Mode != filemode.Regular && entry.Mode != filemode.Executable && entry.Mode != filemode.Deprecated {
			entries = append(entries, job{path: filename, size: -1})
			continue
		}
		size, err := s.repo.Storer.EncodedObjectSize(entry.Hash)
		if err != nil {
			return fmt.Errorf("read Git object size %s: %w", filename, err)
		}
		read := func(limit int64) ([]byte, int64, error) {
			s.objectMu.Lock()
			defer s.objectMu.Unlock()
			blob, err := s.repo.BlobObject(entry.Hash)
			if err != nil {
				return nil, 0, fmt.Errorf("read Git blob %s: %w", filename, err)
			}
			return blobRead(blob, filename, limit)
		}
		entries = append(entries, job{path: filename, size: size, read: read})
		if path.Base(filename) == ".gitattributes" {
			if size > maxAttributesBytes {
				warnings = append(warnings, profile.Warning{Path: filename, Code: "unsupported_gitattributes", Message: "attribute file exceeds 1 MiB; rules ignored"})
				continue
			}
			data, _, err := read(maxAttributesBytes)
			if err != nil {
				return err
			}
			parsed, notices, exceeded := parseGitAttributesBounded(filename, data, maxAttributeRules-len(s.infoRules)-len(rules))
			if exceeded {
				return fmt.Errorf("attribute rules exceed %d rule limit", maxAttributeRules)
			}
			rules = append(rules, parsed...)
			warnings = append(warnings, notices...)
		}
	}
	// Parent scopes precede child scopes even if the lexical tree walk encountered
	// a parent's attribute file after another sibling. Preserve each file's order.
	slices.SortStableFunc(rules, func(a, b attributeRule) int {
		return strings.Count(a.scope, "/") + boolInt(a.scope != "") - strings.Count(b.scope, "/") - boolInt(b.scope != "")
	})
	rules = append(rules, s.infoRules...)
	if len(warnings) > 0 && !send(result{warnings: warnings}) {
		return ctx.Err()
	}
	for _, entry := range entries {
		if entry.size < 0 {
			if !send(result{path: entry.path, skipped: true}) {
				return ctx.Err()
			}
			continue
		}
		var err error
		entry.attrs, err = resolveAttributesContext(ctx, entry.path, rules)
		if err != nil {
			return err
		}
		select {
		case jobs <- entry:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// Git's commondir deliberately permits a linked worktree to reference another
// metadata directory. Resolve that boundary once, then confine attribute reads
// within it; never use an ordinary blocking Open on repository-controlled paths.
func attributeRoot(metadataPath string) (*os.Root, error) {
	root, err := os.OpenRoot(metadataPath)
	if err != nil {
		return nil, err
	}
	info, err := root.Lstat("commondir")
	if errors.Is(err, fs.ErrNotExist) {
		return root, nil
	}
	if err != nil {
		root.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		root.Close()
		return nil, fmt.Errorf("Git commondir must be a regular file")
	}
	data, large, err := readBounded(root, "commondir", 4096)
	root.Close()
	if err != nil {
		return nil, err
	}
	if large {
		return nil, fmt.Errorf("Git commondir exceeds 4096 bytes")
	}
	common := strings.TrimSpace(string(data))
	if common == "" {
		return nil, fmt.Errorf("Git commondir is empty")
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(metadataPath, common)
	}
	return os.OpenRoot(common)
}
