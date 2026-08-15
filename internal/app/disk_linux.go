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
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		// /proc/self/mounts escapes spaces and tabs in paths as \040,
		// \011, ... — reverse the escapes before using the paths.
		dev, mnt, fstype := unescapeMount(fields[0]), unescapeMount(fields[1]), unescapeMount(fields[2])
		if !diskFSTypes[fstype] || seen[dev] {
			continue
		}
		seen[dev] = true
		t, u, statErr := statfs(mnt)
		if statErr != nil {
			continue
		}
		total += t
		used += u
	}
	if err := sc.Err(); err != nil {
		return statfs(path) // incomplete mount list; fall back
	}
	if total == 0 {
		return statfs(path)
	}
	return total, used, nil
}

// unescapeMount reverses the octal escapes used in /proc/self/mounts.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			n, ok := 0, true
			for j := 0; j < 3; j++ {
				c := s[i+1+j]
				if c < '0' || c > '7' {
					ok = false
					break
				}
				n = n*8 + int(c-'0')
			}
			if ok {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
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
