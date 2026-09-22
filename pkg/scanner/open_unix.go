//go:build unix

package scanner

import (
	"os"
	"syscall"
)

// Nonblocking open prevents a regular-file-to-FIFO race from hanging workers.
// os.Root may resolve in-root symlinks even with NOFOLLOW. The caller must
// retain its Lstat check; these flags alone do not enforce a no-symlink policy.
func openRegular(root *os.Root, filename string) (*os.File, error) {
	return root.OpenFile(filename, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}
