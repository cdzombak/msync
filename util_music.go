package main

import (
	"path/filepath"
	"strings"
)

const transcodedFileExt = ".m4a"

func isMusicFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	// could also add m3a, mp4 but my library doesn't have these
	// TODO(cdzombak): allow customizing music extensions
	return ext == ".mp3" || ext == ".m4a" || ext == ".flac" || ext == ".alac"
}

func normalizeFileNameForComparing(name string) string {
	name = strings.ToLower(name)
	if isMusicFile(name) {
		name = removeExt(name)
	}
	return name
}

func removeExt(name string) string {
	ext := filepath.Ext(name)
	return name[0 : len(name)-len(ext)]
}
