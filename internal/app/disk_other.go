//go:build !linux && !darwin && !windows

package app

import "syscall"

// diskUsage falls back to the single filesystem holding path on platforms
// without a mount-table enumeration here.
func diskUsage(path string) (total, used int64, err error) {
	return statfs(path)
}

func statfs(path string) (total, used int64, err error) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return 0, 0, err
	}
	bsize := int64(fs.Bsize)
	total = int64(fs.Blocks) * bsize
	used = int64(fs.Blocks-fs.Bfree) * bsize
	return total, used, nil
}
