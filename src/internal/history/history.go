// Package history manages snapshot persistence and trend computation.
package history

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Entry mirrors scan.Entry but lives here to avoid circular imports.
type Entry struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	IsDir bool   `json:"isDir"`
	// Trend is not persisted; it is computed on-the-fly.
	Trend string `json:"trend,omitempty"`
}

// Snapshot is the persisted JSON schema (FR-7.3).
type Snapshot struct {
	Version   int     `json:"version"`
	ScannedAt string  `json:"scannedAt"` // RFC3339
	Target    string  `json:"target"`
	TotalSize int64   `json:"totalSize"`
	FileCount int64   `json:"fileCount"`
	DirCount  int64   `json:"dirCount"`
	DurationMs int64  `json:"durationMs"`
	Entries   []Entry `json:"entries"`
}

// Summary is like Snapshot but without Entries, used for /history responses.
type Summary struct {
	Version    int    `json:"version"`
	ScannedAt  string `json:"scannedAt"`
	Target     string `json:"target"`
	TotalSize  int64  `json:"totalSize"`
	FileCount  int64  `json:"fileCount"`
	DirCount   int64  `json:"dirCount"`
	DurationMs int64  `json:"durationMs"`
	Filename   string `json:"filename"` // bare filename for drill-down
}

// historyDirFn is the function used to resolve the history directory.
// Tests override this to point at a temp dir.
var historyDirFn = defaultHistoryDir

// HistoryDirFnForTest returns the current resolver; used by tests in other packages.
func HistoryDirFnForTest() func() (string, error) { return historyDirFn }

// SetHistoryDirFnForTest replaces the resolver; used by tests in other packages.
func SetHistoryDirFnForTest(fn func() (string, error)) { historyDirFn = fn }

// HistoryDir returns the OS-appropriate history directory (FR-7.1).
func HistoryDir() (string, error) { return historyDirFn() }

func defaultHistoryDir() (string, error) {
	var base string
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "Library", "Application Support", "dsize", "history")
	case "windows":
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return "", fmt.Errorf("%%APPDATA%% not set")
		}
		base = filepath.Join(appdata, "dsize", "history")
	default: // Linux and others
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		xdg := os.Getenv("XDG_DATA_HOME")
		if xdg == "" {
			xdg = filepath.Join(home, ".local", "share")
		}
		base = filepath.Join(xdg, "dsize", "history")
	}
	return base, nil
}

// ensureDir creates the history directory with mode 0700 if absent (FR-7.4).
func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0700)
}

// snapshotFilename encodes the scan time and target path into a safe filename (FR-7.2).
// Format: <ISO8601 with ':'→'-'>_<base64url(target)>.json
func snapshotFilename(scannedAt time.Time, target string) string {
	ts := scannedAt.UTC().Format("2006-01-02T15-04-05Z")
	encoded := base64.RawURLEncoding.EncodeToString([]byte(target))
	return ts + "_" + encoded + ".json"
}

// targetFromFilename decodes the base64url target from a snapshot filename.
func targetFromFilename(name string) (string, error) {
	// name is like "2026-06-27T09-17-58Z_L2hvbWUvdXNlcg.json"
	base := strings.TrimSuffix(name, ".json")
	parts := strings.SplitN(base, "_", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("unexpected filename format: %s", name)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

// Write atomically persists a Snapshot (FR-7.5).
// It never returns a fatal error — callers should log and continue (FR-7.6).
func Write(s Snapshot) error {
	dir, err := HistoryDir()
	if err != nil {
		return fmt.Errorf("history dir: %w", err)
	}
	if err := ensureDir(dir); err != nil {
		return fmt.Errorf("create history dir: %w", err)
	}

	t, err := time.Parse(time.RFC3339, s.ScannedAt)
	if err != nil {
		t = time.Now().UTC()
	}
	fname := snapshotFilename(t, s.Target)

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}

	// Write to temp file in same directory then rename (atomic on same FS).
	tmp, err := os.CreateTemp(dir, ".tmp-snapshot-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp: %w", err)
	}

	dest := filepath.Join(dir, fname)
	if err := os.Rename(tmpName, dest); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename temp: %w", err)
	}
	return nil
}

// ListSummaries returns snapshot summaries for the given target, ascending by scannedAt (FR-8.4).
func ListSummaries(target string) ([]Summary, error) {
	dir, err := HistoryDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var summaries []Summary
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		// Filter by target via filename decode to avoid reading every file.
		t, err := targetFromFilename(e.Name())
		if err != nil || t != target {
			continue
		}

		s, err := readSnapshotFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue // skip corrupt files
		}
		summaries = append(summaries, Summary{
			Version:    s.Version,
			ScannedAt:  s.ScannedAt,
			Target:     s.Target,
			TotalSize:  s.TotalSize,
			FileCount:  s.FileCount,
			DirCount:   s.DirCount,
			DurationMs: s.DurationMs,
			Filename:   e.Name(),
		})
	}

	// Sort ascending by scannedAt.
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].ScannedAt < summaries[j].ScannedAt
	})
	return summaries, nil
}

// ReadSnapshot reads a full snapshot by bare filename (no path components allowed).
// Returns an error if filename contains path separators or ".." (NFR-2.2).
func ReadSnapshot(filename string) (Snapshot, error) {
	// Security: reject any path traversal.
	if strings.Contains(filename, "/") || strings.Contains(filename, "\\") ||
		strings.Contains(filename, "..") {
		return Snapshot{}, fmt.Errorf("invalid filename: path traversal not allowed")
	}
	if !strings.HasSuffix(filename, ".json") {
		return Snapshot{}, fmt.Errorf("invalid filename: must end in .json")
	}

	dir, err := HistoryDir()
	if err != nil {
		return Snapshot{}, err
	}

	fullPath := filepath.Join(dir, filename)
	// Double-check the cleaned path stays within the history dir.
	cleaned := filepath.Clean(fullPath)
	if !strings.HasPrefix(cleaned, filepath.Clean(dir)+string(filepath.Separator)) {
		return Snapshot{}, fmt.Errorf("path traversal detected")
	}

	return readSnapshotFile(fullPath)
}

func readSnapshotFile(path string) (Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, err
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return Snapshot{}, err
	}
	return s, nil
}

// LatestSnapshot returns the most recent snapshot for target, if any.
func LatestSnapshot(target string) (*Snapshot, error) {
	summaries, err := ListSummaries(target)
	if err != nil || len(summaries) == 0 {
		return nil, err
	}
	// summaries is ascending; last is most recent.
	last := summaries[len(summaries)-1]
	s, err := ReadSnapshot(last.Filename)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ComputeTrends sets the Trend field on curr entries by diffing against prev (FR-8.6).
// Entries not in prev are marked "new"; grown → "up"; shrunk → "down"; equal → "equal".
func ComputeTrends(curr []Entry, prev []Entry) []Entry {
	prevMap := make(map[string]int64, len(prev))
	for _, e := range prev {
		prevMap[e.Path] = e.Size
	}

	result := make([]Entry, len(curr))
	for i, e := range curr {
		prevSize, found := prevMap[e.Path]
		switch {
		case !found:
			e.Trend = "new"
		case e.Size > prevSize:
			e.Trend = "up"
		case e.Size < prevSize:
			e.Trend = "down"
		default:
			e.Trend = "equal"
		}
		result[i] = e
	}
	return result
}
