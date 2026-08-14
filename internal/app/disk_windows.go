//go:build windows

package app

import "golang.org/x/sys/windows"

// diskUsage returns the total and used bytes of the filesystem containing
// path.
func diskUsage(path string) (total, used int64, err error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var freeAvail, totalBytes, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeAvail, &totalBytes, &totalFree); err != nil {
		return 0, 0, err
	}
	return int64(totalBytes), int64(totalBytes - totalFree), nil
}
