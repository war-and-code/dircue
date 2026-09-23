// Package treehash computes a Git-compatible tree object ID for a plain
// directory without running Git or reading outside the selected root.
//
// With ScopeGit, a complete result equals the tree that `git add -A && git
// write-tree` records for the same directory in a fresh repository. Its stated
// assumptions: core.autocrlf=false, core.filemode=true, core.symlinks=true,
// core.ignorecase=false, core.precomposeunicode=false, and no user-level
// excludes or attributes. Repository content (.gitignore, .gitattributes,
// $GIT_DIR/info/exclude and info/attributes, and the root index when it is
// readable) is honored. Anything Git would transform in a way this package
// cannot reproduce is reported as a stable reason instead of being guessed.
package treehash

import (
	"context"
	"crypto/sha1" //nolint:gosec // Git object IDs are defined over SHA-1.
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Format selects the Git object format.
type Format string

const (
	FormatSHA1   Format = "sha1"
	FormatSHA256 Format = "sha256"
)

func (f Format) hexLen() int {
	if f == FormatSHA256 {
		return 64
	}
	return 40
}

func (f Format) newHash() hash.Hash {
	if f == FormatSHA256 {
		return sha256.New()
	}
	return sha1.New() //nolint:gosec // Git object IDs are defined over SHA-1.
}

// Algorithm is the map digest algorithm name for this format.
func (f Format) Algorithm() string { return "git-" + string(f) }

// Scope names the entries and conversions a digest covers.
type Scope string

const (
	// ScopeGit applies Git's ignore rules and clean-direction line-ending
	// normalization, and records nested repositories as gitlinks.
	ScopeGit Scope = "gitignore_filtered+git_normalized"
	// ScopeRaw covers every regular file and symlink except ".git" entries,
	// byte for byte, with nested repositories hashed as ordinary content.
	ScopeRaw Scope = "all_entries+raw"
)

// Status qualifies a result.
type Status string

const (
	// StatusComplete: the tree ID is exactly what Git records for the scope.
	StatusComplete Status = "complete"
	// StatusPartial: the tree ID is a deterministic identity of the scope, but
	// Git could record something different for the reasons given.
	StatusPartial Status = "partial"
	// StatusUnavailable: no tree ID; the scope could not be read completely.
	StatusUnavailable Status = "unavailable"
)

// Stable reason codes.
const (
	ReasonFilterDriver          = "filter_driver_not_applied"
	ReasonIdent                 = "ident_not_applied"
	ReasonWorkingTreeEncoding   = "working_tree_encoding_not_applied"
	ReasonSpecialFiles          = "special_files_excluded"
	ReasonNestedUnresolved      = "nested_repository_unresolved"
	ReasonTextAutoIndexAssumed  = "text_auto_index_state_assumed_empty"
	ReasonIndexNotConsulted     = "repository_index_not_consulted"
	ReasonSparseEntries         = "sparse_checkout_entries_not_present"
	ReasonUnreadableEntry       = "unreadable_entry"
	ReasonEntryLimit            = "digest_entry_limit"
	ReasonByteLimit             = "digest_byte_limit"
	ReasonContentChanged        = "content_changed_during_read"
	ReasonUnsupportedName       = "unsupported_entry_name"
	ReasonSymlinkTargetTooLarge = "symlink_target_too_large"
)

// Options controls a computation. Zero values select SHA-1, ScopeGit, no
// entry or byte limits, and automatic worker selection.
type Options struct {
	Format     Format
	Scope      Scope
	MaxEntries int
	MaxBytes   int64
	Workers    int
	// KeepBlobs retains root-relative path to blob ID pairs for regular files
	// and symlinks, for consumers that key per-file facts by content.
	KeepBlobs bool
}

// Result describes the computed identity.
type Result struct {
	Algorithm    string
	Scope        Scope
	TreeID       string
	Status       Status
	Reasons      []string
	Files        int
	Symlinks     int
	Gitlinks     int
	Directories  int
	Ignored      int
	SpecialFiles int
	BytesRead    int64
	Blobs        map[string]string
}

type child struct {
	name string
	mode string
	dir  *dirNode
	id   []byte
}

type dirNode struct {
	children []*child
}

type fileJob struct {
	path    string
	size    int64
	action  crlfAction
	target  *child
	symlink bool
}

// stopError ends the walk with an unavailable result.
type stopError struct{ reason string }

func (e stopError) Error() string { return e.reason }

type walker struct {
	root       *os.Root
	opts       Options
	index      *repositoryIndex
	indexKnown bool
	jobs       []fileJob
	reasons    map[string]bool
	result     *Result
	entries    int
	bytes      int64
	anyIgnored bool
}

// Compute returns the tree identity of the directory opened as root. I/O
// problems inside the scope become an unavailable status with a reason; only
// cancellation and invalid options are returned as errors.
func Compute(ctx context.Context, root *os.Root, opts Options) (Result, error) {
	if opts.Format == "" {
		opts.Format = FormatSHA1
	}
	if opts.Scope == "" {
		opts.Scope = ScopeGit
	}
	if opts.Format != FormatSHA1 && opts.Format != FormatSHA256 {
		return Result{}, fmt.Errorf("unsupported object format %q", opts.Format)
	}
	if opts.Scope != ScopeGit && opts.Scope != ScopeRaw {
		return Result{}, fmt.Errorf("unsupported digest scope %q", opts.Scope)
	}
	if opts.Workers <= 0 {
		opts.Workers = min(runtime.GOMAXPROCS(0), 16)
	}
	res := Result{Algorithm: opts.Format.Algorithm(), Scope: opts.Scope}
	w := &walker{root: root, opts: opts, reasons: map[string]bool{}, result: &res}

	var ign ignoreStack
	var infoAttrs *attrFile
	if opts.Scope == ScopeGit {
		if gitDir, ok := gitDirFor(root, "."); ok {
			common := commonDirFor(root, gitDir)
			if data, err := readSmall(root, path.Join(common, "info", "exclude"), maxIndexBytes); err == nil {
				ign.patterns = parseIgnore(data, "")
			}
			if data, err := readSmall(root, path.Join(common, "info", "attributes"), maxIndexBytes); err == nil {
				parsed := parseAttributes(data, "", true)
				infoAttrs = &parsed
			}
			idx, err := readRepositoryIndex(root, gitDir)
			w.index, w.indexKnown = idx, err == nil
		}
	}
	rootNode, err := w.walk(ctx, ".", &ign, nil, false, infoAttrs)
	return w.finish(ctx, rootNode, err)
}

func (w *walker) finish(ctx context.Context, rootNode *dirNode, err error) (Result, error) {
	res := w.result
	var stop stopError
	if err != nil {
		if errors.As(err, &stop) {
			res.Status = StatusUnavailable
			res.Reasons = []string{stop.reason}
			return *res, nil
		}
		return Result{}, err
	}
	if w.index != nil && w.index.skipWorktree > 0 {
		w.reasons[ReasonSparseEntries] = true
	}
	if w.opts.Scope == ScopeGit && !w.indexKnown && w.anyIgnored {
		if _, ok := gitDirFor(w.root, "."); ok {
			w.reasons[ReasonIndexNotConsulted] = true
		}
	}
	if err := w.hashAll(ctx); err != nil {
		if errors.As(err, &stop) {
			res.Status = StatusUnavailable
			res.Reasons = []string{stop.reason}
			return *res, nil
		}
		return Result{}, err
	}
	if rootNode == nil {
		rootNode = &dirNode{}
	}
	res.TreeID = hex.EncodeToString(w.buildTree(rootNode))
	res.Status = StatusComplete
	for r := range w.reasons {
		res.Reasons = append(res.Reasons, r)
	}
	slices.Sort(res.Reasons)
	if len(res.Reasons) > 0 {
		res.Status = StatusPartial
	}
	if res.Reasons == nil {
		res.Reasons = []string{}
	}
	return *res, nil
}

func (w *walker) count(size int64) error {
	w.entries++
	if w.opts.MaxEntries > 0 && w.entries > w.opts.MaxEntries {
		return stopError{ReasonEntryLimit}
	}
	w.bytes += size
	if w.opts.MaxBytes > 0 && w.bytes > w.opts.MaxBytes {
		return stopError{ReasonByteLimit}
	}
	return nil
}

// walk visits dir (root-relative). ignoredCtx is true inside a directory that
// is ignored but still contains tracked paths: only tracked paths are added.
// infoAttrs, when non-nil, is $GIT_DIR/info/attributes (highest priority).
func (w *walker) walk(ctx context.Context, dir string, ign *ignoreStack, attrs []attrFile, ignoredCtx bool, infoAttrs *attrFile) (*dirNode, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(w.root.FS(), dir)
	if err != nil {
		return nil, stopError{ReasonUnreadableEntry}
	}
	git := w.opts.Scope == ScopeGit
	base := dir
	if base == "." {
		base = ""
	}
	if git {
		if data, err := readSmall(w.root, path.Join(dir, ".gitignore"), maxIndexBytes); err == nil {
			ign = ign.with(parseIgnore(data, base))
		}
		if data, err := readSmall(w.root, path.Join(dir, ".gitattributes"), maxIndexBytes); err == nil {
			attrs = append(slices.Clip(attrs), parseAttributes(data, base, dir == "."))
		}
	}
	var stack *attrStack
	if git {
		files := attrs
		if infoAttrs != nil {
			files = append(slices.Clip(files), *infoAttrs)
		}
		stack = newAttrStack(files)
	}
	node := &dirNode{}
	w.result.Directories++
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := e.Name()
		if name == ".git" {
			continue
		}
		if name == "" || strings.ContainsAny(name, "/\x00") {
			w.reasons[ReasonUnsupportedName] = true
			continue
		}
		rel := name
		if dir != "." {
			rel = dir + "/" + name
		}
		info, err := w.root.Lstat(rel)
		if err != nil {
			return nil, stopError{ReasonUnreadableEntry}
		}
		mode := info.Mode()
		tracked := w.index != nil && w.index.tracked[rel]
		switch {
		case mode.IsDir():
			trackedDir := w.index != nil && w.index.trackedDirs[rel]
			if git {
				if id, ok := w.index.gitlink(rel); ok && !hasDotGit(w.root, rel) {
					// An uninitialized submodule keeps its recorded commit.
					raw, _ := hex.DecodeString(id)
					node.children = append(node.children, &child{name: name, mode: "160000", id: raw})
					w.result.Gitlinks++
					if err := w.count(0); err != nil {
						return nil, err
					}
					continue
				}
				excluded := ignoredCtx || ign.excluded(rel, true)
				if excluded && !trackedDir && !tracked {
					w.result.Ignored++
					w.anyIgnored = true
					continue
				}
				if hasDotGit(w.root, rel) {
					commit, ok := w.nestedCommit(rel)
					if !ok {
						w.reasons[ReasonNestedUnresolved] = true
						continue
					}
					raw, _ := hex.DecodeString(commit)
					node.children = append(node.children, &child{name: name, mode: "160000", id: raw})
					w.result.Gitlinks++
					if err := w.count(0); err != nil {
						return nil, err
					}
					continue
				}
				childNode, err := w.walk(ctx, rel, ign, attrs, excluded, infoAttrs)
				if err != nil {
					return nil, err
				}
				if len(childNode.children) > 0 {
					node.children = append(node.children, &child{name: name, mode: "40000", dir: childNode})
				}
				continue
			}
			childNode, err := w.walk(ctx, rel, ign, attrs, false, nil)
			if err != nil {
				return nil, err
			}
			if len(childNode.children) > 0 {
				node.children = append(node.children, &child{name: name, mode: "40000", dir: childNode})
			}
		case mode&fs.ModeSymlink != 0, mode.IsRegular():
			if git && !tracked && (ignoredCtx || ign.excluded(rel, false)) {
				w.result.Ignored++
				w.anyIgnored = true
				continue
			}
			c := &child{name: name}
			job := fileJob{path: rel, size: info.Size(), target: c}
			if mode&fs.ModeSymlink != 0 {
				c.mode = "120000"
				job.symlink = true
				w.result.Symlinks++
			} else {
				c.mode = "100644"
				if mode.Perm()&0o100 != 0 {
					c.mode = "100755"
				}
				w.result.Files++
				if git {
					job.action = w.fileAction(stack, rel)
				}
			}
			if err := w.count(info.Size()); err != nil {
				return nil, err
			}
			node.children = append(node.children, c)
			w.jobs = append(w.jobs, job)
		default:
			if git && !tracked && (ignoredCtx || ign.excluded(rel, false)) {
				w.result.Ignored++
				w.anyIgnored = true
				continue
			}
			w.result.SpecialFiles++
			w.reasons[ReasonSpecialFiles] = true
		}
	}
	return node, nil
}

