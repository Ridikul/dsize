// Package scan implements the disk-usage walk engine.
// It is used by both the CLI output path and the UI SSE path.
package scan

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Options controls scan behaviour (mirrors the existing CLI flags).
type Options struct {
	MaxDepth int  // 0 = unlimited
	TopN     int  // 0 = all
	Bytes    bool // if true, human-readable uses raw bytes
	// CrossMounts, when true, descends into directories that live on a
	// different filesystem than the scan root. The default (false) stays on
	// the root's filesystem, like `du -x`. This matters on macOS where "/"
	// has separate mounted volumes for swap (/System/Volumes/VM), Preboot,
	// Update, etc. that would otherwise inflate the total well beyond the
	// disk space actually used by the volume.
	CrossMounts bool
	// ApparentSize, when true, sums logical file sizes (st_size) instead of
	// the on-disk allocated size. The default (false) reports real disk
	// usage like `du`, which is much smaller for the compressed/cloned files
	// that fill a macOS system volume.
	ApparentSize bool
}

// Entry is a single filesystem entry returned in the scan result.
type Entry struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	IsDir bool   `json:"isDir"`
	// Trend is set by the history package after diff; not populated here.
	Trend string `json:"trend,omitempty"`
}

// Progress is emitted periodically during the scan.
type Progress struct {
	CurrentPath string
	TotalSize   int64
	FileCount   int64
	DirCount    int64
	ElapsedMs   int64
	FilesPerSec float64
	ErrorCount  int64
}

// Result is returned when the scan completes.
type Result struct {
	Entries     []Entry
	TotalSize   int64
	FileCount   int64
	DirCount    int64
	DurationMs  int64
	ErrorCount  int64
}

