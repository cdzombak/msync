package main

import (
	"flag"
	"fmt"
	"os"

	"golang.org/x/term"
)

var (
	verboseFlag = flag.Bool("verbose", false, "Log detailed output to stderr. Suppresses progress bars.")
)

// IsStdoutInteractive returns true iff standard output is an interactive terminal.
func IsStdoutInteractive() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// PrintWarnings print each of the given strings as its own line to stdout, with a "[warning]" prefix.
func PrintWarnings(warnings []string) {
	for _, w := range warnings {
		fmt.Printf("[warning] %s\n", w)
	}
}

func useProgressIndicators() bool {
	return !*verboseFlag && IsStdoutInteractive()
}
