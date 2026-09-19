//go:build !unix

package cli

import "os"

func openReportFile(filename string) (*os.File, error) {
	if err := requireRegularReport(filename); err != nil {
		return nil, err
	}
	return os.Open(filename)
}
