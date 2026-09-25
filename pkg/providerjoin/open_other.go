//go:build !unix

package providerjoin

import "os"

func openReportFile(filename string) (*os.File, error) { return os.Open(filename) }
