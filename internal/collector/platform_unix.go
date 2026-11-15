//go:build !windows

package collector

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

func nativeSupported() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "linux"
}

func configureNativeProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func lockLifecycleFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
}

func unlockLifecycleFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
