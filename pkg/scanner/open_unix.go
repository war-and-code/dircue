//go:build unix

package scanner

import (
	"os"
	"syscall"
)

// Nonblocking open prevents a regular-file-to-FIFO race from hanging workers.
// NOFOLLOW rejects final-component symlinks introduced after the Lstat check.
func openRegular(root *os.Root, filename string) (*os.File, error) {
	return root.OpenFile(filename, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}
