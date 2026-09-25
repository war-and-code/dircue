//go:build unix

package treehash

import (
	"os"
	"syscall"
)

// openRegular opens without blocking so a regular file replaced by a FIFO
// between Lstat and open cannot hang a worker. Callers keep their Lstat check
// and verify the opened descriptor is still a regular file.
func openRegular(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}
