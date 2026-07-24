package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"

	"msync/dzutil"
)

const bitrateExecutable = "afinfo"

var bitrateRegex = regexp.MustCompile("bit rate: (\\d+) bits per second")

// checkBitrateToolAvailable returns an error iff the tool used to read file bitrates is unavailable.
// This lets msync fail fast on a broken environment, so that a failure to read any individual file's
// bitrate can be treated as a problem with that file rather than with the environment.
func checkBitrateToolAvailable() error {
	if _, err := exec.LookPath(bitrateExecutable); err != nil {
		return fmt.Errorf("%s is required but was not found in $PATH: %w", bitrateExecutable, err)
	}
	return nil
}

// fileBitrate returns the bitrate of the file at the given path, as determined by macOS's afinfo command.
// An error is returned if afinfo cannot be found, returns a nonzero exit code, or
// produces no or un-parsable output.
func fileBitrate(path string) (int, error) {
	out, err := dzutil.Exec(bitrateExecutable, []string{path})
	if err != nil {
		return 0, fmt.Errorf("could not run afinfo to get bitrate for '%s': %w", path, err)
	}
	if out == "" {
		return 0, fmt.Errorf("afinfo returned no output for '%s'", path)
	}
	matches := bitrateRegex.FindStringSubmatch(out)
	if len(matches) < 2 {
		return 0, fmt.Errorf("failed to parse output from afinfo for '%s'", path)
	}
	bitrate, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, fmt.Errorf("failed to parse bitrate '%s' from afinfo for '%s'", matches[1], path)
	}
	return bitrate, nil
}
