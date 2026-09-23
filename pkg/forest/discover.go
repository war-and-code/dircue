package forest

import (
	"bufio"
	"bytes"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing"
	gogitcache "github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	gogitfs "github.com/go-git/go-git/v5/storage/filesystem"

	"dircue/pkg/treehash"
)

// RootKind describes the kind of a discovered root.
type RootKind string

const (
	RootGitWorktree  RootKind = "git_worktree"
	RootGitBare      RootKind = "git_bare"
	RootGitSubmodule RootKind = "git_submodule"
)

// IdentityStatus records how well a root's identity was resolved.
type IdentityStatus string

const (
	IdentityResolved IdentityStatus = "resolved"
	IdentityUnknown  IdentityStatus = "unknown"
)

// Remote holds one stripped Git remote.
type Remote struct {
	Name     string  `json:"name"`
	Kind     string  `json:"kind,omitempty"`     // "local_path" when the URL is a local filesystem path
	URL      *string `json:"url"`                // nil when redacted (local path outside forest root)
	Redacted bool    `json:"redacted,omitempty"` // true when URL is a local path outside the forest root
}

// IsLocalRemoteURL reports whether u is a local filesystem path or a file:// URL.
func IsLocalRemoteURL(u string) bool {
	if strings.HasPrefix(u, "/") {
		return true
	}
	if strings.HasPrefix(u, "./") || strings.HasPrefix(u, "../") {
		return true
	}
	if strings.HasPrefix(u, "file://") {
		return true
	}
	// Windows absolute: C:/ or C:\ or UNC \\host\share
	if len(u) >= 3 && u[1] == ':' && (u[2] == '/' || u[2] == '\\') {
		return true
	}
	if strings.HasPrefix(u, "\\\\") {
		return true
	}
	return false
}

// resolveLocalRemotePath converts a local remote URL to an absolute filesystem path.
// baseDir is the working directory of the git root (not the .git dir).
func resolveLocalRemotePath(u, baseDir string) string {
	if strings.HasPrefix(u, "file://") {
		// file:///path → /path  OR  file://localhost/path → /path
		rest := strings.TrimPrefix(u, "file://")
		if strings.HasPrefix(rest, "/") {
			return filepath.FromSlash(rest)
		}
		if idx := strings.IndexByte(rest, '/'); idx >= 0 {
			return filepath.FromSlash(rest[idx:])
		}
		return filepath.FromSlash(rest)
	}
	if filepath.IsAbs(u) {
		return filepath.FromSlash(u)
	}
	// relative path: resolve relative to the working directory
	return filepath.Clean(filepath.Join(baseDir, filepath.FromSlash(u)))
}

// ClassifyRemote inspects rem's URL for a local filesystem path and, if found,
// either converts it to a root-relative path (inside forestRootAbs) or redacts it.
// rootWorkdirAbs is the absolute path of the git working tree for this root.
func ClassifyRemote(rem Remote, forestRootAbs, rootWorkdirAbs string) Remote {
	if rem.URL == nil || !IsLocalRemoteURL(*rem.URL) {
		return rem
	}
	absPath := resolveLocalRemotePath(*rem.URL, rootWorkdirAbs)
	// Ensure forestRootAbs ends with separator for prefix check.
	rootPrefix := forestRootAbs
	if !strings.HasSuffix(rootPrefix, string(filepath.Separator)) {
		rootPrefix += string(filepath.Separator)
	}
	rem.Kind = "local_path"
	if strings.HasPrefix(absPath, rootPrefix) || absPath == forestRootAbs {
		rel, err := filepath.Rel(forestRootAbs, absPath)
		if err == nil {
			relSlash := filepath.ToSlash(rel)
			rem.URL = &relSlash
		} else {
			rem.URL = nil
			rem.Redacted = true
		}
	} else {
		rem.URL = nil
		rem.Redacted = true
	}
	return rem
}

// WorkingTreeCounts holds per-root filesystem entry counts for a working tree.
type WorkingTreeCounts struct {
	Regular  int64 // regular files (excluding .git and env tree subtrees)
	Symlinks int64 // symbolic links
	Special  int64 // FIFOs, sockets, devices
}

// CountWorkingTreeFiles walks dirPath inside root and counts filesystem entries by type.
// excludedForestPaths is a set of paths (relative to the forest root, using forward slashes)
// that should be skipped entirely (e.g., env tree directories inside this root).
func CountWorkingTreeFiles(root *os.Root, dirPath string, excludedForestPaths map[string]bool) WorkingTreeCounts {
	var counts WorkingTreeCounts
	dirFS := root.FS()
	_ = fs.WalkDir(dirFS, dirPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if p == dirPath {
			return nil
		}
		name := path.Base(p)
		if d.IsDir() {
			if name == ".git" {
				return fs.SkipDir
			}
			if excludedForestPaths[p] {
				return fs.SkipDir
			}
			return nil
		}
		typ := d.Type()
		switch {
		case typ&fs.ModeSymlink != 0:
			counts.Symlinks++
		case typ&(fs.ModeNamedPipe|fs.ModeSocket|fs.ModeDevice|fs.ModeCharDevice) != 0:
			counts.Special++
		case typ.IsRegular():
			counts.Regular++
		}
		return nil
	})
	return counts
}

