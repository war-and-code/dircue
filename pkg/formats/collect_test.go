package formats

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func candidate(name string, data []byte) Candidate {
	return Candidate{Path: name, Size: int64(len(data)), Read: func(_ context.Context, n int64) ([]byte, int64, error) {
		return data[:min(int64(len(data)), n)], int64(len(data)), nil
	}}
}
func TestCollectorDeterministicSelectionAndCountBudget(t *testing.T) {
	run := func(reverse bool) *Report {
		c := New("directory", "", 0)
		for k := 0; k < MaxFiles+7; k++ {
			i := k
			if reverse {
				i = MaxFiles + 6 - k
			}
			c.Add(candidate(fmt.Sprintf("%05d.json", i), []byte("{}")))
		}
		r, err := c.Finish(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	a, b := run(false), run(true)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("selection depends on arrival order")
	}
	if a.Status != "partial" || a.Coverage.SelectedFiles != MaxFiles+7 || a.Coverage.OmittedFiles != 7 || a.Coverage.InspectedFiles != MaxFiles || a.Observations[0].Path != "00000.json" || a.Observations[MaxFiles-1].Path != fmt.Sprintf("%05d.json", MaxFiles-1) {
		t.Fatalf("coverage: %+v", a.Coverage)
	}
}
func TestCollectorChargesLookaheadAndTotalBudget(t *testing.T) {
	data := bytes.Repeat([]byte(" "), int(MaxFileBytes)+1)
	calls := 0
	c := New("git", "tree", 0)
	for i := 0; i < 600; i++ {
		v := candidate(fmt.Sprintf("%04d.xml", i), data)
		v.Size = 2 << 30
		v.Read = func(_ context.Context, n int64) ([]byte, int64, error) {
			calls++
			if n > MaxFileBytes+1 {
				t.Fatal("oversized request")
			}
			return data[:n], 2 << 30, nil
		}
		c.Add(v)
	}
	r, err := c.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Coverage.InspectedBytes != MaxInputBytes || r.Coverage.CompleteReads != 0 || r.Coverage.PrefixReads != int64(calls) || r.Coverage.OmittedFiles+int64(calls) != 600 || r.Status != "partial" {
		t.Fatalf("coverage: %+v calls=%d", r.Coverage, calls)
	}
	for _, o := range r.Observations {
		if o.ReadScope != "prefix" {
			t.Fatalf("whole giant file claimed: %+v", o)
		}
		for _, e := range o.Evidence {
			if e.Basis == "complete_validation" {
				t.Fatalf("prefix validated: %+v", o)
			}
		}
	}
}
func TestCollectorRejectsPathsAndSourceLimit(t *testing.T) {
	c := New("directory", "", 2)
	for _, name := range []string{"../escape", "bad\xff", ".", string(bytes.Repeat([]byte("a"), MaxPathBytes+1))} {
		c.Add(candidate(name, nil))
	}
	c.Add(candidate("big.json", []byte("123")))
	c.Add(candidate("small", []byte("1")))
	c.Omit("non_regular_file")
	r, err := c.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Coverage.SelectedFiles != 6 || r.Coverage.OmittedFiles != 5 || r.Coverage.InspectedFiles != 1 || r.Omissions["non_regular_file"] != 1 {
		t.Fatalf("coverage %+v omissions%v", r.Coverage, r.Omissions)
	}
	b, _ := json.Marshal(r)
	if !json.Valid(b) || bytes.Contains(b, []byte("escape")) {
		t.Fatal("unsafe path serialized")
	}
}
func TestCollectorChangedSourceAndCancellation(t *testing.T) {
	c := New("directory", "", 0)
	v := candidate("x.json", []byte("{}"))
	v.Size = 20
	c.Add(v)
	r, err := c.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "partial" || r.Coverage.CompleteReads != 0 || r.Coverage.PrefixReads != 1 || hasEvidence(r.Observations[0], "json", "complete_validation") || !hasDiagnostic(r.Observations[0], "source_changed_or_incomplete_read") {
		t.Fatalf("changed source: %+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c = New("directory", "", 0)
	c.Add(v)
	if _, err := c.Finish(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v", err)
	}
}
func TestCollectorReaderFailures(t *testing.T) {
	for _, reader := range []func(context.Context, int64) ([]byte, int64, error){nil, func(context.Context, int64) ([]byte, int64, error) { return nil, 0, errors.New("secret source error") }, func(_ context.Context, n int64) ([]byte, int64, error) { return make([]byte, n+1), n + 1, nil }} {
		c := New("directory", "", 0)
		c.Add(Candidate{Path: "a", Size: 1, Read: reader})
		if _, err := c.Finish(context.Background()); err == nil {
			t.Fatal("bad reader accepted")
		} else if bytes.Contains([]byte(err.Error()), []byte("secret")) {
			t.Fatal("source error disclosed")
		}
	}
}
func TestCollectorOutputBudget(t *testing.T) {
	c := New("directory", "", 0)
	for i := 0; i < MaxFiles; i++ {
		name := fmt.Sprintf("%04d-", i) + string(bytes.Repeat([]byte("\x01"), MaxPathBytes-5))
		c.Add(candidate(name, []byte("{}")))
	}
	r, err := c.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(r)
	if err != nil || len(encoded) > MaxOutputBytes {
		t.Fatalf("output %d %v", len(encoded), err)
	}
	if r.Omissions["output_byte_limit"] == 0 || r.Coverage.RetainedObservations >= r.Coverage.InspectedFiles {
		t.Fatalf("output cap not used: %+v", r.Coverage)
	}
}
