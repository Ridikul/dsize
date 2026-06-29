//go:build windows

package scan

import "io/fs"

// fileID has no portable inode equivalent on Windows; de-duplication is
// disabled there (ok == false).
func fileID(info fs.FileInfo) (dev uint64, ino uint64, ok bool) {
	return 0, 0, false
}

// diskUsage falls back to the logical size on Windows, which exposes no
// portable allocated-block count through fs.FileInfo.
func diskUsage(info fs.FileInfo) int64 {
	return info.Size()
}