// RootIdentity holds the resolved identity of a discovered Git root.
type RootIdentity struct {
	// Path is the root-relative path of this root (empty string = input root).
	Path string
	// Kind is the root type.
	Kind RootKind
	// HEAD is the branch name (e.g. "refs/heads/main") or "detached".
	HEAD string
	// Commit is the resolved HEAD commit object ID (hex).
	Commit string
	// Tree is the resolved HEAD tree object ID (hex).
	Tree string
	// CommitterTime is the committer timestamp of HEAD, as Unix seconds.
	CommitterTime int64
	// Remotes are the credential-stripped remote URLs.
	Remotes []Remote
	// SubmoduleOf is set when kind is git_submodule; it holds the parent path.
	SubmoduleOf string
	// IdentityStatus is "resolved" or "unknown".
	IdentityStatus IdentityStatus
	// IdentityReason explains an unknown identity.
	IdentityReason string
}

// DiscoverOptions controls root discovery.
type DiscoverOptions struct {
	// RootCap is the maximum number of roots to return. Default 1000.
	RootCap int
	// IncludeRemotes controls whether remotes are read from .git/config.
	IncludeRemotes bool
	// SummarizeTrees controls whether env trees are recognized and summarized.
	SummarizeTrees bool
	// EnvTreeCap is the max entries per env tree walk. Default 1_000_000.
	EnvTreeCap int64
}

// DiscoverResult holds the output of a forest discovery walk.
type DiscoverResult struct {
	// Roots are the discovered Git roots, sorted by path.
	Roots []RootIdentity
	// EnvTrees are the summarized environment trees.
	EnvTrees []EnvTreeSummary
	// Partial is true when the root cap was reached.
	Partial bool
	// PartialReason explains why Partial is true.
	PartialReason string
}

const defaultRootCap = 1000

// DiscoverRoots walks the input directory (given by an *os.Root) and finds
// all nested Git roots. It is metadata-only and never executes anything.
func DiscoverRoots(root *os.Root, opts DiscoverOptions) DiscoverResult {
	if opts.RootCap <= 0 {
		opts.RootCap = defaultRootCap
	}

	var result DiscoverResult
	rootSet := map[string]bool{}

	// Read submodule declarations from the root's .gitmodules if present.
	submodulePaths := readSubmodulePaths(root, ".")

	// Perform a metadata-only walk.
	dirFS := root.FS()
	_ = fs.WalkDir(dirFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if p == "." {
			return nil
		}

		name := path.Base(p)

		// Never descend into .git directories.
		if name == ".git" {
			return fs.SkipDir
		}

		// Never descend into already-discovered root subtrees.
		if rootSet[p] {
			return fs.SkipDir
		}

		// Check for env trees and skip them.
		if opts.SummarizeTrees {
			if match := CheckEnvTree(root, p); match != nil {
				summary := SummarizeEnvTree(root, *match, opts.EnvTreeCap)
				result.EnvTrees = append(result.EnvTrees, summary)
				return fs.SkipDir
			}
		}

		// Check for a Git working tree (.git directory or .git file).
		if gitDir, ok := gitDirForDiscover(root, p); ok {
			if len(result.Roots) >= opts.RootCap {
				result.Partial = true
				result.PartialReason = "root_cap_reached"
				return fs.SkipAll
			}
			kind := RootGitWorktree
			submoduleOf := ""
			if _, isSubmodule := submodulePaths[p]; isSubmodule {
				kind = RootGitSubmodule
				submoduleOf = parentRootPath(p, rootSet)
			}
			ident := resolveRootIdentity(root, p, gitDir, kind, submoduleOf, opts)
			result.Roots = append(result.Roots, ident)
			rootSet[p] = true
			// Still descend to find nested roots within this working tree.
			return nil
		}

		// Check for a bare repository.
		if isBareGitDir(root, p) {
			if len(result.Roots) >= opts.RootCap {
				result.Partial = true
				result.PartialReason = "root_cap_reached"
				return fs.SkipAll
			}
			ident := resolveRootIdentity(root, p, p, RootGitBare, "", opts)
			result.Roots = append(result.Roots, ident)
			rootSet[p] = true
			return fs.SkipDir // don't descend into bare repo internals
		}

		return nil
	})

	// Sort roots by path for determinism.
	sort.Slice(result.Roots, func(i, j int) bool {
		return result.Roots[i].Path < result.Roots[j].Path
	})

	return result
}

