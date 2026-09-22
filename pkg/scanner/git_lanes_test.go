package scanner

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

func TestGitObjectLanesAcquireAndCancellation(t *testing.T) {
	lanes := &gitObjectLanes{available: make(chan *gitObjectLane, 2)}
	first := &gitObjectLane{}
	second := &gitObjectLane{}
	lanes.available <- first
	lanes.available <- second

	gotFirst, releaseFirst, err := lanes.acquire(context.Background())
	if err != nil || gotFirst != first {
		t.Fatalf("first lane=%p error=%v", gotFirst, err)
	}
	gotSecond, releaseSecond, err := lanes.acquire(context.Background())
	if err != nil || gotSecond != second {
		t.Fatalf("second lane=%p error=%v", gotSecond, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := lanes.acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting acquisition ignored cancellation: %v", err)
	}
	releaseSecond()
	releaseFirst()
	if len(lanes.available) != 2 {
		t.Fatalf("released lanes=%d", len(lanes.available))
	}
	if _, _, err := lanes.acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("available lane won over pre-canceled context: %v", err)
	}
	if len(lanes.available) != 2 {
		t.Fatalf("pre-canceled acquisition consumed a lane: %d", len(lanes.available))
	}
}

type descriptorTrackingFS struct {
	billy.Filesystem
	active  atomic.Int64
	peak    atomic.Int64
	block   atomic.Bool
	entered atomic.Int64
	release chan struct{}
	once    sync.Once
}

func newDescriptorTrackingFS(fs billy.Filesystem) *descriptorTrackingFS {
	return &descriptorTrackingFS{Filesystem: fs, release: make(chan struct{})}
}

func (f *descriptorTrackingFS) track(file billy.File, err error) (billy.File, error) {
	if err != nil {
		return nil, err
	}
	active := f.active.Add(1)
	for peak := f.peak.Load(); active > peak && !f.peak.CompareAndSwap(peak, active); peak = f.peak.Load() {
	}
	return &descriptorTrackingFile{File: file, owner: f}, nil
}

func (f *descriptorTrackingFS) Create(name string) (billy.File, error) {
	return f.track(f.Filesystem.Create(name))
}

func (f *descriptorTrackingFS) Open(name string) (billy.File, error) {
	return f.track(f.Filesystem.Open(name))
}

func (f *descriptorTrackingFS) OpenFile(name string, flag int, perm os.FileMode) (billy.File, error) {
	return f.track(f.Filesystem.OpenFile(name, flag, perm))
}

type descriptorTrackingFile struct {
	billy.File
	owner     *descriptorTrackingFS
	closeOnce sync.Once
	blockOnce sync.Once
}

func (f *descriptorTrackingFile) waitForPeer() {
	if !f.owner.block.Load() || filepath.Ext(f.Name()) != ".pack" {
		return
	}
	f.blockOnce.Do(func() {
		if f.owner.entered.Add(1) == gitObjectLaneCount {
			f.owner.once.Do(func() { close(f.owner.release) })
		}
		<-f.owner.release
	})
}

func (f *descriptorTrackingFile) Read(p []byte) (int, error) {
	f.waitForPeer()
	return f.File.Read(p)
}

func (f *descriptorTrackingFile) ReadAt(p []byte, off int64) (int, error) {
	f.waitForPeer()
	return f.File.ReadAt(p, off)
}

func (f *descriptorTrackingFile) Close() error {
	var err error
	f.closeOnce.Do(func() {
		err = f.File.Close()
		f.owner.active.Add(-1)
	})
	return err
}

func TestGitObjectLanesShareBoundedCacheAndCloseDescriptors(t *testing.T) {
	root, names, files, _ := packedCacheFixture(t)
	opened, err := git.PlainOpen(root)
	if err != nil {
		t.Fatal(err)
	}
	base := opened.Storer.(*filesystem.Storage)
	worktree, err := opened.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	tracked := newDescriptorTrackingFS(base.Filesystem())
	shared := cache.NewObjectLRUDefault()
	lanes, storages, err := newGitObjectLanes(tracked, worktree.Filesystem, shared, filesystem.Options{
		LargeObjectThreshold: ClassificationBytes,
		MaxOpenDescriptors:   maxGitPackDescriptorsPerLane,
	}, gitObjectLaneCount)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, storage := range storages {
			if err := storage.Close(); err != nil {
				t.Error(err)
			}
		}
	}()

	primary := lanes.primaryRepo
	head, err := primary.Head()
	if err != nil {
		t.Fatal(err)
	}
	commit, err := primary.CommitObject(head.Hash())
	if err != nil {
		t.Fatal(err)
	}
	tree, err := commit.Tree()
	if err != nil {
		t.Fatal(err)
	}
	targets := make([]struct {
		hash plumbing.Hash
		name string
	}, 2)
	for i := range targets {
		entry, err := tree.FindEntry(names[i])
		if err != nil {
			t.Fatal(err)
		}
		targets[i].hash = entry.Hash
		targets[i].name = names[i]
	}

	tracked.block.Store(true)
	timeout := time.AfterFunc(5*time.Second, func() {
		tracked.once.Do(func() { close(tracked.release) })
	})
	defer timeout.Stop()
	errs := make(chan error, 2)
	for i := range targets {
		go func(i int) {
			hash := targets[i].hash
			name := targets[i].name
			content, size, err := lanes.read(context.Background(), hash, name, ClassificationBytes)
			if err == nil && (size != int64(len(files[name])) || int64(len(content)) != ClassificationBytes) {
				err = io.ErrUnexpectedEOF
			}
			errs <- err
		}(i)
	}
	for range targets {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if tracked.entered.Load() < gitObjectLaneCount {
		t.Fatalf("object reads did not overlap across lanes: %d", tracked.entered.Load())
	}
	for _, storage := range storages {
		if err := storage.Close(); err != nil {
			t.Fatal(err)
		}
	}
	storages = nil
	if active := tracked.active.Load(); active != 0 {
		t.Fatalf("descriptors remain open after lane close: %d", active)
	}
	// The fixture observes every index, retained pack, and lazy FSObject open.
	// This is a measured bound for the fixture, not a universal pack-chain cap.
	if peak := tracked.peak.Load(); peak > gitObjectLaneCount*maxGitPackDescriptorsPerLane+gitObjectLaneCount+1 {
		t.Fatalf("descriptor peak=%d exceeds fixture bound", peak)
	} else {
		t.Logf("measured repository descriptor peak: %d", peak)
	}
}
