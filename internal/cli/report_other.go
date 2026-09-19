//go:build !unix

package cli

import "os"

func openInputFile(filename, label string) (*os.File, error) {
	if err := requireRegularInput(filename, label); err != nil {
		return nil, err
	}
	return os.Open(filename)
}
