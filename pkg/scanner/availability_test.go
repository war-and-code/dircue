package scanner

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/war-and-code/dircue/pkg/profile"
	git "github.com/war-and-code/dircue/third_party/go-git"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing/filemode"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing/object"
)

func addGitlinkCommit(t *testing.T, root string, hashString string) {
	t.Helper()
	repo, err := git.PlainOpen(root)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := repo.Storer.Index()
	if err != nil {
		t.Fatal(err)
	}
	entry := idx.Add("vendor/lib")
	entry.Mode = filemode.Submodule
	entry.Hash = plumbing.NewHash(hashString)
	if err := repo.Storer.SetIndex(idx); err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	_, err = wt.Commit("gitlink", &git.CommitOptions{Author: &object.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Unix(1700000001, 0)}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAvailabilityGitPointersGitlinksAndMissingReference(t *testing.T) {
	pointer := "version https://git-lfs.github.com/spec/v1\noid sha256:" + strings.Repeat("a", 64) + "\nsize 5000\n"
	root, repo, first := gitFixture(t, map[string]string{
		".gitattributes":       "*.bin filter=lfs\n",
		".gitmodules":          "[submodule \"private-name\"]\n\tpath = vendor/lib\n\turl = ssh://private.invalid/repo\n",
		"assets/model.bin":     pointer,
		"assets/apparent.bin":  strings.Repeat("z", 2048),
		"assets/malformed.bin": "version https://git-lfs.github.com/spec/v1\noid nope\nsize bad\n",
		"app/app.csproj":       `<Project><ItemGroup><ProjectReference Include="../vendor/lib/Missing.csproj"/></ItemGroup></Project>`,
	})
	_ = repo
	addGitlinkCommit(t, root, first.String())
	report, err := Scan(context.Background(), root, Options{Availability: true, Projects: true})
	if err != nil {
		t.Fatal(err)
	}
	a := report.Availability
	if a == nil || a.Source.Mode != "git" || a.Source.CheckoutMetadata != "not_inspected_for_git_tree" {
		t.Fatalf("source: %+v", a)
	}
	if a.Counts.ValidPointers != 1 || a.Counts.PointerLikeFiles != 1 || a.Counts.LFSAttributedNonPointerFiles != 1 {
		t.Fatalf("LFS counts: %+v", a.Counts)
	}
	if len(a.Gitlinks) != 1 || !a.Gitlinks[0].Declared || len(a.Submodules) != 1 || !a.Submodules[0].HasGitlink {
		t.Fatalf("submodules: %+v %+v", a.Gitlinks, a.Submodules)
	}
	if len(a.References) != 1 || a.References[0].Qualification != "established_boundary" || a.References[0].BoundaryKind != "gitlink" {
		t.Fatalf("reference correlation: %+v", a.References)
	}
	if a.Counts.SparseIndications != 0 {
		t.Fatalf("Git-tree scan inherited checkout sparsity: %+v", a.Sparse)
	}
	encoded, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private.invalid") || strings.Contains(string(encoded), "private-name") {
		t.Fatal("submodule secret leaked")
	}
}

func TestAvailabilityDirectorySparseMetadataIsSeparateFromGitTree(t *testing.T) {
	root, _, _ := gitFixture(t, map[string]string{"main.go": goSource})
	gitReport, err := Scan(context.Background(), root, Options{Availability: true})
	if err != nil {
		t.Fatal(err)
	}
	if gitReport.Availability.Counts.SparseIndications != 0 {
		t.Fatalf("Git report sparse evidence: %+v", gitReport.Availability.Sparse)
	}
	config := "[core]\n\tsparseCheckout = true\n\tsparseCheckoutCone = true\n[index]\n\tsparse = true\n"
	if err := os.WriteFile(filepath.Join(root, ".git", "config.worktree"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git", "info"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "info", "sparse-checkout"), []byte("/*\n!/*/\n/cold/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	index := sparseIndexFixture(3, "cold/", 0040000, true, "sdir")
	if err := os.WriteFile(filepath.Join(root, ".git", "index"), index, 0600); err != nil {
		t.Fatal(err)
	}
	directory, err := Scan(context.Background(), root, Options{Availability: true, Source: "directory"})
	if err != nil {
		t.Fatal(err)
	}
	a := directory.Availability
	if a.Source.Mode != "directory" || a.Source.CheckoutMetadata != "confined_local_metadata" || a.Counts.SparseIndications < 6 {
		t.Fatalf("directory sparse evidence: source=%+v sparse=%+v", a.Source, a.Sparse)
	}
	foundPath := false
	for _, item := range a.Sparse {
		if item.Kind == "sparse_directory" && item.Path == "cold" {
			foundPath = true
		}
	}
	if !foundPath {
		t.Fatalf("missing sparse directory: %+v", a.Sparse)
	}
}

func TestAvailabilityUnsupportedSparseIndexPreventsAbsenceClaim(t *testing.T) {
	root := fixtures(t, map[string]string{"project.json": "{}"})
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	index := sparseIndexFixture(4, "cold/file", 0100644, true, "")
	if err := os.WriteFile(filepath.Join(root, ".git", "index"), index, 0600); err != nil {
		t.Fatal(err)
	}
	report, err := Scan(context.Background(), root, Options{Availability: true, Source: "directory"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Availability.Status != "partial" || !report.Availability.Coverage.SelectedInventoryComplete || !report.Availability.Coverage.CheckoutMetadataInspected || report.Availability.Coverage.CheckoutMetadataComplete {
		t.Fatalf("unsupported index coverage: %+v", report.Availability)
	}
}

func TestAvailabilityMaxFileBytesOmitsPointerAndGitmodulesReads(t *testing.T) {
	root := fixtures(t, map[string]string{"small.txt": "ok"})
	directory, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()

	a := newAvailabilityAccumulator(Options{Availability: true, MaxFileBytes: 8}, nil)
	reads := 0
	read := func(int64) ([]byte, int64, error) {
		reads++
		return nil, 64, nil
	}
	for _, item := range []job{
		{path: "large.bin", size: 64, attrs: overrides{lfsTracked: true}, read: read},
		{path: ".gitmodules", size: 64, read: read},
	} {
		if err := a.add(result{selectedJob: &item}, directory); err != nil {
			t.Fatal(err)
		}
	}
	report := &profile.Report{}
	if err := a.finish(context.Background(), directory, report); err != nil {
		t.Fatal(err)
	}
	if reads != 0 {
		t.Fatalf("oversized selected-source callbacks read %d times", reads)
	}
	got := report.Availability
	if got.Bounds.MaxFileBytes != 8 || got.Coverage.SelectedRegularFiles != 2 || got.Counts.LFSTrackedFiles != 1 || got.Coverage.OmittedPointerFiles != 2 {
		t.Fatalf("bounded coverage: %+v", got)
	}
	if got.Omissions["file_size_limit"] != 2 || got.Omissions["gitmodules_file_size_limit"] != 1 {
		t.Fatalf("omissions: %+v", got.Omissions)
	}
}

func sparseIndexFixture(version uint32, name string, mode uint32, skip bool, extension string) []byte {
	data := make([]byte, 12)
	copy(data, "DIRC")
	binary.BigEndian.PutUint32(data[4:8], version)
	binary.BigEndian.PutUint32(data[8:12], 1)
	entry := make([]byte, 62)
	binary.BigEndian.PutUint32(entry[24:28], mode)
	flags := uint16(len(name))
	if skip {
		flags |= 0x4000
	}
	binary.BigEndian.PutUint16(entry[60:62], flags)
	data = append(data, entry...)
	if skip {
		data = append(data, 0x40, 0)
	}
	data = append(data, name...)
	data = append(data, 0)
	for (len(data)-12)%8 != 0 {
		data = append(data, 0)
	}
	if extension != "" {
		data = append(data, extension...)
		data = append(data, 0, 0, 0, 0)
	}
	sum := sha1.Sum(data)
	return append(data, sum[:]...)
}
