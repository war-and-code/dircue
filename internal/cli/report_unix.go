//go:build unix

package cli

import (
	"os"
	"syscall"
)

func openInputFile(filename, label string) (*os.File, error) {
	if err := requireRegularInput(filename, label); err != nil {
		return nil, err
	}
	// A replacement FIFO must not block the CLI; a replacement link must not redirect it.
	return os.OpenFile(filename, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}
