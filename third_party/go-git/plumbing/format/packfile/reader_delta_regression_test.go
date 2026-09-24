package packfile

import (
	"bytes"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/war-and-code/dircue/third_party/go-git/plumbing"
)

func TestReaderFromDeltaBackwardThenForwardCopies(t *testing.T) {
	for _, test := range []struct {
		name  string
		delta []byte
		want  string
		seeks int
	}{
		{"backward then forward", []byte{20, 3, 0x91, 10, 1, 0x90, 1, 0x91, 13, 1}, "kan", 3},
		{"repeated backward", []byte{20, 4, 0x91, 10, 1, 0x90, 1, 0x91, 13, 1, 0x91, 2, 1}, "kanc", 4},
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
			base.requireClosed(t, 1)
			if got := int(base.seekCalls.Load()); got != test.seeks {
				t.Fatalf("seek calls = %d, want %d", got, test.seeks)
			}
		})
	}
}

func TestReaderFromDeltaFailedReopen(t *testing.T) {
	base := newTrackedDeltaBase(2)
	base.seekFailAt = 2
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

func TestReaderFromDeltaSeekFallbacks(t *testing.T) {
	delta := []byte{20, 3, 0x91, 10, 1, 0x90, 1, 0x91, 13, 1}
	for _, test := range []struct {
		name        string
		seekFailAt  int
		seekWrongAt int
	}{
		{name: "seek error", seekFailAt: 2},
		{name: "wrong seek offset", seekWrongAt: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := newTrackedDeltaBase(0)
			base.seekFailAt = test.seekFailAt
			base.seekWrongAt = test.seekWrongAt
			reader, err := ReaderFromDelta(base, bytes.NewReader(delta))
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
			if string(got) != "kan" {
				t.Fatalf("got %q, want %q", got, "kan")
			}
			base.requireClosed(t, 2)
			if got := base.calls.Load(); got != 2 {
				t.Fatalf("reader calls = %d, want 2", got)
			}
		})
	}
}

