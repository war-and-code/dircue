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

	"dircue/pkg/availability"
	"dircue/pkg/profile"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

type gitSnapshot struct {
	availability bool
	explainPath  string
	// go-git packed-object/index caches mutate during reads and are not concurrency safe.
	objectMu     sync.Mutex
	storage      io.Closer
	root         string
	tree         *object.Tree
	repo         *git.Repository
	maxTreeSize  int
	errorPolicy  ErrorPolicy
	infoRules    []attributeRule
	infoWarnings []profile.Warning
}

const maxGitTreeDepth = 1024

// Bound cached pack readers per snapshot; alternates retain their uncached policy.
const maxGitPackDescriptors = 8

func (s *gitSnapshot) close() error {
	if s == nil || s.storage == nil {
		return nil
	}
	storage := s.storage
	s.storage = nil
	return storage.Close()
}

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

func openGitSnapshot(ctx context.Context, directory string, opts Options, discover bool) (snapshot *gitSnapshot, err error) {
	if opts.Source == "directory" {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repo, err := git.PlainOpenWithOptions(directory, &git.PlainOpenOptions{DetectDotGit: discover, EnableDotGitCommonDir: true})
	if errors.Is(err, git.ErrRepositoryNotExists) && opts.Source == "auto" && opts.Revision == "" && opts.Tree == "" {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open Git repository: %w", err)
	}
	var retainedStorage *filesystem.Storage
	defer func() {
		if snapshot == nil && retainedStorage != nil {
			if closeErr := retainedStorage.Close(); err == nil && closeErr != nil {
				err = fmt.Errorf("close Git storage: %w", closeErr)
			}
		}
	}()
	// PlainOpen's default storage eagerly materializes every complete object.
	// Reopen using a bounded threshold so large non-delta objects are streamed.
	if storage, ok := repo.Storer.(*filesystem.Storage); ok {
		bounded := filesystem.NewStorageWithOptions(storage.Filesystem(), cache.NewObjectLRUDefault(), filesystem.Options{LargeObjectThreshold: ClassificationBytes, MaxOpenDescriptors: maxGitPackDescriptors})
		retainedStorage = bounded
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
	var tree *object.Tree
	if opts.Tree != "" {
		if !fullSHA1(opts.Tree) {
			return nil, errors.New("tree must be a full 40-character hexadecimal Git object ID")
		}
		tree, err = repo.TreeObject(plumbing.NewHash(opts.Tree))
		if err != nil {
			// When the object exists but is a commit, blob, or tag, tell the
			// caller what kind it actually is so a copy-pasted commit or blob
			// hash does not look like a missing object. The kind probe reuses
			// the same storage the tree lookup consulted.
			if errors.Is(err, plumbing.ErrObjectNotFound) {
				if obj, probeErr := repo.Object(plumbing.AnyObject, plumbing.NewHash(opts.Tree)); probeErr == nil && obj != nil {
					return nil, fmt.Errorf("resolve Git tree %q: object is a %s, not a tree", opts.Tree, obj.Type())
				}
			}
			return nil, fmt.Errorf("resolve Git tree %q: %w", opts.Tree, err)
		}
	} else {
		revision := opts.Revision
		if revision == "" {
			revision = "HEAD"
		}
		hash, resolveErr := repo.ResolveRevision(plumbing.Revision(revision))
		if resolveErr != nil {
			// Only a repository with no branch refs is treated as legitimately
			// unborn. Existing refs whose objects cannot be read fail closed.
			if opts.Source == "auto" && opts.Revision == "" && errors.Is(resolveErr, plumbing.ErrReferenceNotFound) && isUnbornRepository(repo) {
				return nil, nil
			}
			return nil, fmt.Errorf("resolve Git revision %q: %w", revision, resolveErr)
		}
		commit, commitErr := repo.CommitObject(*hash)
		if commitErr != nil {
			return nil, fmt.Errorf("resolve Git commit: %w", commitErr)
		}
		tree, err = commit.Tree()
		if err != nil {
			return nil, fmt.Errorf("read Git tree: %w", err)
		}
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
	snapshot = &gitSnapshot{root: root, tree: tree, repo: repo, storage: retainedStorage, maxTreeSize: opts.MaxTreeSize, errorPolicy: opts.ErrorPolicy}
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
				snapshot.infoRules, snapshot.infoWarnings, exceeded = parseGitAttributesBoundedFrom(".gitattributes", ".git/info/attributes", data, maxAttributeRules)
				if exceeded {
					snapshot.infoRules = nil
					snapshot.infoWarnings = append(snapshot.infoWarnings, profile.Warning{Path: ".git/info/attributes", Code: "unsupported_gitattributes", Message: fmt.Sprintf("attribute rules exceed %d rule limit; rules ignored", maxAttributeRules)})
				}
				for i := range snapshot.infoWarnings {
					snapshot.infoWarnings[i].Path = ".git/info/attributes"
				}
			}
		}
	}
	snapshot.availability = opts.Availability
	snapshot.explainPath = opts.ExplainPath
	return snapshot, nil
}

