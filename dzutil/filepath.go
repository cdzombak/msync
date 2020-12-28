package dzutil

import "path/filepath"

// RemoveExt removes the extension, if any, from the given path/filename.
func RemoveExt(name string) string {
	ext := filepath.Ext(name)
	return name[0 : len(name)-len(ext)]
}
