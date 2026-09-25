//go:build linux || darwin

package scanner

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/war-and-code/dircue/pkg/registries"
)

func TestRegistriesSpecialFilesAreNeverOpened(t *testing.T) {
	dir := fixtures(t, map[string]string{"real/NuGet.Config": registryNuGet})
	if err := syscall.Mkfifo(filepath.Join(dir, ".npmrc"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := fixtures(t, map[string]string{"NuGet.Config": "SECRET"})
	if err := os.Symlink(filepath.Join(outside, "NuGet.Config"), filepath.Join(dir, "NuGet.Config")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := Scan(ctx, dir, registryOptions())
	if err != nil || r.Registries.Coverage.ReadFiles != 1 || r.Registries.Omissions["non_regular_file"] != 2 {
		t.Fatalf("special files %+v %v", r, err)
	}
}

func TestRegistriesDeferredDirectoryReadRemainsConfined(t *testing.T) {
	for _, change := range []string{"grow", "delete", "symlink", "parent-symlink"} {
		t.Run(change, func(t *testing.T) {
			dir := fixtures(t, map[string]string{"sub/.npmrc": registryNPM})
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			a, err := newRegistryAccumulator(registryOptions(), nil)
			if err != nil {
				t.Fatal(err)
			}
			candidate := registryCandidate(root, job{path: "sub/.npmrc", size: int64(len(registryNPM))})
			if err := a.add(result{path: "sub/.npmrc", registryFile: candidate}); err != nil {
				t.Fatal(err)
			}
			outside := fixtures(t, map[string]string{".npmrc": "registry=https://SECRET.invalid\n"})
			switch change {
			case "grow":
				err = os.WriteFile(filepath.Join(dir, "sub/.npmrc"), []byte(registryNPM+"x"), 0600)
			case "delete":
				err = os.Remove(filepath.Join(dir, "sub/.npmrc"))
			case "symlink":
				if err = os.Remove(filepath.Join(dir, "sub/.npmrc")); err == nil {
					err = os.Symlink(filepath.Join(outside, ".npmrc"), filepath.Join(dir, "sub/.npmrc"))
				}
			case "parent-symlink":
				if err = os.Rename(filepath.Join(dir, "sub"), filepath.Join(dir, "original")); err == nil {
					err = os.Symlink(outside, filepath.Join(dir, "sub"))
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			r, err := a.finish(context.Background())
			if change == "grow" {
				if err != nil || r.Configurations[0].Omissions["incomplete_content"] != 1 {
					t.Fatalf("growth %+v %v", r, err)
				}
			} else if r != nil || err != registries.ErrRead {
				t.Fatalf("unsafe deferred read %+v %v", r, err)
			}
		})
	}
}

func TestRegistriesContinueOmitsDeferredSymlinkWithoutFollowingIt(t *testing.T) {
	dir := fixtures(t, map[string]string{".npmrc": registryNPM})
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	opts := registryOptions()
	opts.ErrorPolicy = ErrorPolicyContinue
	a, err := newRegistryAccumulator(opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	candidate := registryCandidate(root, job{path: ".npmrc", size: int64(len(registryNPM))})
	if err := a.add(result{path: ".npmrc", registryFile: candidate}); err != nil {
		t.Fatal(err)
	}
	outside := fixtures(t, map[string]string{"secret.npmrc": "registry=https://SECRET.invalid\n"})
	if err := os.Remove(filepath.Join(dir, ".npmrc")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.npmrc"), filepath.Join(dir, ".npmrc")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	r, err := a.finish(context.Background())
	if err != nil {
		t.Fatalf("continue policy aborted on confined refusal: %v", err)
	}
	if r.Status != "partial" || len(r.Configurations) != 1 || r.Configurations[0].Omissions["file_read_error"] != 1 {
		t.Fatalf("missing safe per-file omission: %+v", r)
	}
	if r.Coverage.ReadFiles != 0 || len(r.Configurations[0].Declarations) != 0 {
		t.Fatalf("symlink target contributed evidence: %+v", r)
	}
	if got := a.collector.ReadErrors(); len(got) != 1 || got[0] != ".npmrc" {
		t.Fatalf("read-error paths: %v", got)
	}
}
