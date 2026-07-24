package dzutil

import (
	"io"
	"os"
	"time"
)

// CopyFile copies the file at `from` to the path `to`, creating `to` with the
// given permissions and setting its modification time to sourceModTime.
func CopyFile(from, to string, mode os.FileMode, sourceModTime time.Time) error {
	fromFile, err := os.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = fromFile.Close() }()

	toFile, err := os.OpenFile(to, os.O_RDWR|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() { _ = toFile.Close() }()

	_, err = io.Copy(toFile, fromFile)
	if err != nil {
		return err
	}

	// Preserve the source file's modification time
	return os.Chtimes(to, time.Now(), sourceModTime)
}
