package packfile

import (
	"bytes"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
)

func TestReaderFromDeltaBackwardThenForwardCopies(t *testing.T) {
	for _, test := range []struct {
		name  string
		delta []byte
		want  string
		opens int
	}{
		{"backward then forward", []byte{20, 3, 0x91, 10, 1, 0x90, 1, 0x91, 13, 1}, "kan", 2},
		{"repeated backward", []byte{20, 4, 0x91, 10, 1, 0x90, 1, 0x91, 13, 1, 0x91, 2, 1}, "kanc", 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := newTrackedDeltaBase(0)
			reader, err := ReaderFromDelta(base, bytes.NewReader(test.delta))
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			got, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
			base.requireClosed(t, test.opens)
		})
	}
}

func TestReaderFromDeltaFailedReopen(t *testing.T) {
	base := newTrackedDeltaBase(2)
	reader, err := ReaderFromDelta(base, bytes.NewReader([]byte{20, 2, 0x91, 10, 1, 0x90, 1}))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	_, err = io.ReadAll(reader)
	if !errors.Is(err, ErrInvalidDelta) {
		t.Fatalf("got %v, want invalid delta", err)
	}
	base.requireClosed(t, 1)
}

func TestReaderFromDeltaEarlyCloseReleasesBase(t *testing.T) {
	base := newTrackedDeltaBase(0)
	reader, err := ReaderFromDelta(base, bytes.NewReader([]byte{20, 3, 0x91, 10, 1, 0x90, 1, 0x91, 13, 1}))
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	base.requireClosed(t, 1)
}

type trackedDeltaBase struct {
	plumbing.MemoryObject
	calls  atomic.Int32
	failAt int
	closed chan int
}

func newTrackedDeltaBase(failAt int) *trackedDeltaBase {
	base := &trackedDeltaBase{failAt: failAt, closed: make(chan int, 10)}
	base.SetSize(20)
	return base
}

func (base *trackedDeltaBase) Reader() (io.ReadCloser, error) {
	id := int(base.calls.Add(1))
	if id == base.failAt {
		return nil, errors.New("cannot reopen base")
	}
	return &trackedDeltaReader{Reader: bytes.NewReader([]byte("abcdefghijklmnopqrst")), id: id, closed: base.closed}, nil
}

func (base *trackedDeltaBase) requireClosed(t *testing.T, count int) {
	t.Helper()
	closed := make(map[int]bool)
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for len(closed) < count {
		select {
		case id := <-base.closed:
			if closed[id] {
				t.Fatalf("reader %d closed twice", id)
			}
			closed[id] = true
		case <-timer.C:
			t.Fatalf("closed %d readers, want %d", len(closed), count)
		}
	}
	select {
	case id := <-base.closed:
		t.Fatalf("unexpected additional close of reader %d", id)
	default:
	}
}

type trackedDeltaReader struct {
	*bytes.Reader
	id     int
	closed chan<- int
}

func (reader *trackedDeltaReader) Close() error { reader.closed <- reader.id; return nil }
