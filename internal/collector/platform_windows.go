package collector

import (
	"os"
	"os/exec"

	"golang.org/x/sys/windows"
)

func nativeSupported() bool {
	return false
}

func configureNativeProcess(command *exec.Cmd) {}

func lockLifecycleFile(file *os.File) error {
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &windows.Overlapped{})
}

func unlockLifecycleFile(file *os.File) error {
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &windows.Overlapped{})
}
