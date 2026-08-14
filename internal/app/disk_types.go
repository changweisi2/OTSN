package app

// diskFSTypes are real on-disk filesystems. Virtual filesystems (tmpfs,
// proc, sysfs, cgroup, ...) and compressed loop mounts (squashfs snaps)
// are excluded so the gauge matches what df reports for local disks.
var diskFSTypes = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true,
	"xfs": true, "btrfs": true, "f2fs": true, "reiserfs": true, "jfs": true,
	"ntfs": true, "ntfs3": true, "exfat": true, "vfat": true, "fuseblk": true,
	"hfsplus": true, "apfs": true, "zfs": true,
}
