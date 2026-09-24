// Package forest discovers nested Git roots and summarizes environment trees
// inside a plain directory. It never executes inspected content, never contacts
// a network service, and never reads more than metadata.
package forest

import (
	"io/fs"
	"os"
	"path"
	"strings"
)

// EnvTreeKind classifies a recognized environment or build-output tree.
type EnvTreeKind string

const (
	KindNodeModules EnvTreeKind = "dependency_tree"
	KindPythonVenv  EnvTreeKind = "dependency_tree"
	KindPycache     EnvTreeKind = "cache"
	KindCachedirTag EnvTreeKind = "cache"
	KindGradle      EnvTreeKind = "build_output"
	KindTerraform   EnvTreeKind = "build_output"
	KindTox         EnvTreeKind = "cache"
	KindNox         EnvTreeKind = "cache"
	KindMypyCache   EnvTreeKind = "cache"
	KindPytestCache EnvTreeKind = "cache"
	KindRuffCache   EnvTreeKind = "cache"
	KindCocoaPods   EnvTreeKind = "dependency_tree"
)

// cachedirTagSignature is the standard first-line marker defined at
// https://bford.info/cachedir/ and used by Rust's cargo target/ among others.
const cachedirTagSignature = "Signature: 8a477f597d28d172789f06886806bc55"

// EnvTreeMatch records a recognized environment or build-output tree.
type EnvTreeMatch struct {
	// Path is the root-relative path of the summarized directory.
	Path string
	// Kind is the content role: dependency_tree, build_output, or cache.
	Kind EnvTreeKind
	// Ecosystem names the technology, e.g. "npm", "python_venv", "rust_cargo".
	Ecosystem string
	// Marker is the root-relative path of the evidence file or directory name.
	Marker string
	// Basis is the evidence kind: "filename_hint" or "rule_inferred".
	Basis string
}

// CheckEnvTree checks whether dirPath (root-relative) inside root is a
// recognized environment or build-output tree. It reads only metadata.
// Returns nil when the directory should be walked normally.
func CheckEnvTree(root *os.Root, dirPath string) *EnvTreeMatch {
	name := path.Base(dirPath)

	switch name {
	case "node_modules":
		return &EnvTreeMatch{
			Path:      dirPath,
			Kind:      KindNodeModules,
			Ecosystem: "npm",
			Marker:    dirPath,
			Basis:     "filename_hint",
		}

	case "__pycache__":
		return &EnvTreeMatch{
			Path:      dirPath,
			Kind:      KindPycache,
			Ecosystem: "python_cache",
			Marker:    dirPath,
			Basis:     "filename_hint",
		}

	case ".terraform":
		return &EnvTreeMatch{
			Path:      dirPath,
			Kind:      KindTerraform,
			Ecosystem: "terraform",
			Marker:    dirPath,
			Basis:     "filename_hint",
		}

	case ".tox":
		return &EnvTreeMatch{
			Path:      dirPath,
			Kind:      KindTox,
			Ecosystem: "tox",
			Marker:    dirPath,
			Basis:     "filename_hint",
		}

	case ".nox":
		return &EnvTreeMatch{
			Path:      dirPath,
			Kind:      KindNox,
			Ecosystem: "nox",
			Marker:    dirPath,
			Basis:     "filename_hint",
		}

	case ".mypy_cache":
		return &EnvTreeMatch{
			Path:      dirPath,
			Kind:      KindMypyCache,
			Ecosystem: "mypy",
			Marker:    dirPath,
			Basis:     "filename_hint",
		}

	case ".pytest_cache":
		return &EnvTreeMatch{
			Path:      dirPath,
			Kind:      KindPytestCache,
			Ecosystem: "pytest",
			Marker:    dirPath,
			Basis:     "filename_hint",
		}

	case ".ruff_cache":
		return &EnvTreeMatch{
			Path:      dirPath,
			Kind:      KindRuffCache,
			Ecosystem: "ruff",
			Marker:    dirPath,
			Basis:     "filename_hint",
		}
	}

	// Python virtualenv: a directory containing pyvenv.cfg.
	if isPythonVenv(root, dirPath) {
		return &EnvTreeMatch{
			Path:      dirPath,
			Kind:      KindPythonVenv,
			Ecosystem: "python_venv",
			Marker:    path.Join(dirPath, "pyvenv.cfg"),
			Basis:     "rule_inferred",
		}
	}

	// CACHEDIR.TAG: standard cache-directory marker (covers Rust target/ etc.).
	if hasCachedirTag(root, dirPath) {
		return &EnvTreeMatch{
			Path:      dirPath,
			Kind:      KindCachedirTag,
			Ecosystem: "cachedir_tagged",
			Marker:    path.Join(dirPath, "CACHEDIR.TAG"),
			Basis:     "rule_inferred",
		}
	}

	// .gradle next to a Gradle build file.
	if name == ".gradle" {
		if hasGradleSibling(root, dirPath) {
			return &EnvTreeMatch{
				Path:      dirPath,
				Kind:      KindGradle,
				Ecosystem: "gradle",
				Marker:    gradleSiblingPath(root, dirPath),
				Basis:     "rule_inferred",
			}
		}
	}

	// build/ only with a sibling Gradle build file.
	if name == "build" {
		if hasGradleSibling(root, dirPath) {
			return &EnvTreeMatch{
				Path:      dirPath,
				Kind:      KindGradle,
				Ecosystem: "gradle_build",
				Marker:    gradleSiblingPath(root, dirPath),
				Basis:     "rule_inferred",
			}
		}
	}

	// CocoaPods Pods/ directory with a sibling Podfile.
	if name == "Pods" {
		podfile := path.Join(path.Dir(dirPath), "Podfile")
		if fileExists(root, podfile) {
			return &EnvTreeMatch{
				Path:      dirPath,
				Kind:      KindCocoaPods,
				Ecosystem: "cocoapods",
				Marker:    podfile,
				Basis:     "rule_inferred",
			}
		}
	}

	return nil
}

