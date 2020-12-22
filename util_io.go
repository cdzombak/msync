package main

import (
	"io"
	"os"
	"path/filepath"
)

// CopyFile copies the file at `from` to the path `to`, creating `to` with the
// given permissions.
func CopyFile(from, to string, mode os.FileMode) error {
	fromFile, err := os.Open(from)
	if err != nil {
		return err
	}
	defer fromFile.Close()

	toFile, err := os.OpenFile(to, os.O_RDWR|os.O_CREATE, mode)
	if err != nil {
		return err
	}
	defer toFile.Close()

	_, err = io.Copy(toFile, fromFile)
	return err
}

// RemoveExt removes the extension, if any, from the given path/filename.
func RemoveExt(name string) string {
	ext := filepath.Ext(name)
	return name[0 : len(name)-len(ext)]
}