// ScanStream walks root, calling onProgress at most every 100 ms.
// It respects ctx cancellation.  The returned Result contains entries
// sorted descending by size, trimmed to opts.TopN if set.
func ScanStream(ctx context.Context, root string, opts Options, onProgress func(Progress)) (Result, error) {
	start := time.Now()

	// dirSizes accumulates sizes bottom-up so directory entries reflect
	// the total size of their contents, not just the inode itself.
	dirSizes := map[string]int64{}

	var totalSize, fileCount, dirCount, errorCount int64

	// First pass: walk and collect all individual entry sizes.
	// We record every file/dir path and its raw (non-recursive) size,
	// then propagate upward.
	type raw struct {
		path  string
		size  int64
		isDir bool
		depth int
	}
	var raws []raw

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Result{}, err
	}

	lastEmit := time.Now()
	var currentPath string
	var localFiles, localDirs, localErrors int64

	// seen de-duplicates by (device, inode) so hardlinks and macOS firmlinks
	// are counted once. Without this, walking "/" double-counts the data
	// volume, which is firmlinked at both /Users (etc.) and
	// /System/Volumes/Data/Users.
	seen := map[[2]uint64]bool{}

	// rootDev is the filesystem of the scan root; entries on other
	// filesystems are skipped unless opts.CrossMounts is set.
	var rootDev uint64
	var haveRootDev bool
	if fi, statErr := os.Lstat(absRoot); statErr == nil {
		if dev, _, ok := fileID(fi); ok {
			rootDev, haveRootDev = dev, true
		}
	}

	err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, werr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if werr != nil {
			localErrors++
			return nil // count and continue
		}

		// Compute depth relative to root.
		rel, _ := filepath.Rel(absRoot, path)
		depth := 0
		if rel != "." {
			depth = len(filepath.SplitList(filepath.ToSlash(rel))) // rough depth
			// Use separator count for accuracy.
			depth = countSlashes(rel) + 1
		}

		if opts.MaxDepth > 0 && depth > opts.MaxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, infoErr := d.Info()
		var size int64
		if infoErr == nil {
			if opts.ApparentSize {
				size = info.Size()
			} else {
				size = diskUsage(info)
			}
		}

		// Skip entries on a different filesystem than the root (du -x),
		// unless the caller opted into crossing mounts. Keeps swap, Preboot,
		// Update and other mounted volumes out of a "/" scan.
		if !opts.CrossMounts && haveRootDev && infoErr == nil && path != absRoot {
			if dev, _, ok := fileID(info); ok && dev != rootDev {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Skip entries already counted under another path (hardlinks,
		// macOS firmlinks). For a duplicate directory, skip its whole
		// subtree to avoid re-walking the data volume.
		if infoErr == nil {
			if dev, ino, ok := fileID(info); ok {
				key := [2]uint64{dev, ino}
				if seen[key] {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				seen[key] = true
			}
		}

		currentPath = path

		if d.IsDir() {
			localDirs++
			dirSizes[path] = 0 // initialise so propagation works
		} else {
			localFiles++
			// Propagate file size up to all ancestor dirs within root.
			propagate(dirSizes, absRoot, path, size)
		}

		raws = append(raws, raw{path: path, size: size, isDir: d.IsDir(), depth: depth})

		// Emit progress at most every 100 ms.
		if time.Since(lastEmit) >= 100*time.Millisecond && onProgress != nil {
			elapsed := time.Since(start)
			fps := 0.0
			if elapsed.Seconds() > 0 {
				fps = float64(localFiles) / elapsed.Seconds()
			}
			// Running total uses file sizes only; dirs get updated after walk.
			var runSize int64
			for _, r := range raws {
				if !r.isDir {
					runSize += r.size
				}
			}
			onProgress(Progress{
				CurrentPath: currentPath,
				TotalSize:   runSize,
				FileCount:   localFiles,
				DirCount:    localDirs,
				ElapsedMs:   elapsed.Milliseconds(),
				FilesPerSec: fps,
				ErrorCount:  localErrors,
			})
			lastEmit = time.Now()
		}

		return nil
	})
	if err != nil && ctx.Err() != nil {
		return Result{}, ctx.Err()
	}

	errorCount = localErrors

	// Build final entries: for each raw entry at depth == 1 (direct children
	// of root) plus the root itself, use accumulated dir sizes.
	// Per design: entries are the top-level children of root (like du -d1).
	var entries []Entry
	for _, r := range raws {
		if r.path == absRoot {
			continue // skip root itself
		}
		// Only include direct children (depth 1).
		if r.depth != 1 {
			continue
		}
		sz := r.size
		if r.isDir {
			sz = dirSizes[r.path]
		}
		totalSize += sz
		if r.isDir {
			dirCount++
		} else {
			fileCount++
		}
		entries = append(entries, Entry{
			Path:  r.path,
			Size:  sz,
			IsDir: r.isDir,
		})
	}

	// Sort descending by size.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Size > entries[j].Size
	})

	if opts.TopN > 0 && len(entries) > opts.TopN {
		entries = entries[:opts.TopN]
	}

	duration := time.Since(start)
	return Result{
		Entries:    entries,
		TotalSize:  totalSize,
		FileCount:  fileCount,
		DirCount:   dirCount,
		DurationMs: duration.Milliseconds(),
		ErrorCount: errorCount,
	}, nil
}

// propagate adds size to all ancestor directories of path that are under root.
func propagate(dirSizes map[string]int64, root, path string, size int64) {
	dir := filepath.Dir(path)
	for {
		if _, ok := dirSizes[dir]; ok {
			dirSizes[dir] += size
		}
		if dir == root || dir == filepath.Dir(dir) {
			break
		}
		dir = filepath.Dir(dir)
	}
}

func countSlashes(rel string) int {
	count := 0
	for _, c := range rel {
		if c == filepath.Separator {
			count++
		}
	}
	return count
}

// FormatSize returns a human-readable IEC size string.
func FormatSize(bytes int64, raw bool) string {
	if raw {
		return formatInt(bytes) + " B"
	}
	const (
		KiB = 1024
		MiB = 1024 * KiB
		GiB = 1024 * MiB
		TiB = 1024 * GiB
	)
	switch {
	case bytes >= TiB:
		return formatFloat(float64(bytes)/TiB) + " TiB"
	case bytes >= GiB:
		return formatFloat(float64(bytes)/GiB) + " GiB"
	case bytes >= MiB:
		return formatFloat(float64(bytes)/MiB) + " MiB"
	case bytes >= KiB:
		return formatFloat(float64(bytes)/KiB) + " KiB"
	default:
		return formatInt(bytes) + " B"
	}
}

func formatFloat(f float64) string {
	// Simple 1-decimal formatting without fmt to avoid import cycles.
	i := int64(f * 10)
	whole := i / 10
	frac := i % 10
	return formatInt(whole) + "." + string(rune('0'+frac))
}

func formatInt(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
