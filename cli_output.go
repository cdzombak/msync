package main

import (
	"fmt"
)

// LogWarnings logs each of the given strings as its own line, with a "[warning]" prefix.
func LogWarnings(warnings []string) {
	for _, w := range warnings {
		fmt.Printf("[warning] %s\n", w)
	}
}
