//go:build !windows

package scan

import (
	"io/fs"
	"syscall"
)

// fileID returns a stable (device, inode) identity used to de-duplicate
// hardlinks and macOS APFS firmlinks during the walk. ok is false when the
// platform does not expose inode metadata.
func fileID(info fs.FileInfo) (dev uint64, ino uint64, ok bool) {
	st, isStat := info.Sys().(*syscall.Stat_t)
	if !isStat {
		return 0, 0, false
	}
	return uint64(st.Dev), uint64(st.Ino), true
}

// diskUsage returns the on-disk size in bytes (allocated 512-byte blocks),
// matching `du`. For compressed or sparse files this is smaller than the
// logical size — significant on macOS, whose system files are heavily
// compressed, so summing logical sizes overstates real disk usage.
func diskUsage(info fs.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int64(st.Blocks) * 512
	}
	return info.Size()
}
