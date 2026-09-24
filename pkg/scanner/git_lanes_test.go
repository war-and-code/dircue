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
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
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

func TestGitObjectLanePolicy(t *testing.T) {
	for _, test := range []struct {
		name        string
		workers     int
		wantLanes   int
		wantReaders int
	}{
		{name: "default direct call", wantLanes: 1, wantReaders: maxGitPackDescriptorsSingleLane},
		{name: "one worker", workers: 1, wantLanes: 1, wantReaders: maxGitPackDescriptorsSingleLane},
		{name: "two workers", workers: 2, wantLanes: 2, wantReaders: maxGitPackDescriptorsPerConcurrentLane},
		{name: "three workers", workers: 3, wantLanes: 3, wantReaders: maxGitPackDescriptorsPerConcurrentLane},
		{name: "many workers", workers: 16, wantLanes: 3, wantReaders: maxGitPackDescriptorsPerConcurrentLane},
	} {
		t.Run(test.name, func(t *testing.T) {
			gotLanes := gitObjectLaneCount(test.workers)
			if gotLanes != test.wantLanes {
				t.Fatalf("lanes=%d want=%d", gotLanes, test.wantLanes)
			}
			if gotReaders := gitPackDescriptorLimit(gotLanes); gotReaders != test.wantReaders {
				t.Fatalf("retained readers=%d want=%d", gotReaders, test.wantReaders)
			}
		})
	}
}

func TestGitSnapshotOpensOnlyNeededObjectLanes(t *testing.T) {
	root, _, _ := gitFixture(t, map[string]string{"main.go": goSource})
	for _, test := range []struct {
		name     string
		opts     Options
		discover bool
		lanes    int
	}{
		{name: "one-worker scan", opts: Options{Source: "git", Workers: 1}, lanes: 1},
		{name: "single-file inspect", opts: Options{Source: "git", Workers: 16}, discover: true, lanes: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot, _, err := openGitSnapshot(context.Background(), root, test.opts, test.discover, test.lanes)
			if err != nil {
				t.Fatal(err)
			}
			defer snapshot.close()
			if len(snapshot.storages) != 1 || cap(snapshot.lanes.available) != 1 {
				t.Fatalf("storages=%d lane capacity=%d", len(snapshot.storages), cap(snapshot.lanes.available))
			}
		})
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
		if f.owner.entered.Add(1) == maxGitObjectLanes {
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
	tracked := newDescriptorTrackingFS(base.Filesystem())
	shared := cache.NewObjectLRUDefault()
	lanes, storages, err := newGitObjectLanes(tracked, shared, filesystem.Options{
		LargeObjectThreshold: ClassificationBytes,
		MaxOpenDescriptors:   maxGitPackDescriptorsPerConcurrentLane,
	}, maxGitObjectLanes)
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

	primary := lanes.primaryStorage
	head, err := storer.ResolveReference(primary, plumbing.HEAD)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := object.GetCommit(primary, head.Hash())
	if err != nil {
		t.Fatal(err)
	}
	tree, err := object.GetTree(primary, commit.TreeHash)
	if err != nil {
		t.Fatal(err)
	}
	targets := make([]struct {
		hash plumbing.Hash
		name string
	}, maxGitObjectLanes)
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
	errs := make(chan error, maxGitObjectLanes)
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
	if tracked.entered.Load() < maxGitObjectLanes {
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
	if peak := tracked.peak.Load(); peak > maxGitObjectLanes*maxGitPackDescriptorsPerConcurrentLane+maxGitObjectLanes+1 {
		t.Fatalf("descriptor peak=%d exceeds fixture bound", peak)
	} else {
		t.Logf("measured repository descriptor peak: %d", peak)
	}
}