func (idx *repositoryIndex) gitlink(rel string) (string, bool) {
	if idx == nil {
		return "", false
	}
	id, ok := idx.gitlinks[rel]
	return id, ok
}

func hasDotGit(root *os.Root, dir string) bool {
	_, err := root.Lstat(path.Join(dir, ".git"))
	return err == nil
}

func (w *walker) nestedCommit(dir string) (string, bool) {
	gitDir, ok := gitDirFor(w.root, dir)
	if !ok {
		return "", false
	}
	commit, err := resolveHEAD(w.root, gitDir, w.opts.Format)
	return commit, err == nil
}

// fileAction resolves the clean-direction conversion for a regular file and
// records attributes whose conversions cannot be reproduced without running
// external programs or consulting user configuration.
func (w *walker) fileAction(stack *attrStack, rel string) crlfAction {
	attrs := stack.resolve(rel)
	if f := attrs["filter"]; f.decided && f.value != "" {
		w.reasons[ReasonFilterDriver] = true
	}
	if a := attrs["ident"]; a.decided && a.set {
		w.reasons[ReasonIdent] = true
	}
	if a := attrs["working-tree-encoding"]; a.decided && a.value != "" {
		w.reasons[ReasonWorkingTreeEncoding] = true
	}
	return cleanAction(attrs)
}

