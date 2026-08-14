//go:build windows

package app

import "golang.org/x/sys/windows"

// diskUsage returns the total and used bytes summed over every fixed or
// removable drive letter, falling back to the single drive holding path.
func diskUsage(path string) (total, used int64, err error) {
	bits, err := windows.GetLogicalDrives()
	if err != nil {
		return driveStats(path)
	}
	for i := 0; i < 26; i++ {
		if bits&(1<<uint(i)) == 0 {
			continue
		}
		letter := string(rune('A'+i)) + `:\`
		p, err := windows.UTF16PtrFromString(letter)
		if err != nil {
			continue
		}
		switch windows.GetDriveType(p) {
		case windows.DRIVE_FIXED, windows.DRIVE_REMOVABLE:
		default:
			continue
		}
		t, u, err := driveStats(letter)
		if err != nil {
			continue
		}
		total += t
		used += u
	}
	if total == 0 {
		return driveStats(path)
	}
	return total, used, nil
}

func driveStats(path string) (total, used int64, err error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var freeAvail, totalBytes, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeAvail, &totalBytes, &totalFree); err != nil {
		return 0, 0, err
	}
	_ = freeAvail
	return int64(totalBytes), int64(totalBytes - totalFree), nil
}
