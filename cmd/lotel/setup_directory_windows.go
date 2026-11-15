package main

import "errors"

type setupDirectory struct{}

func openSetupDirectory(path string) (*setupDirectory, error) {
	return nil, errors.New("setup recommendations are not supported on Windows")
}

func (d *setupDirectory) Close() error { return nil }

func (d *setupDirectory) lookup(name string) (setupEntry, error) {
	return setupEntry{}, errors.New("setup recommendations are not supported on Windows")
}
