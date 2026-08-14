//go:build windows

package app

import "golang.org/x/sys/windows"

// defaultRoots returns every fixed or removable drive letter.
func defaultRoots() []string {
	bits, err := windows.GetLogicalDrives()
	if err != nil {
		return []string{`C:\`}
	}
	var roots []string
	for i := 0; i < 26; i++ {
		if bits&(1<<uint(i)) == 0 {
			continue
		}
		letter := string(rune('A'+i)) + `:\`
		if p, err := windows.UTF16PtrFromString(letter); err == nil {
			switch windows.GetDriveType(p) {
			case windows.DRIVE_FIXED, windows.DRIVE_REMOVABLE:
				roots = append(roots, letter)
			}
		}
	}
	if len(roots) == 0 {
		return []string{`C:\`}
	}
	return roots
}
