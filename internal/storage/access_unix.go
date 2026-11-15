//go:build !windows

package storage

import (
	"errors"
	"os"
	"syscall"
)

func tryDatabaseLock(file *os.File) (bool, error) {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}
func releaseDatabaseLock(file *os.File) { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }
