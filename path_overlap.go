package main

import (
	"path/filepath"
	"strings"
)

func pathsOverlap(pathA, pathB string) bool {
	a, err := filepath.Abs(pathA)
	if err != nil {
		a = filepath.Clean(pathA)
	}
	b, err := filepath.Abs(pathB)
	if err != nil {
		b = filepath.Clean(pathB)
	}
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	return isSubpath(a, b) || isSubpath(b, a)
}

func isSubpath(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	if rel == "." {
		return true
	}
	if rel == ".." {
		return false
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}
