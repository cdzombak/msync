package main

import (
	"path/filepath"
	"strings"
)

func isMusicFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	// could also add m3a, mp4 but my library doesn't have these
	return ext == ".mp3" || ext == ".m4a" || ext == ".flac" || ext == ".alac"
}

func normalizeFileNameForComparing(name string) string {
	name = strings.ToLower(name)
	if isMusicFile(name) {
		ext := filepath.Ext(name)
		name = name[0 : len(name)-len(ext)]
	}
	return name
}