// gitDirForDiscover returns the root-relative git directory for a working
// tree at dir, following a .git file pointer if needed. It only uses
// metadata reads confined to root.
func gitDirForDiscover(root *os.Root, dir string) (string, bool) {
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
	// .git file: read the gitdir pointer.
	data, err := readSmallFile(root, dotgit, 64<<10)
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
	// Confirm the gitdir target exists inside root.
	if _, err := root.Lstat(resolved); err != nil {
		return "", false
	}
	return resolved, true
}

// isBareGitDir returns true when dir looks like a bare Git repository.
func isBareGitDir(root *os.Root, dir string) bool {
	head, headErr := root.Lstat(path.Join(dir, "HEAD"))
	if headErr != nil || !head.Mode().IsRegular() {
		return false
	}
	objects, objErr := root.Lstat(path.Join(dir, "objects"))
	if objErr != nil || !objects.IsDir() {
		return false
	}
	refs, refsErr := root.Lstat(path.Join(dir, "refs"))
	return refsErr == nil && refs.IsDir()
}

// resolveRootIdentity resolves the identity of a discovered root.
func resolveRootIdentity(root *os.Root, dirPath, gitDir string, kind RootKind, submoduleOf string, opts DiscoverOptions) RootIdentity {
	ident := RootIdentity{
		Path:        dirPath,
		Kind:        kind,
		SubmoduleOf: submoduleOf,
		Remotes:     []Remote{},
	}

	// Always read remotes when requested, regardless of HEAD resolution.
	if opts.IncludeRemotes {
		ident.Remotes = readRemotes(root, gitDir)
	}

	// Read HEAD to determine branch or detached state.
	headData, err := readSmallFile(root, path.Join(gitDir, "HEAD"), 64<<10)
	if err != nil {
		ident.IdentityStatus = IdentityUnknown
		ident.IdentityReason = "head_unreadable"
		return ident
	}
	headStr := strings.TrimSpace(string(headData))
	if ref, ok := strings.CutPrefix(headStr, "ref:"); ok {
		ident.HEAD = strings.TrimSpace(ref)
	} else if headStr == "" {
		ident.IdentityStatus = IdentityUnknown
		ident.IdentityReason = "head_empty"
		return ident
	} else {
		// Bare hash = detached HEAD.
		ident.HEAD = "detached"
	}

	// Resolve the commit via metadata reads only (no git execution).
	format := treehash.FormatSHA1
	commitHash, err := resolveHeadFromGitDir(root, gitDir, format)
	if err != nil {
		ident.IdentityStatus = IdentityUnknown
		ident.IdentityReason = "head_unresolvable"
		return ident
	}
	ident.Commit = commitHash

	// Read the commit object to get tree hash and committer time.
	// Use the common dir (follows worktree commondir pointer) as the storage root.
	commonRelDir := commonDir(root, gitDir)
	absCommonDir := filepath.Join(root.Name(), filepath.FromSlash(commonRelDir))
	treeHash, committerTime, detailErr := readCommitDetails(absCommonDir, commitHash)
	if detailErr == nil {
		ident.Tree = treeHash
		ident.CommitterTime = committerTime
	}
	// A missing tree/committer time does not invalidate the identity; the
	// commit hash itself is still resolved.
	ident.IdentityStatus = IdentityResolved
	return ident
}

// readCommitDetails opens a minimal go-git storage at absGitDir and reads the
// commit object to extract the tree hash and committer Unix timestamp.
// It never executes git and uses no network access.
func readCommitDetails(absGitDir string, commitHex string) (treeHex string, unixTime int64, err error) {
	// Resolve symlinks so go-git's pack-file index can locate objects.
	// On macOS, /var/folders is a symlink to /private/var/folders and
	// some osfs implementations do not follow symlinks for directory opens.
	realDir, symlinkErr := filepath.EvalSymlinks(absGitDir)
	if symlinkErr == nil {
		absGitDir = realDir
	}
	repoFS := osfs.New(absGitDir)
	storage := gogitfs.NewStorageWithOptions(repoFS, gogitcache.NewObjectLRUDefault(), gogitfs.Options{})
	defer storage.Close()
	hash := plumbing.NewHash(commitHex)
	commit, err := object.GetCommit(storage, hash)
	if err != nil {
		return "", 0, err
	}
	return commit.TreeHash.String(), commit.Committer.When.Unix(), nil
}

// readSmallFile reads a file from root, bounded to maxBytes.
func readSmallFile(root *os.Root, name string, maxBytes int64) ([]byte, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fs.ErrInvalid
	}
	if info.Size() > maxBytes {
		return nil, fs.ErrInvalid
	}
	return io.ReadAll(io.LimitReader(f, maxBytes+1))
}

