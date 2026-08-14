//go:build !windows

package app

// defaultRoots returns what to scan when no paths are given.
func defaultRoots() []string {
	return []string{"/"}
}
