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
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/war-and-code/dircue/pkg/availability"
	"github.com/war-and-code/dircue/pkg/profile"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing/cache"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing/filemode"
	formatcfg "github.com/war-and-code/dircue/third_party/go-git/plumbing/format/config"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing/object"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing/storer"
	"github.com/war-and-code/dircue/third_party/go-git/storage/filesystem"
	"github.com/war-and-code/dircue/third_party/go-git/storage/filesystem/dotgit"
)

type gitSnapshot struct {
	availability bool
	explainPath  string
	lanes        *gitObjectLanes
	storages     []io.Closer
	root         string
	tree         *object.Tree
	storage      *filesystem.Storage
	commit       plumbing.Hash
	maxTreeSize  int
	errorPolicy  ErrorPolicy
	infoRules    []attributeRule
	infoWarnings []profile.Warning
}

const maxGitTreeDepth = 1024

const (
	maxGitObjectLanes = 3
	// Preserve the pre-lane retained-reader capacity when one storage is enough.
	maxGitPackDescriptorsSingleLane = 8
	// This bounds retained pack readers. Lazy objects and nested delta bases
	// open transient readers whose peak depends on the pack's delta graph.
	maxGitPackDescriptorsPerConcurrentLane = 2
)

func gitObjectLaneCount(workers int) int {
	return min(max(workers, 1), maxGitObjectLanes)
}

func gitPackDescriptorLimit(laneCount int) int {
	if laneCount == 1 {
		return maxGitPackDescriptorsSingleLane
	}
	return maxGitPackDescriptorsPerConcurrentLane
}

type gitObjectLane struct {
	storage *filesystem.Storage
}

// gitObjectLanes serializes each mutable go-git filesystem storage while
// allowing independent storages to decode objects concurrently. Every lane
// shares one thread-safe, size-bounded object cache.
type gitObjectLanes struct {
	// primaryStorage is also one of the available lanes. Direct use is confined
	// to snapshot setup, the serial tree prepass, and single-file inspection;
	// those phases never overlap leased worker reads.
	primaryStorage *filesystem.Storage
	available      chan *gitObjectLane
}

func newGitObjectLanes(repositoryFS billy.Filesystem, objectCache cache.Object, options filesystem.Options, count int) (*gitObjectLanes, []*filesystem.Storage, error) {
	if count < 1 {
		return nil, nil, errors.New("Git object lane count must be positive")
	}
	lanes := &gitObjectLanes{available: make(chan *gitObjectLane, count)}
	storages := make([]*filesystem.Storage, 0, count)
	for range count {
		storage := filesystem.NewStorageWithOptions(repositoryFS, objectCache, options)
		storages = append(storages, storage)
		if lanes.primaryStorage == nil {
			lanes.primaryStorage = storage
		}
		lanes.available <- &gitObjectLane{storage: storage}
	}
	return lanes, storages, nil
}

