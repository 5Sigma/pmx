package ui

import "fmt"

// FormatBytes formats a byte count as a human-readable string (B / KB / MB).
func FormatBytes(n int64) string {
	const (
		kb = 1024
		mb = 1024 * 1024
	)
	switch {
	case n < kb:
		return fmt.Sprintf("%d B", n)
	case n < mb:
		return fmt.Sprintf("%d KB", (n+kb/2)/kb)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	}
}
