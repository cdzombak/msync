package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// Exec finds the executable with the given name in the path, runs it, and returns its output.
func Exec(executable string, args []string) (string, error) {
	path, err := exec.LookPath(executable)
	if err != nil {
		return fmt.Sprintf("command not found: %s", executable), err
	}
	raw, err := exec.Command(path, args...).CombinedOutput()
	return strings.TrimSpace(string(raw)), err
}
