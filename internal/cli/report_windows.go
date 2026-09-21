//go:build windows

package cli

import (
	"fmt"
	"golang.org/x/sys/windows"
	"os"
)

func openInputFile(filename, label string) (*os.File, error) {
	if err := requireRegularInput(filename, label); err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(filename)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	var info windows.ByHandleFileInformation
	err = windows.GetFileInformationByHandle(handle, &info)
	if err != nil || info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("%s must be a regular file without reparse points", label)
	}
	file := os.NewFile(uintptr(handle), filename)
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("%s must be a regular file", label)
	}
	return file, nil
}
