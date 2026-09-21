//go:build !windows

package structure

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestWorkerProcessGrandchildHelper(t *testing.T) {
	if os.Getenv("DIRCUE_GRANDCHILD_HELPER") == "" {
		return
	}
	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(os.Getenv("DIRCUE_GRANDCHILD_PID"), []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
		os.Exit(3)
	}
	time.Sleep(time.Hour)
}

func TestWorkerTimeoutKillsDescendantProcess(t *testing.T) {
	pidFile := t.TempDir() + "/pid"
	worker := t.TempDir() + "/worker"
	if err := os.WriteFile(worker, []byte("#!/bin/sh\nexec \"$DIRCUE_TEST_BINARY\" -test.run='^TestWorkerProcessGrandchildHelper$'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DIRCUE_GRANDCHILD_HELPER", "1")
	t.Setenv("DIRCUE_GRANDCHILD_PID", pidFile)
	t.Setenv("DIRCUE_TEST_BINARY", os.Args[0])
	c, err := New(Options{Worker: worker, Timeout: 500 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Analyze(context.Background(), "A.java", "Java", []byte("class A {}"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		err = syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("worker descendant %d survived cancellation: %v", pid, err)
}