func (l *gitObjectLanes) acquire(ctx context.Context) (*gitObjectLane, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	select {
	case lane := <-l.available:
		return lane, func() { l.available <- lane }, nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

func (l *gitObjectLanes) read(ctx context.Context, hash plumbing.Hash, filename string, limit int64) ([]byte, int64, error) {
	lane, release, err := l.acquire(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer release()
	blob, err := object.GetBlob(lane.storage, hash)
	if err != nil {
		return nil, 0, recoverable(fmt.Errorf("read Git blob %s: %w", filename, err))
	}
	return blobRead(blob, filename, limit)
}

func (s *gitSnapshot) close() error {
	if s == nil || len(s.storages) == 0 {
		return nil
	}
	storages := s.storages
	s.storages = nil
	var err error
	for _, storage := range storages {
		err = errors.Join(err, storage.Close())
	}
	return err
}

type gitTreeFrame struct {
	tree *object.Tree
	base string
	next int
}

type gitTreeIterator struct {
	storage storer.EncodedObjectStorer
	stack   []gitTreeFrame
}

func newGitTreeIterator(storage storer.EncodedObjectStorer, tree *object.Tree) *gitTreeIterator {
	return &gitTreeIterator{storage: storage, stack: []gitTreeFrame{{tree: tree}}}
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
			tree, err := object.GetTree(w.storage, entry.Hash)
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
		tree, err = object.GetTree(s.storage, found.Hash)
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
			tree, err := object.GetTree(s.storage, entry.Hash)
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

// openGitSnapshot opens the git snapshot for the given directory.
// It returns a non-nil fallbackWarning when --source auto silently switched to
// directory mode because a .git directory was found but could not be used.
// The warning message is safe for display (no absolute host paths).
func openGitSnapshot(ctx context.Context, directory string, opts Options, discover bool, laneCount int) (snap *gitSnapshot, fallbackWarning *profile.Warning, err error) {
	return openGitSnapshotWithAttributeRoot(ctx, directory, opts, discover, laneCount, attributeRoot)
}

func openGitSnapshotWithAttributeRoot(ctx context.Context, directory string, opts Options, discover bool, laneCount int, openAttributeRoot func(string) (*os.Root, error)) (snapshot *gitSnapshot, fallbackWarning *profile.Warning, err error) {
	if opts.Source == "directory" {
		return nil, nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	repositoryFS, root, metadataPath, err := openLocalGitFilesystem(directory, discover)
	if errors.Is(err, errGitRepositoryNotFound) && opts.Source == "auto" && opts.Revision == "" && opts.Tree == "" {
		// No .git directory present; directory mode is the expected and silent path.
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("open Git repository: %w", err)
	}
	objectCache := cache.Object(cache.NewObjectLRUDefault())
	if opts.GitObjectCacheBytes > 0 {
		objectCache = cache.NewObjectLRU(cache.FileSize(opts.GitObjectCacheBytes))
	}
	if opts.GitReadMetrics != nil {
		objectCache = &metricsObjectCache{Object: objectCache, metrics: opts.GitReadMetrics}
	}
	lanes, retainedStorages, err := newGitObjectLanes(repositoryFS, objectCache, filesystem.Options{
		LargeObjectThreshold: ClassificationBytes,
		MaxOpenDescriptors:   gitPackDescriptorLimit(laneCount),
		ReadMetrics:          opts.GitReadMetrics,
	}, laneCount)
	if err != nil {
		return nil, nil, fmt.Errorf("open bounded Git storage: %w", err)
	}
	defer func() {
		if snapshot == nil {
			var closeErr error
			for _, storage := range retainedStorages {
				closeErr = errors.Join(closeErr, storage.Close())
			}
			if err == nil && closeErr != nil {
				err = fmt.Errorf("close Git storage: %w", closeErr)
			}
		}
	}()
	storage := lanes.primaryStorage

	// Detect SHA-256 object-format repositories.  go-git is compiled without
	// SHA-256 support (no "sha256" build tag), so it cannot read objects from
	// these repositories and would produce a misleading "object not found"
	// error later.  Catch the situation early with a clear message instead.
	//
	// Note: config.Config.Extensions.ObjectFormat is NOT populated by
	// config.Unmarshal in the current go-git fork (unmarshalExtensions is
	// absent).  We use the raw parsed section instead.
	if cfg, cfgErr := storage.Config(); cfgErr == nil {
		if cfg.Raw.Section("extensions").Options.Get("objectformat") == string(formatcfg.SHA256) {
			if opts.Source == "auto" && opts.Revision == "" && opts.Tree == "" {
				w := &profile.Warning{
					Path:    ".git/config",
					Code:    "git_object_format_unsupported",
					Message: "repository uses sha256 object format; git mode not available, using directory scan",
				}
				return nil, w, nil
			}
			return nil, nil, errors.New("unsupported Git object format sha256: use --source directory")
		}
	}

	if _, err := storage.Reference(plumbing.HEAD); err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) && opts.Source == "auto" && opts.Revision == "" && opts.Tree == "" {
			w := &profile.Warning{
				Path:    ".git/HEAD",
				Code:    "git_head_not_found",
				Message: "HEAD reference not found; git mode not available, using directory scan",
			}
			return nil, w, nil
		}
		return nil, nil, fmt.Errorf("open Git repository: %w", err)
	}

	var tree *object.Tree
	var selectedCommit plumbing.Hash
	if opts.Tree != "" {
		if !fullSHA1(opts.Tree) {
			return nil, nil, errors.New("tree must be a full 40-character hexadecimal Git object ID")
		}
		hash := plumbing.NewHash(opts.Tree)
		tree, err = object.GetTree(storage, hash)
		if err != nil {
			if errors.Is(err, plumbing.ErrObjectNotFound) {
				if encoded, probeErr := storage.EncodedObject(plumbing.AnyObject, hash); probeErr == nil {
					return nil, nil, fmt.Errorf("resolve Git tree %q: object is a %s, not a tree", opts.Tree, encoded.Type())
				}
			}
			return nil, nil, fmt.Errorf("resolve Git tree %q: %w", opts.Tree, err)
		}
	} else {
		revision := opts.Revision
		if revision == "" {
			revision = "HEAD"
		}
		selectedCommit, err = resolveLocalRevision(storage, revision)
		if err != nil {
			if opts.Source == "auto" && opts.Revision == "" && errors.Is(err, plumbing.ErrReferenceNotFound) && isUnbornRepository(storage) {
				// An unborn repository (no commits yet) or a corrupt gitdir
				// (e.g. .git/commondir pointing to a path that does not hold
				// the expected refs) looks identical from git mode's point of
				// view: HEAD points to a branch that has no objects.  Fall
				// back to directory scan and emit a warning so the user knows
				// git mode was not active.
				w := &profile.Warning{
					Path:    ".git",
					Code:    "git_no_commits_or_corrupt_gitdir",
					Message: "HEAD branch has no commits visible in the reference store (unborn repository or corrupt commondir); using directory scan",
				}
				return nil, w, nil
			}
			return nil, nil, fmt.Errorf("resolve Git revision %q: %w", revision, err)
		}
		commit, commitErr := object.GetCommit(storage, selectedCommit)
		if commitErr != nil {
			return nil, nil, fmt.Errorf("resolve Git commit: %w", commitErr)
		}
		tree, err = object.GetTree(storage, commit.TreeHash)
		if err != nil {
			return nil, nil, fmt.Errorf("read Git tree: %w", err)
		}
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	ownedStorages := make([]io.Closer, len(retainedStorages))
	for i := range retainedStorages {
		ownedStorages[i] = retainedStorages[i]
	}
	snapshot = &gitSnapshot{root: root, tree: tree, storage: storage, commit: selectedCommit, lanes: lanes, storages: ownedStorages, maxTreeSize: opts.MaxTreeSize, errorPolicy: opts.ErrorPolicy}
	files, err := openAttributeRoot(metadataPath)
	if err != nil {
		return nil, nil, err
	}
	defer files.Close()
	info, err := files.Lstat("info/attributes")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, fmt.Errorf("inspect Git info attributes: %w", err)
	}
	if err == nil {
		if !info.Mode().IsRegular() || info.Size() > maxAttributesBytes {
			snapshot.infoWarnings = append(snapshot.infoWarnings, profile.Warning{Path: ".git/info/attributes", Code: "unsupported_gitattributes", Message: "non-regular or oversized attribute file ignored"})
		} else {
			data, tooLarge, err := readBounded(files, "info/attributes", maxAttributesBytes)
			if err != nil {
				return nil, nil, fmt.Errorf("read Git info attributes: %w", err)
			}
			if tooLarge {
				return nil, nil, fmt.Errorf("Git info attributes exceeds 1 MiB")
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
	snapshot.availability = opts.Availability
	snapshot.explainPath = opts.ExplainPath
	return snapshot, nil, nil
}

var errGitRepositoryNotFound = errors.New("Git repository does not exist")

func openLocalGitFilesystem(directory string, discover bool) (billy.Filesystem, string, string, error) {
	root, err := filepath.Abs(directory)
	if err != nil {
		return nil, "", "", err
	}
	for {
		checkout, openErr := os.OpenRoot(root)
		if openErr != nil {
			return nil, "", "", openErr
		}
		info, statErr := checkout.Lstat(".git")
		if statErr == nil {
			metadataPath := filepath.Join(root, ".git")
			switch {
			case info.IsDir():
				checkout.Close()
			case info.Mode().IsRegular():
				if info.Size() > 4096 {
					checkout.Close()
					return nil, "", "", errors.New("Git metadata pointer exceeds 4096 bytes")
				}
				data, tooLarge, readErr := readBounded(checkout, ".git", 4096)
				checkout.Close()
				if readErr != nil {
					return nil, "", "", readErr
				}
				if tooLarge {
					return nil, "", "", errors.New("Git metadata pointer exceeds 4096 bytes")
				}
				value := strings.TrimSpace(string(data))
				if !strings.HasPrefix(value, "gitdir: ") || strings.TrimSpace(strings.TrimPrefix(value, "gitdir: ")) == "" {
					return nil, "", "", errors.New("invalid Git metadata pointer")
				}
				metadataPath = strings.TrimSpace(strings.TrimPrefix(value, "gitdir: "))
				if !filepath.IsAbs(metadataPath) {
					metadataPath = filepath.Join(root, metadataPath)
				}
				metadataPath = filepath.Clean(metadataPath)
			default:
				checkout.Close()
				return nil, "", "", errors.New("Git metadata path is not a directory or regular file")
			}
			metadataFS := osfs.New(metadataPath)
			metadataRoot, rootErr := os.OpenRoot(metadataPath)
			if rootErr != nil {
				return nil, "", "", rootErr
			}
			commonInfo, commonErr := metadataRoot.Lstat("commondir")
			if errors.Is(commonErr, fs.ErrNotExist) {
				metadataRoot.Close()
				return metadataFS, root, metadataPath, nil
			}
			if commonErr != nil {
				metadataRoot.Close()
				return nil, "", "", commonErr
			}
			if !commonInfo.Mode().IsRegular() || commonInfo.Size() > 4096 {
				metadataRoot.Close()
				return nil, "", "", errors.New("Git commondir must be a regular file no larger than 4096 bytes")
			}
			data, tooLarge, readErr := readBounded(metadataRoot, "commondir", 4096)
			metadataRoot.Close()
			if readErr != nil {
				return nil, "", "", readErr
			}
			if tooLarge {
				return nil, "", "", errors.New("Git commondir exceeds 4096 bytes")
			}
			common := strings.TrimSpace(string(data))
			if common == "" {
				return nil, "", "", errors.New("Git commondir is empty")
			}
			if !filepath.IsAbs(common) {
				common = filepath.Join(metadataPath, common)
			}
			commonFS := osfs.New(filepath.Clean(common))
			return dotgit.NewRepositoryFilesystem(metadataFS, commonFS), root, metadataPath, nil
		}
		checkout.Close()
		if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
			return nil, "", "", statErr
		}
		if !discover {
			if isBareGitDirectory(root) {
				return osfs.New(root), root, root, nil
			}
			return nil, "", "", errGitRepositoryNotFound
		}
		parent := filepath.Dir(root)
		if parent == root {
			return nil, "", "", errGitRepositoryNotFound
		}
		root = parent
	}
}

func isBareGitDirectory(directory string) bool {
	head, headErr := os.Lstat(filepath.Join(directory, "HEAD"))
	objects, objectsErr := os.Lstat(filepath.Join(directory, "objects"))
	return headErr == nil && head.Mode().IsRegular() && objectsErr == nil && objects.IsDir()
}

func resolveLocalRevision(storage *filesystem.Storage, value string) (plumbing.Hash, error) {
	base, operations, err := parseRevisionOperations(value)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	hash, err := resolveRevisionBase(storage, base)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	commit, err := peelCommit(storage, hash)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	for _, operation := range operations {
		switch operation.kind {
		case '^':
			if operation.depth == 0 {
				continue
			}
			commit, err = commit.Parent(operation.depth - 1)
		case '~':
			for range operation.depth {
				commit, err = commit.Parent(0)
				if err != nil {
					break
				}
			}
		}
		if err != nil {
			return plumbing.ZeroHash, err
		}
	}
	return commit.Hash, nil
}

type revisionOperation struct {
	kind  byte
	depth int
}

func parseRevisionOperations(value string) (string, []revisionOperation, error) {
	if value == "" {
		return "", nil, plumbing.ErrReferenceNotFound
	}
	first := strings.IndexAny(value, "^~")
	if first < 0 {
		return value, nil, nil
	}
	base := value[:first]
	if base == "" {
		return "", nil, fmt.Errorf("invalid Git revision %q", value)
	}
	var operations []revisionOperation
	for i := first; i < len(value); {
		kind := value[i]
		i++
		start := i
		for i < len(value) && value[i] >= '0' && value[i] <= '9' {
			i++
		}
		depth := 1
		if start != i {
			parsed, err := strconv.ParseUint(value[start:i], 10, 31)
			if err != nil {
				return "", nil, fmt.Errorf("invalid Git revision %q: %w", value, err)
			}
			depth = int(parsed)
		}
		if i < len(value) && value[i] != '^' && value[i] != '~' {
			return "", nil, fmt.Errorf("invalid Git revision %q", value)
		}
		operations = append(operations, revisionOperation{kind: kind, depth: depth})
	}
	return base, operations, nil
}

func resolveRevisionBase(storage *filesystem.Storage, value string) (plumbing.Hash, error) {
	if isHexPrefix(value) {
		if len(value) == 40 {
			return plumbing.NewHash(value), nil
		}
		iter, err := storage.IterEncodedObjects(plumbing.AnyObject)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		defer iter.Close()
		var match plumbing.Hash
		for {
			encoded, err := iter.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return plumbing.ZeroHash, err
			}
			if strings.HasPrefix(encoded.Hash().String(), strings.ToLower(value)) {
				if match != plumbing.ZeroHash && match != encoded.Hash() {
					return plumbing.ZeroHash, fmt.Errorf("ambiguous abbreviated object ID %q", value)
				}
				match = encoded.Hash()
			}
		}
		if match != plumbing.ZeroHash {
			return match, nil
		}
	}
	var candidates []plumbing.ReferenceName
	if strings.HasPrefix(value, "refs/") || value == "HEAD" || strings.HasPrefix(value, "MERGE_HEAD") {
		candidates = append(candidates, plumbing.ReferenceName(value))
	}
	if !strings.HasPrefix(value, "refs/") && value != "HEAD" {
		candidates = append(candidates,
			plumbing.NewBranchReferenceName(value),
			plumbing.NewTagReferenceName(value),
			plumbing.ReferenceName("refs/remotes/"+value),
		)
	}
	for _, candidate := range candidates {
		ref, err := storer.ResolveReference(storage, candidate)
		if err == nil {
			return ref.Hash(), nil
		}
		if !errors.Is(err, plumbing.ErrReferenceNotFound) {
			return plumbing.ZeroHash, err
		}
	}
	return plumbing.ZeroHash, plumbing.ErrReferenceNotFound
}

func isHexPrefix(value string) bool {
	if len(value) < 4 || len(value) > 40 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func peelCommit(storage storer.EncodedObjectStorer, hash plumbing.Hash) (*object.Commit, error) {
	for range 1024 {
		commit, err := object.GetCommit(storage, hash)
		if err == nil {
			return commit, nil
		}
		tag, tagErr := object.GetTag(storage, hash)
		if tagErr != nil {
			return nil, plumbing.ErrObjectNotFound
		}
		hash = tag.Target
	}
	return nil, errors.New("Git tag chain exceeds 1024 objects")
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

func isUnbornRepository(storage *filesystem.Storage) bool {
	head, err := storage.Reference(plumbing.HEAD)
	if err != nil || head.Type() != plumbing.SymbolicReference || !head.Target().IsBranch() {
		return false
	}
	refs, err := storage.IterReferences()
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
	walker := newGitTreeIterator(s.storage, s.tree)
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
		// EncodedObjectSize mutates the primary lane's pack scanner and index
		// state. walk() finishes every size lookup and .gitattributes read in
		// this first loop before the second loop dispatches any jobs. Workers
		// therefore cannot lease that lane until this prepass is complete.
		// Preserve that ordering if traversal and dispatch are ever interleaved.
		size, err := s.storage.EncodedObjectSize(entry.Hash)
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
			return s.lanes.read(ctx, entry.Hash, filename, limit)
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
