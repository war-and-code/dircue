//go:build !unix && !windows

package cli

import (
	"fmt"
	"os"
)

func openInputFile(filename, label string) (*os.File, error) {
	if err := requireRegularInput(filename, label); err != nil {
		return nil, err
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("%s must be a regular file", label)
	}
	return file, nil
}
