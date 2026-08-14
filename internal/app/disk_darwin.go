//go:build darwin

package app

import "golang.org/x/sys/unix"

// diskUsage returns the total and used bytes summed over every local disk
// filesystem (each device counted once), falling back to the single
// filesystem holding path.
func diskUsage(path string) (total, used int64, err error) {
	stats, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil {
		return statfs(path)
	}
	seen := make(map[unix.Fsid]bool)
	for _, s := range stats {
		fst := string(s.Fstypename[:])
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