// resolveHeadFromGitDir resolves the HEAD commit hash from a gitDir.
// It reads HEAD, loose refs, and packed-refs as data. Never runs git.
func resolveHeadFromGitDir(root *os.Root, gitDir string, format treehash.Format) (string, error) {
	headData, err := readSmallFile(root, path.Join(gitDir, "HEAD"), 64<<10)
	if err != nil {
		return "", err
	}
	head := strings.TrimSpace(string(headData))
	common := commonDir(root, gitDir)

	for depth := 0; depth < 8; depth++ {
		ref, symbolic := strings.CutPrefix(head, "ref:")
		if !symbolic {
			if isHexHash(head, format) {
				return strings.ToLower(head), nil
			}
			return "", fs.ErrInvalid
		}
		ref = strings.TrimSpace(ref)
		if !strings.HasPrefix(ref, "refs/") || strings.Contains(ref, "..") {
			return "", fs.ErrInvalid
		}
		next, found := "", false
		for _, dir := range []string{gitDir, common} {
			if loose, err := readSmallFile(root, path.Join(dir, ref), 64<<10); err == nil {
				next, found = strings.TrimSpace(string(loose)), true
				break
			}
		}
		if !found {
			packed, err := readSmallFile(root, path.Join(common, "packed-refs"), 256<<20)
			if err != nil {
				return "", fs.ErrInvalid
			}
			for _, line := range strings.Split(string(packed), "\n") {
				id, name, ok := strings.Cut(strings.TrimSpace(line), " ")
				if ok && name == ref {
					next, found = id, true
					break
				}
			}
			if !found {
				return "", fs.ErrInvalid
			}
		}
		head = next
	}
	return "", fs.ErrInvalid
}

// commonDir returns the common directory for a gitDir (follows "commondir" file).
func commonDir(root *os.Root, gitDir string) string {
	data, err := readSmallFile(root, path.Join(gitDir, "commondir"), 64<<10)
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

// isHexHash returns true when v is a valid hex object ID for the given format.
func isHexHash(v string, format treehash.Format) bool {
	expectedLen := 40 // SHA-1
	if format == treehash.FormatSHA256 {
		expectedLen = 64
	}
	if len(v) != expectedLen {
		return false
	}
	for _, c := range v {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// readRemotes parses the [remote] sections from .git/config and returns
// credential-stripped remote entries.
func readRemotes(root *os.Root, gitDir string) []Remote {
	common := commonDir(root, gitDir)
	configData, err := readSmallFile(root, path.Join(common, "config"), 64<<10)
	if err != nil {
		return []Remote{}
	}
	return parseGitConfigRemotes(configData)
}

// parseGitConfigRemotes parses remote sections from git config data.
// It reads only url = lines inside [remote "name"] sections.
func parseGitConfigRemotes(data []byte) []Remote {
	var remotes []Remote
	seen := map[string]bool{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	currentRemote := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[remote ") && strings.HasSuffix(line, "]") {
			// Parse [remote "name"]
			inner := line[len(`[remote `):]
			inner = strings.TrimSuffix(inner, "]")
			inner = strings.Trim(inner, `"`)
			currentRemote = inner
		} else if strings.HasPrefix(line, "[") {
			currentRemote = ""
		} else if currentRemote != "" {
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key == "url" && !seen[currentRemote] {
				seen[currentRemote] = true
				stripped := StripCredentials(value)
				remotes = append(remotes, Remote{Name: currentRemote, URL: &stripped})
			}
		}
	}
	// Sort for determinism.
	sort.Slice(remotes, func(i, j int) bool {
		return remotes[i].Name < remotes[j].Name
	})
	return remotes
}

// readSubmodulePaths reads submodule paths from .gitmodules in the given
// working tree directory (as a root-relative path). Returns a set of
// root-relative paths.
func readSubmodulePaths(root *os.Root, workDir string) map[string]bool {
	gitmodulesPath := path.Join(workDir, ".gitmodules")
	if workDir == "." {
		gitmodulesPath = ".gitmodules"
	}
	info, err := root.Lstat(gitmodulesPath)
	if err != nil {
		return nil
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	data, err := readSmallFile(root, gitmodulesPath, 1<<20)
	if err != nil {
		return nil
	}
	paths := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) == "path" {
			p := strings.TrimSpace(value)
			if p != "" {
				var full string
				if workDir == "." || workDir == "" {
					full = p
				} else {
					full = path.Join(workDir, p)
				}
				paths[full] = true
			}
		}
	}
	return paths
}

// parentRootPath returns the path of the most-specific ancestor root that
// contains p. Returns "" when none is found.
func parentRootPath(p string, rootSet map[string]bool) string {
	dir := path.Dir(p)
	for dir != "." && dir != "" {
		if rootSet[dir] {
			return dir
		}
		dir = path.Dir(dir)
	}
	return ""
}
