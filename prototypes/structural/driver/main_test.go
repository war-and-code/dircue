package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("DIRCUE_PROTOTYPE_TEST_WORKER"); mode != "" {
		switch mode {
		case "sleep":
			time.Sleep(time.Minute)
		case "malformed":
			fmt.Println(`{"status":"complete"} trailing`)
		case "wrong-file":
			fmt.Println(`{"status":"complete","path":"other.java","language":"Java","source_bytes":0,"parse_count":1}`)
		case "error":
			fmt.Fprintln(os.Stderr, "fixture failure")
			os.Exit(2)
		case "structured-error":
			fmt.Println(`{"status":"error","error":{"code":"binary_source","message":"NUL in source"},"parse_count":0}`)
			os.Exit(2)
		case "overflow":
			io.CopyN(os.Stdout, repeatReader{}, 17<<20)
		default:
			var req request
			if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
				os.Exit(2)
			}
			status := "complete"
			if mode == "partial" {
				status = "partial"
			}
			result := map[string]any{"status": status, "path": req.Path, "language": req.Language, "source_bytes": len(req.Source), "parse_count": 1, "provenance": map[string]string{"bca": "fixture"}}
			if req.Mode != "metrics" {
				result["observations"] = map[string]int{}
			}
			if req.Mode != "structure" {
				result["metrics"] = map[string]int{}
			}
			if mode == "missing-metrics" {
				delete(result, "metrics")
			}
			if mode == "missing-provenance" {
				delete(result, "provenance")
			}
			json.NewEncoder(os.Stdout).Encode(result)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type repeatReader struct{}

func (repeatReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func worker(t *testing.T, mode string) string {
	t.Helper()
	t.Setenv("DIRCUE_PROTOTYPE_TEST_WORKER", mode)
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func fixture(t *testing.T, root, path string, content []byte) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, content, 0600); err != nil {
		t.Fatal(err)
	}
}

func records(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var result []map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	for {
		var value map[string]any
		err := dec.Decode(&value)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, value)
	}
	return result
}

func TestWalkSelectionAndOrder(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "B.cs", []byte("class B {}"))
	fixture(t, root, "A.java", []byte("class A {}"))
	fixture(t, root, ".cache/Ignore.java", []byte("ignored"))
	fixture(t, root, "obj/Ignore.cs", []byte("ignored"))
	fixture(t, root, "logs.xml", []byte("not read"))
	var out, stderr bytes.Buffer
	code := run(context.Background(), []string{"--worker", worker(t, "complete"), root}, &out, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d %s", code, stderr.String())
	}
	rs := records(t, out.Bytes())
	if len(rs) != 5 || rs[0]["path"] != ".cache" || rs[1]["path"] != "A.java" || rs[2]["path"] != "B.cs" || rs[3]["path"] != "obj" {
		t.Fatalf("records: %s", out.String())
	}
	s := rs[4]
	if s["scope"] != "working-directory" || s["observed"] != float64(2) || s["excluded_directories"] != float64(2) || s["ignored_files"] != float64(1) || s["status"] != "complete" {
		t.Fatalf("summary: %v", s)
	}
}

func TestFileBoundsUTF8AndSymlink(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "big.cs", []byte("123456789"))
	fixture(t, root, "invalid.cs", []byte{0xff})
	fixture(t, root, "ok.java", []byte(""))
	if err := os.Symlink(filepath.Join(root, "ok.java"), filepath.Join(root, "link.java")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	var out, stderr bytes.Buffer
	code := run(context.Background(), []string{"--worker", worker(t, "complete"), "--max-file-bytes", "8", root}, &out, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d %s", code, stderr.String())
	}
	rs := records(t, out.Bytes())
	want := []string{"file-too-large", "invalid-utf8", "non-regular-file"}
	for i, reason := range want {
		if rs[i]["reason"] != reason {
			t.Fatalf("record %d: %v", i, rs[i])
		}
	}
	s := rs[len(rs)-1]
	if s["status"] != "partial" || s["skipped"] != float64(3) || s["observed"] != float64(1) {
		t.Fatalf("summary: %v", s)
	}
}

func TestWorkerFailuresAndPartial(t *testing.T) {
	for _, tc := range []struct{ mode, contains string }{
		{"malformed", "invalid result"}, {"wrong-file", "does not match"}, {"missing-metrics", "analysis mode"}, {"missing-provenance", "provenance contract"}, {"error", "fixture failure"}, {"structured-error", "binary_source: NUL in source"}, {"overflow", "worker-output-limit"}, {"sleep", "deadline exceeded"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			start := time.Now()
			timeout := 5 * time.Second
			if tc.mode == "sleep" {
				timeout = 50 * time.Millisecond
			}
			_, _, err := invoke(context.Background(), worker(t, tc.mode), request{Path: "x.java", Language: "Java"}, timeout)
			if err == nil || !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("got %v, need %q", err, tc.contains)
			}
			if tc.mode == "sleep" && time.Since(start) > 2*time.Second {
				t.Fatal("worker was not stopped promptly")
			}
		})
	}
	t.Run("partial", func(t *testing.T) {
		_, partial, err := invoke(context.Background(), worker(t, "partial"), request{}, time.Second)
		if err != nil || !partial {
			t.Fatalf("partial=%v err=%v", partial, err)
		}
	})
}

func TestParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := invoke(ctx, worker(t, "sleep"), request{}, time.Second)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatal(err)
	}
}

func TestArgumentValidation(t *testing.T) {
	w := worker(t, "complete")
	for _, args := range [][]string{nil, {"--worker", w, "--mode", "oops"}, {"--worker", w, "--max-file-bytes", "8388609"}, {"--worker", w, "--max-file-bytes", "0"}, {"--worker", w, "--timeout", "0s"}, {"--worker", "/not-present"}, {"--worker", w, "a", "b"}} {
		var out, stderr bytes.Buffer
		if code := run(context.Background(), args, &out, &stderr); code != 2 || out.Len() != 0 {
			t.Fatalf("args %v: code=%d stdout=%s", args, code, out.String())
		}
	}
}

func TestCappedBufferNeverGrowsPastLimit(t *testing.T) {
	canceled := false
	b := cappedBuffer{limit: 4, cancel: func() { canceled = true }}
	b.Write([]byte("abc"))
	b.Write([]byte("def"))
	b.Write([]byte("ghi"))
	if b.buf.String() != "abcd" || !b.overflow || !canceled {
		t.Fatalf("buffer: %+v", b)
	}
}
