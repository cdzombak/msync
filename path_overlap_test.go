package main

import (
	"path/filepath"
	"testing"
)

func TestPathsOverlap(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "music")
	dest := filepath.Join(base, "music2")
	srcChild := filepath.Join(src, "subdir")
	destChild := filepath.Join(dest, "subdir")

	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{name: "same path", a: src, b: src, want: true},
		{name: "b under a", a: src, b: srcChild, want: true},
		{name: "a under b", a: srcChild, b: src, want: true},
		{name: "siblings", a: src, b: dest, want: false},
		{name: "music vs music2", a: filepath.Join(string(filepath.Separator), "music"), b: filepath.Join(string(filepath.Separator), "music2"), want: false},
		{name: "cousins", a: src, b: destChild, want: false},
		{name: "trailing separator", a: src + string(filepath.Separator), b: src, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pathsOverlap(tt.a, tt.b)
			if got != tt.want {
				t.Fatalf("pathsOverlap(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
