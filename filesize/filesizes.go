package filesize

import "fmt"

// based on https://yourbasic.org/golang/formatting-byte-size-to-human-readable-format/

// ByteCountBothStyles returns a formatted string for the given byte count in both
// SI (base 10) and IEC (base 2) styles.
func ByteCountBothStyles(b int64) string {
	if b == 0 {
		return "0 B"
	}
	return fmt.Sprintf("%s (%s)", ByteCountSI(b), ByteCountIEC(b))
}

// ByteCountSI returns a formatted string for the given byte count, assuming
// SI (base 10) units.
func ByteCountSI(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB",
		float64(b)/float64(div), "kMGTPE"[exp])
}

// ByteCountIEC returns a formatted string for the given byte count, assuming
// IEC (base 2) units.
func ByteCountIEC(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB",
		float64(b)/float64(div), "KMGTPE"[exp])
}
