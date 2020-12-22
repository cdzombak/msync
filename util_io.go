package main

import (
	"io"
	"os"

	"golang.org/x/term"
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

// IsStdoutInteractive returns true iff standard output is an interactive terminal.
func IsStdoutInteractive() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}
