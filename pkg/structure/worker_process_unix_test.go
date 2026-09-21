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
	// Exceed the original 500 ms timeout to reproduce slow race-instrumented
	// test-binary startup on Intel CI before publishing readiness.
	time.Sleep(750 * time.Millisecond)
	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		os.Exit(2)
	}
	pidFile := os.Getenv("DIRCUE_GRANDCHILD_PID")
	temporary := pidFile + ".tmp"
	if err := os.WriteFile(temporary, []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
		os.Exit(3)
	}
	if err := os.Rename(temporary, pidFile); err != nil {
		os.Exit(4)
	}
	time.Sleep(time.Hour)
}

func TestWorkerCancellationKillsReadyDescendantProcess(t *testing.T) {
	pidFile := t.TempDir() + "/pid"
	worker := t.TempDir() + "/worker"
	if err := os.WriteFile(worker, []byte("#!/bin/sh\nexec \"$DIRCUE_TEST_BINARY\" -test.run='^TestWorkerProcessGrandchildHelper$'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DIRCUE_GRANDCHILD_HELPER", "1")
	t.Setenv("DIRCUE_GRANDCHILD_PID", pidFile)
	t.Setenv("DIRCUE_TEST_BINARY", os.Args[0])
	c, err := New(Options{Worker: worker, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, analyzeErr := c.Analyze(ctx, "A.java", "Java", []byte("class A {}"))
		result <- analyzeErr
	}()
	stop := func() {
		cancel()
		select {
		case <-result:
		case <-time.After(5 * time.Second):
		}
	}

	var data []byte
	pid := 0
	readyDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(readyDeadline) {
		select {
		case err = <-result:
			cancel()
			t.Fatalf("worker returned before descendant readiness: %v", err)
		default:
		}
		data, err = os.ReadFile(pidFile)
		if err == nil {
			pid, err = strconv.Atoi(string(data))
			if err == nil && pid > 0 {
				break
			}
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			stop()
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		stop()
		t.Fatalf("worker did not publish descendant readiness: %v", err)
	}
	descendantGone := false
	t.Cleanup(func() {
		cancel()
		if !descendantGone {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})

	cancel()
	select {
	case err = <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not return after cancellation")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		err = syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			descendantGone = true
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("worker descendant %d survived cancellation: %v", pid, err)
}
