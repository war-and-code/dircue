//go:build !unix

package scanner

import "os"

func openRegular(root *os.Root, filename string) (*os.File, error) {
	return root.Open(filename)
}
