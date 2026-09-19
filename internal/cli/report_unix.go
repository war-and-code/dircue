//go:build unix

package cli

import (
	"os"
	"syscall"
)

func openReportFile(filename string) (*os.File, error) {
	if err := requireRegularReport(filename); err != nil {
		return nil, err
	}
	// A replacement FIFO must not block the CLI; a replacement link must not redirect it.
	return os.OpenFile(filename, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}
