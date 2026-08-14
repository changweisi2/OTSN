//go:build !windows

package app

import "syscall"

// diskUsage returns the total and used bytes of the filesystem containing
// path, matching what df reports for that mount point.
func diskUsage(path string) (total, used int64, err error) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return 0, 0, err
	}
	bsize := int64(fs.Bsize)
	total = int64(fs.Blocks) * bsize
	used = int64(fs.Blocks-fs.Bfree) * bsize
	return total, used, nil
}
