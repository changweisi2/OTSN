//go:build darwin

package app

import (
	"strings"

	"golang.org/x/sys/unix"
)

// diskUsage returns the total and used bytes summed over every local disk
// filesystem (each device counted once), falling back to the single
// filesystem holding path.
func diskUsage(path string) (total, used int64, err error) {
	// Getfsstat(nil, ...) reports how many entries exist; allocate the
	// buffer and fetch them.
	n, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil || n <= 0 {
		return statfs(path)
	}
	stats := make([]unix.Statfs_t, n)
	if _, err := unix.Getfsstat(stats, unix.MNT_NOWAIT); err != nil {
		return statfs(path)
	}
	seen := make(map[unix.Fsid]bool)
	for _, s := range stats {
		// Fstypename is a null-terminated C string in a fixed array;
		// trim trailing NULs or the map lookup below never matches.
		fst := string(s.Fstypename[:])
		if i := strings.IndexByte(fst, 0); i >= 0 {
			fst = fst[:i]
		}
		if !diskFSTypes[fst] || seen[s.Fsid] {
			continue
		}
		seen[s.Fsid] = true
		bsize := int64(s.Bsize)
		total += int64(s.Blocks) * bsize
		used += int64(s.Blocks-s.Bfree) * bsize
	}
	if total == 0 {
		return statfs(path)
	}
	return total, used, nil
}

func statfs(path string) (total, used int64, err error) {
	var fs unix.Statfs_t
	if err := unix.Statfs(path, &fs); err != nil {
		return 0, 0, err
	}
	bsize := int64(fs.Bsize)
	total = int64(fs.Blocks) * bsize
	used = int64(fs.Blocks-fs.Bfree) * bsize
	return total, used, nil
}
