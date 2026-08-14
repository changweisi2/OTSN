//go:build !windows

package ui

// enableVT is a no-op on Unix: terminals handle ANSI natively.
func enableVT() {}