func fullSHA1(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func isUnbornRepository(repo *git.Repository) bool {
	head, err := repo.Reference(plumbing.HEAD, false)
	if err != nil || head.Type() != plumbing.SymbolicReference || !head.Target().IsBranch() {
		return false
	}
	refs, err := repo.References()
	if err != nil {
		return false
	}
	defer refs.Close()
	return refs.ForEach(func(ref *plumbing.Reference) error {
		if ref.Name().IsBranch() {
			return errors.New("branch reference exists")
		}
		return nil
	}) == nil
}

func blobRead(blob *object.Blob, filename string, limit int64) ([]byte, int64, error) {
	reader, err := blob.Reader()
	if err != nil {
		return nil, 0, recoverable(fmt.Errorf("read Git blob %s: %w", filename, err))
	}
	defer reader.Close()
	data, err := readAllBounded(reader, limit, blob.Size)
	if err != nil {
		return nil, 0, recoverable(fmt.Errorf("read Git blob %s: %w", filename, err))
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
			item := job{path: filename, size: -1}
			if s.availability && entry.Mode == filemode.Submodule {
				item.gitlink = &availability.Gitlink{Path: filename, Commit: entry.Hash.String()}
			}
			entries = append(entries, item)
			continue
		}
		size, err := s.repo.Storer.EncodedObjectSize(entry.Hash)
		if err != nil {
			if s.errorPolicy == ErrorPolicyContinue {
				if !send(result{path: filename, skipped: true, omission: "missing_git_object", warnings: []profile.Warning{{Path: filename, Code: "missing_git_object", Message: fmt.Sprintf("Git object could not be read; file skipped: %v", err)}}}) {
					return ctx.Err()
				}
				continue
			}
			return fmt.Errorf("read Git object size %s: %w", filename, err)
		}
		read := func(limit int64) ([]byte, int64, error) {
			s.objectMu.Lock()
			defer s.objectMu.Unlock()
			blob, err := s.repo.BlobObject(entry.Hash)
			if err != nil {
				return nil, 0, recoverable(fmt.Errorf("read Git blob %s: %w", filename, err))
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
				warnings = append(warnings, profile.Warning{Path: filename, Code: "unsupported_gitattributes", Message: fmt.Sprintf("attribute rules exceed %d rule limit; rules ignored", maxAttributeRules)})
				continue
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
			if !send(result{path: entry.path, skipped: true, gitlink: entry.gitlink}) {
				return ctx.Err()
			}
			continue
		}
		var err error
		if entry.path == s.explainPath {
			entry.attrs, entry.traceOverrides, err = resolveAttributesTraceContext(ctx, entry.path, rules)
		} else {
			entry.attrs, err = resolveAttributesContext(ctx, entry.path, rules)
		}
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
