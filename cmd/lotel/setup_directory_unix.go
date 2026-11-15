//go:build !windows

package main

import "golang.org/x/sys/unix"

type setupDirectory struct{ fd int }

func openSetupDirectory(path string) (*setupDirectory, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return &setupDirectory{fd: fd}, nil
}

func (d *setupDirectory) Close() error { return unix.Close(d.fd) }

func (d *setupDirectory) lookup(name string) (setupEntry, error) {
	var info unix.Stat_t
	if err := unix.Fstatat(d.fd, name, &info, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return setupEntry{}, err
	}
	mode := uint32(info.Mode)
	return setupEntry{symlink: mode&unix.S_IFMT == unix.S_IFLNK, regular: mode&unix.S_IFMT == unix.S_IFREG, readableBits: mode&0444 != 0}, nil
}
