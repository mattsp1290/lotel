//go:build windows

package storage

import (
	"os"
	"syscall"
)

func openExistingDiagnosticFile(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	var info syscall.ByHandleFileInformation
	kind, e := syscall.GetFileType(handle)
	e2 := syscall.GetFileInformationByHandle(handle, &info)
	if e != nil || e2 != nil || kind != syscall.FILE_TYPE_DISK || info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		syscall.CloseHandle(handle)
		return nil, os.ErrInvalid
	}
	return os.NewFile(uintptr(handle), path), nil
}