func (w *walker) hashAll(ctx context.Context) error {
	if w.opts.KeepBlobs {
		w.result.Blobs = make(map[string]string, len(w.jobs))
	}
	var (
		mu       sync.Mutex
		firstErr error
		next     int
		wg       sync.WaitGroup
		read     int64
		autoConv bool
	)
	workers := min(w.opts.Workers, max(1, len(w.jobs)))
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 256<<10)
			var out []byte
			for {
				mu.Lock()
				if firstErr != nil || next >= len(w.jobs) {
					mu.Unlock()
					return
				}
				job := w.jobs[next]
				next++
				mu.Unlock()
				if err := ctx.Err(); err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					return
				}
				id, n, converted, err := w.hashOne(job, buf, &out)
				mu.Lock()
				if err != nil && firstErr == nil {
					firstErr = err
				}
				read += n
				if converted && job.action == crlfAuto {
					autoConv = true
				}
				job.target.id = id
				if w.result.Blobs != nil && err == nil {
					w.result.Blobs[job.path] = hex.EncodeToString(id)
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	w.result.BytesRead = read
	if firstErr != nil {
		return firstErr
	}
	if autoConv && w.indexKnown {
		// Git skips text=auto normalization for paths whose indexed blob
		// already contains CR; that blob content is not consulted here.
		w.reasons[ReasonTextAutoIndexAssumed] = true
	}
	return nil
}

