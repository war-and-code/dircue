//go:build windows

package structure

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func configureWorkerProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// taskkill /T terminates the process tree rooted at the explicitly
		// selected worker. Fall back to terminating the worker if unavailable.
		systemRoot := os.Getenv("SystemRoot")
		if systemRoot == "" {
			systemRoot = `C:\Windows`
		}
		taskkill := filepath.Join(systemRoot, "System32", "taskkill.exe")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, taskkill, "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run(); err == nil {
			return nil
		}
		if err := cmd.Process.Kill(); err != nil {
			return err
		}
		return nil
	}
}