// isPythonVenv returns true when dirPath contains pyvenv.cfg.
func isPythonVenv(root *os.Root, dirPath string) bool {
	return fileExists(root, path.Join(dirPath, "pyvenv.cfg"))
}

// hasCachedirTag returns true when dirPath contains CACHEDIR.TAG whose
// first line starts with the standard signature.
func hasCachedirTag(root *os.Root, dirPath string) bool {
	tagPath := path.Join(dirPath, "CACHEDIR.TAG")
	f, err := root.Open(tagPath)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	// Read only the first line (up to 200 bytes).
	buf := make([]byte, 200)
	n, _ := f.Read(buf)
	if n == 0 {
		return false
	}
	line := string(buf[:n])
	if nl := strings.IndexByte(line, '\n'); nl >= 0 {
		line = line[:nl]
	}
	return strings.TrimSpace(line) == cachedirTagSignature
}

// gradleFileNames lists supported Gradle build file names.
var gradleFileNames = []string{
	"build.gradle", "build.gradle.kts",
	"settings.gradle", "settings.gradle.kts",
}

// hasGradleSibling returns true when the parent of dirPath contains a Gradle
// build or settings file.
func hasGradleSibling(root *os.Root, dirPath string) bool {
	parent := path.Dir(dirPath)
	if parent == "." {
		parent = ""
	}
	for _, name := range gradleFileNames {
		var p string
		if parent == "" {
			p = name
		} else {
			p = path.Join(parent, name)
		}
		if fileExists(root, p) {
			return true
		}
	}
	return false
}

// gradleSiblingPath returns the path of the first found Gradle sibling, or the
// directory itself when none is found (should not happen after hasGradleSibling).
func gradleSiblingPath(root *os.Root, dirPath string) string {
	parent := path.Dir(dirPath)
	if parent == "." {
		parent = ""
	}
	for _, name := range gradleFileNames {
		var p string
		if parent == "" {
			p = name
		} else {
			p = path.Join(parent, name)
		}
		if fileExists(root, p) {
			return p
		}
	}
	return dirPath
}

// fileExists returns true when name exists as a regular file inside root.
func fileExists(root *os.Root, name string) bool {
	info, err := root.Lstat(name)
	return err == nil && info.Mode().IsRegular()
}

// EnvTreeSummary holds the result of a bounded metadata-only walk of a
// recognized environment or build-output tree.
type EnvTreeSummary struct {
	EnvTreeMatch
	// Entries is the count of directory entries found.
	Entries int64
	// Bytes is the total size of files found.
	Bytes int64
	// Bounded is true when the walk completed within the entry cap.
	Bounded bool
	// LowerBound is true when the entry cap was reached or walk errors occurred
	// (both entries and bytes are lower bounds).
	LowerBound bool
	// Reason is set when LowerBound is true, explaining why the walk stopped.
	Reason string
	// WalkErrors is the count of directory entries that could not be read.
	// When non-zero, LowerBound is also true and byte counts are a lower bound.
	WalkErrors int
}

const defaultEnvTreeEntryCap = 1_000_000

// SummarizeEnvTree does a bounded metadata-only walk of an environment tree
// and returns a summary. It reads no file content.
func SummarizeEnvTree(root *os.Root, match EnvTreeMatch, cap int64) EnvTreeSummary {
	if cap <= 0 {
		cap = defaultEnvTreeEntryCap
	}
	s := EnvTreeSummary{EnvTreeMatch: match, Bounded: true}
	dirFS := root.FS()
	_ = fs.WalkDir(dirFS, match.Path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			s.WalkErrors++
			s.LowerBound = true
			if s.Reason == "" {
				s.Reason = "walk_errors"
			} else if s.Reason != "walk_errors" && s.Reason != "entry_cap_reached_and_walk_errors" {
				s.Reason = "entry_cap_reached_and_walk_errors"
			}
			return nil
		}
		if p == match.Path {
			return nil // skip the root itself
		}
		s.Entries++
		if s.Entries > cap {
			s.Bounded = false
			s.LowerBound = true
			s.Reason = "entry_cap_reached"
			return fs.SkipAll
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err == nil {
				s.Bytes += info.Size()
			}
		}
		return nil
	})
	return s
}