func (w *walker) hashOne(job fileJob, buf []byte, out *[]byte) ([]byte, int64, bool, error) {
	if job.symlink {
		target, err := w.root.Readlink(job.path)
		if err != nil {
			return nil, 0, false, stopError{ReasonUnreadableEntry}
		}
		if len(target) > maxMetadataBytes {
			return nil, 0, false, stopError{ReasonSymlinkTargetTooLarge}
		}
		h := w.opts.Format.newHash()
		writeHeader(h, "blob", int64(len(target)))
		io.WriteString(h, target)
		return h.Sum(nil), int64(len(target)), false, nil
	}
	f, err := openRegular(w.root, job.path)
	if err != nil {
		return nil, 0, false, stopError{ReasonUnreadableEntry}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != job.size {
		return nil, 0, false, stopError{ReasonContentChanged}
	}
	raw := w.opts.Format.newHash()
	writeHeader(raw, "blob", job.size)
	var stats statsGatherer
	var total int64
	for {
		n, err := f.Read(buf)
		if n > 0 {
			raw.Write(buf[:n])
			if job.action != crlfBinary {
				stats.write(buf[:n])
			}
			total += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, total, false, stopError{ReasonUnreadableEntry}
		}
	}
	if total != job.size {
		return nil, total, false, stopError{ReasonContentChanged}
	}
	if job.action == crlfBinary {
		return raw.Sum(nil), total, false, nil
	}
	s := stats.finish()
	if s.crlf == 0 || (job.action == crlfAuto && s.isBinary()) {
		return raw.Sum(nil), total, false, nil
	}
	// Second pass: hash the CRLF-normalized content under its new length.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, total, false, stopError{ReasonUnreadableEntry}
	}
	normalized := w.opts.Format.newHash()
	want := job.size - s.crlf
	writeHeader(normalized, "blob", want)
	var strip crlfStripper
	var written, again int64
	for {
		n, err := f.Read(buf)
		if n > 0 {
			again += int64(n)
			*out = strip.apply((*out)[:0], buf[:n])
			normalized.Write(*out)
			written += int64(len(*out))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, total, false, stopError{ReasonUnreadableEntry}
		}
	}
	*out = strip.flush((*out)[:0])
	normalized.Write(*out)
	written += int64(len(*out))
	if again != job.size || written != want {
		return nil, total, false, stopError{ReasonContentChanged}
	}
	return normalized.Sum(nil), total + again, true, nil
}

func writeHeader(h hash.Hash, kind string, size int64) {
	io.WriteString(h, kind)
	h.Write([]byte{' '})
	io.WriteString(h, strconv.FormatInt(size, 10))
	h.Write([]byte{0})
}

// buildTree encodes a tree object bottom-up and returns its raw ID.
func (w *walker) buildTree(n *dirNode) []byte {
	for _, c := range n.children {
		if c.dir != nil {
			c.id = w.buildTree(c.dir)
		}
	}
	slices.SortFunc(n.children, func(a, b *child) int {
		return strings.Compare(sortKey(a), sortKey(b))
	})
	var body []byte
	for _, c := range n.children {
		body = append(body, c.mode...)
		body = append(body, ' ')
		body = append(body, c.name...)
		body = append(body, 0)
		body = append(body, c.id...)
	}
	h := w.opts.Format.newHash()
	writeHeader(h, "tree", int64(len(body)))
	h.Write(body)
	return h.Sum(nil)
}

// sortKey reproduces Git's base_name_compare: trees sort as if their name
// ended in '/'.
func sortKey(c *child) string {
	if c.mode == "40000" {
		return c.name + "/"
	}
	return c.name
}
