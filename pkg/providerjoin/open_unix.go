//go:build unix

package providerjoin

import (
	"os"
	"syscall"
)

func openReportFile(filename string) (*os.File, error) {
	return os.OpenFile(filename, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}
