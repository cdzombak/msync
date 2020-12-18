package main

import (
	"os/exec"
	"strings"
)

// Exec finds the executable with the given name in the path, runs it, and returns its output.
func Exec(executable string, args []string) (string, error) {
	path, err := exec.LookPath(executable)
	if err != nil {
		return "", err
	}
	raw, err := exec.Command(path, args...).CombinedOutput()
	if err != nil {
		return "", err
	} else {
		return strings.TrimSpace(string(raw)), nil
	}
}
