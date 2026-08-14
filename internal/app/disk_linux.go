//go:build linux

package app

import (
	"bufio"
	"os"
	"strings"
	"syscall"
)

// diskUsage returns the total and used bytes summed over every local disk
// filesystem visible in /proc/self/mounts (each device counted once),
// falling back to the single filesystem holding path.
func diskUsage(path string) (total, used int64, err error) {
	f, err := os.Open("/proc/self/mounts")
	if err != nil {
		return statfs(path)
	}
	defer f.Close()
	seen := make(map[string]bool)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 {
			continue
		}
		dev, mnt, fstype := f[0], f[1], f[2]
		if !diskFSTypes[fstype] || seen[dev] {
			continue
		}
		seen[dev] = true
		t, u, err := statfs(mnt)
		if err != nil {
			continue
		}
		total += t
		used += u
	}
	if total == 0 {
		return statfs(path)
	}
	return total, used, nil
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