func TestReaderFromDeltaNonSeekableFallback(t *testing.T) {
	base := newTrackedDeltaBase(0)
	base.nonSeekable = true
	reader, err := ReaderFromDelta(base, bytes.NewReader([]byte{20, 3, 0x91, 10, 1, 0x90, 1, 0x91, 13, 1}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if string(got) != "kan" {
		t.Fatalf("got %q, want %q", got, "kan")
	}
	base.requireClosed(t, 2)
}

func TestReaderFromDeltaSeekResetsBufferedState(t *testing.T) {
	base := newTrackedDeltaBase(0)
	// The second copy begins exactly where the first ends and must reuse the
	// buffered reader. The later backward and forward copies must seek and reset
	// that buffer so read-ahead bytes cannot leak into the output.
	delta := buildDelta(20, 8,
		encodeCopyOperation(10, 2),
		encodeCopyOperation(12, 2),
		encodeCopyOperation(0, 2),
		encodeCopyOperation(18, 2),
	)
	reader, err := ReaderFromDelta(base, bytes.NewReader(delta))
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if string(got) != "klmnabst" {
		t.Fatalf("got %q, want %q", got, "klmnabst")
	}
	base.requireClosed(t, 1)
	if got := base.seekCalls.Load(); got != 3 {
		t.Fatalf("seek calls = %d, want 3", got)
	}
}

func TestReaderFromDeltaRejectsShortBaseAndDelta(t *testing.T) {
	for _, test := range []struct {
		name        string
		nonSeekable bool
		seekFailAt  int
		wantClosed  int
	}{
		{name: "seekable", wantClosed: 1},
		{name: "non-seekable", nonSeekable: true, wantClosed: 1},
		{name: "failed seek fallback", seekFailAt: 1, wantClosed: 2},
	} {
		t.Run("short base/"+test.name, func(t *testing.T) {
			base := newTrackedDeltaBase(0)
			base.data = []byte("short")
			base.nonSeekable = test.nonSeekable
			base.seekFailAt = test.seekFailAt
			reader, err := ReaderFromDelta(base, bytes.NewReader(buildDelta(20, 1, encodeCopyOperation(10, 1))))
			if err != nil {
				t.Fatal(err)
			}
			_, err = io.ReadAll(reader)
			if !errors.Is(err, ErrInvalidDelta) {
				t.Fatalf("got %v, want invalid delta", err)
			}
			base.requireClosed(t, test.wantClosed)
		})
	}
	t.Run("truncated delta insertion", func(t *testing.T) {
		base := newTrackedDeltaBase(0)
		reader, err := ReaderFromDelta(base, bytes.NewReader([]byte{20, 3, 3, 'x'}))
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.ReadAll(reader)
		if !errors.Is(err, ErrInvalidDelta) {
			t.Fatalf("got %v, want invalid delta", err)
		}
		base.requireClosed(t, 1)
	})
	for _, test := range []struct {
		name  string
		delta []byte
	}{
		{name: "offset operand", delta: []byte{20, 1, 0x81}},
		{name: "size operand", delta: []byte{20, 1, 0x90}},
	} {
		t.Run("truncated copy "+test.name, func(t *testing.T) {
			base := newTrackedDeltaBase(0)
			reader, err := ReaderFromDelta(base, bytes.NewReader(test.delta))
			if err != nil {
				t.Fatal(err)
			}
			_, err = io.ReadAll(reader)
			if !errors.Is(err, ErrInvalidDelta) {
				t.Fatalf("got %v, want invalid delta", err)
			}
			base.requireClosed(t, 1)
		})
	}
}

func TestReaderFromDeltaPreservesBaseReadError(t *testing.T) {
	readError := errors.New("base read failed")
	base := newTrackedDeltaBase(0)
	base.readError = readError
	reader, err := ReaderFromDelta(base, bytes.NewReader(buildDelta(20, 1, encodeCopyOperation(10, 1))))
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(reader)
	if !errors.Is(err, readError) {
		t.Fatalf("got %v, want base read error", err)
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

func TestReaderFromDeltaMetricsPreserveBehaviorAndCapabilities(t *testing.T) {
	delta := []byte{20, 3, 0x91, 10, 1, 0x90, 1, 0x91, 13, 1}
	for _, test := range []struct {
		name        string
		nonSeekable bool
		wantSeeks   int
	}{
		{name: "seekable", wantSeeks: 3},
		{name: "non-seekable", nonSeekable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			plainBase := newTrackedDeltaBase(0)
			plainBase.nonSeekable = test.nonSeekable
			plain, err := ReaderFromDelta(plainBase, bytes.NewReader(delta))
			if err != nil {
				t.Fatal(err)
			}
			plainBytes, plainErr := io.ReadAll(plain)
			if closeErr := plain.Close(); plainErr == nil {
				plainErr = closeErr
			}

			metrics := &ReadMetrics{}
			measuredBase := newTrackedDeltaBase(0)
			measuredBase.nonSeekable = test.nonSeekable
			measured, err := ReaderFromDeltaWithMetrics(measuredBase, bytes.NewReader(delta), metrics)
			if err != nil {
				t.Fatal(err)
			}
			measuredBytes, measuredErr := io.ReadAll(measured)
			if closeErr := measured.Close(); measuredErr == nil {
				measuredErr = closeErr
			}
			if !bytes.Equal(measuredBytes, plainBytes) || !errors.Is(measuredErr, plainErr) {
				t.Fatalf("measured (%q, %v), plain (%q, %v)", measuredBytes, measuredErr, plainBytes, plainErr)
			}
			if got := int(measuredBase.seekCalls.Load()); got != test.wantSeeks {
				t.Fatalf("measured seek calls = %d, want %d", got, test.wantSeeks)
			}
			snapshot := metrics.Snapshot()
			if snapshot.DeltaBaseBytesRead == 0 || snapshot.ActiveDeltaReaders != 0 {
				t.Fatalf("unexpected metrics: %+v", snapshot)
			}
		})
	}

	bad := []byte{20, 3, 3, 'x'}
	plain, err := ReaderFromDelta(newTrackedDeltaBase(0), bytes.NewReader(bad))
	if err != nil {
		t.Fatal(err)
	}
	_, plainErr := io.ReadAll(plain)
	measured, err := ReaderFromDeltaWithMetrics(newTrackedDeltaBase(0), bytes.NewReader(bad), &ReadMetrics{})
	if err != nil {
		t.Fatal(err)
	}
	_, measuredErr := io.ReadAll(measured)
	if !errors.Is(plainErr, ErrInvalidDelta) || !errors.Is(measuredErr, ErrInvalidDelta) {
		t.Fatalf("plain error %v, measured error %v", plainErr, measuredErr)
	}
}

func TestReadMetricsResetHonorsActiveDeltaReader(t *testing.T) {
	delta := &blockingDeltaReader{data: []byte{20, 1}, release: make(chan struct{})}
	metrics := &ReadMetrics{}
	reader, err := ReaderFromDeltaWithMetrics(newTrackedDeltaBase(0), delta, metrics)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Reset() {
		t.Fatal("reset succeeded with active delta reader")
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	close(delta.release)
	deadline := time.Now().Add(3 * time.Second)
	for metrics.Snapshot().ActiveDeltaReaders != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if snapshot := metrics.Snapshot(); snapshot.ActiveDeltaReaders != 0 {
		t.Fatalf("delta reader remained active: %+v", snapshot)
	}
	if !metrics.Reset() || metrics.Snapshot() != (ReadMetricsSnapshot{}) {
		t.Fatalf("reset did not clear completed run: %+v", metrics.Snapshot())
	}
}

type blockingDeltaReader struct {
	data    []byte
	release chan struct{}
}

func (reader *blockingDeltaReader) Read(p []byte) (int, error) {
	if len(reader.data) > 0 {
		n := copy(p, reader.data)
		reader.data = reader.data[n:]
		return n, nil
	}
	<-reader.release
	return 0, io.EOF
}

type trackedDeltaBase struct {
	plumbing.MemoryObject
	calls       atomic.Int32
	seekCalls   atomic.Int32
	failAt      int
	seekFailAt  int
	seekWrongAt int
	nonSeekable bool
	readError   error
	data        []byte
	closed      chan int
}

func newTrackedDeltaBase(failAt int) *trackedDeltaBase {
	base := &trackedDeltaBase{failAt: failAt, data: []byte("abcdefghijklmnopqrst"), closed: make(chan int, 10)}
	base.SetSize(20)
	return base
}

func (base *trackedDeltaBase) Reader() (io.ReadCloser, error) {
	id := int(base.calls.Add(1))
	if id == base.failAt {
		return nil, errors.New("cannot reopen base")
	}
	reader := bytes.NewReader(base.data)
	if base.readError != nil {
		return &trackedReadErrorDeltaReader{err: base.readError, id: id, closed: base.closed}, nil
	}
	if base.nonSeekable {
		return &trackedNonSeekableDeltaReader{reader: reader, id: id, closed: base.closed}, nil
	}
	return &trackedDeltaReader{Reader: reader, owner: base, id: id, closed: base.closed}, nil
}

type trackedReadErrorDeltaReader struct {
	err    error
	id     int
	closed chan<- int
}

func (reader *trackedReadErrorDeltaReader) Read([]byte) (int, error) { return 0, reader.err }
func (reader *trackedReadErrorDeltaReader) Close() error {
	reader.closed <- reader.id
	return nil
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
	owner  *trackedDeltaBase
	id     int
	closed chan<- int
}

func (reader *trackedDeltaReader) Seek(offset int64, whence int) (int64, error) {
	call := int(reader.owner.seekCalls.Add(1))
	if call == reader.owner.seekFailAt {
		return 0, errors.New("cannot seek base")
	}
	if call == reader.owner.seekWrongAt {
		return reader.Reader.Seek(offset+1, whence)
	}
	return reader.Reader.Seek(offset, whence)
}

func (reader *trackedDeltaReader) Close() error { reader.closed <- reader.id; return nil }

type trackedNonSeekableDeltaReader struct {
	reader *bytes.Reader
	id     int
	closed chan<- int
}

func (reader *trackedNonSeekableDeltaReader) Read(p []byte) (int, error) {
	return reader.reader.Read(p)
}

func (reader *trackedNonSeekableDeltaReader) Close() error {
	reader.closed <- reader.id
	return nil
}
