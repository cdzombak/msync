package main

import (
	"fmt"
	"regexp"
	"strconv"

	"msync/dzutil"
)

var bitrateRegex = regexp.MustCompile("bit rate: (\\d+) bits per second")

// fileBitrate returns the bitrate of the file at the given path, as determined by macOS's afinfo command.
// An error is returned if afinfo cannot be found, returns a nonzero exit code, or
// produces no or un-parsable output.
func fileBitrate(path string) (int, error) {
	out, err := dzutil.Exec("afinfo", []string{path})
	if err != nil {
		return 0, fmt.Errorf("could not run afinfo to get bitrate: %w", err)
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
