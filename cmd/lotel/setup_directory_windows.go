package main

import "os"

type setupDirectory struct{ root *os.Root }

func openSetupDirectory(path string) (*setupDirectory, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	return &setupDirectory{root: root}, nil
}

func (d *setupDirectory) Close() error { return d.root.Close() }

func (d *setupDirectory) lookup(name string) (setupEntry, error) {
	info, err := d.root.Lstat(name)
	if err != nil {
		return setupEntry{}, err
	}
	mode := info.Mode()
	return setupEntry{symlink: mode&os.ModeSymlink != 0, regular: mode.IsRegular(), readableBits: mode.Perm()&0444 != 0}, nil
}
