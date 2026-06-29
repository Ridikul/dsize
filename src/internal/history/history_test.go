package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// overrideHistoryDir temporarily redirects HistoryDir to a temp dir.
func withTempHistoryDir(t *testing.T, fn func(dir string)) {
	t.Helper()
	dir := t.TempDir()
	// Monkey-patch by setting an env var that historyDirOverride reads.
	t.Setenv("DSIZE_HISTORY_DIR_TEST", dir)
	fn(dir)
}

// HistoryDir is patched for tests by checking the env var.
// NOTE: we can't monkey-patch the real HistoryDir easily without an
// interface, so tests call the internal helper directly via an exported shim.

// testHistoryDir returns the test override if set, else the real dir.
func testHistoryDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("DSIZE_HISTORY_DIR_TEST")
	if dir == "" {
		t.Skip("DSIZE_HISTORY_DIR_TEST not set")
	}
	return dir
}

func TestSnapshotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DSIZE_HISTORY_DIR_TEST", dir)

	snap := Snapshot{
		Version:    1,
		ScannedAt:  time.Now().UTC().Format(time.RFC3339),
		Target:     "/tmp/testdir",
		TotalSize:  1234567,
		FileCount:  42,
		DirCount:   7,
		DurationMs: 321,
		Entries: []Entry{
			{Path: "/tmp/testdir/a.txt", Size: 1000, IsDir: false},
			{Path: "/tmp/testdir/subdir", Size: 234567, IsDir: true},
		},
	}

	// Patch HistoryDir to return dir.
	origFn := historyDirFn
	historyDirFn = func() (string, error) { return dir, nil }
	defer func() { historyDirFn = origFn }()

	if err := Write(snap); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Verify a .json file was created.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
	fname := entries[0].Name()
	if filepath.Ext(fname) != ".json" {
		t.Fatalf("expected .json, got %s", fname)
	}

	// Read back.
	got, err := ReadSnapshot(fname)
	if err != nil {
		t.Fatalf("ReadSnapshot: %v", err)
	}

	if got.Version != snap.Version      { t.Errorf("Version: got %d want %d", got.Version, snap.Version) }
	if got.Target != snap.Target        { t.Errorf("Target: got %q want %q", got.Target, snap.Target) }
	if got.TotalSize != snap.TotalSize  { t.Errorf("TotalSize: got %d want %d", got.TotalSize, snap.TotalSize) }
	if got.FileCount != snap.FileCount  { t.Errorf("FileCount: got %d want %d", got.FileCount, snap.FileCount) }
	if len(got.Entries) != len(snap.Entries) {
		t.Errorf("Entries len: got %d want %d", len(got.Entries), len(snap.Entries))
	}
}

func TestListSummaries(t *testing.T) {
	dir := t.TempDir()
	origFn := historyDirFn
	historyDirFn = func() (string, error) { return dir, nil }
	defer func() { historyDirFn = origFn }()

	target := "/home/user/docs"
	for i := 0; i < 3; i++ {
		snap := Snapshot{
			Version:   1,
			ScannedAt: time.Now().Add(time.Duration(i) * time.Hour).UTC().Format(time.RFC3339),
			Target:    target,
			TotalSize: int64(1000 * (i + 1)),
		}
		if err := Write(snap); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
		// Sleep 1ms so timestamps differ (test writes happen fast).
		time.Sleep(time.Millisecond)
	}

	summaries, err := ListSummaries(target)
	if err != nil {
		t.Fatalf("ListSummaries: %v", err)
	}
	if len(summaries) != 3 {
		t.Fatalf("expected 3 summaries, got %d", len(summaries))
	}
	// Should be ascending by scannedAt.
	for i := 1; i < len(summaries); i++ {
		if summaries[i].ScannedAt < summaries[i-1].ScannedAt {
			t.Errorf("summaries not ascending: [%d]=%s < [%d]=%s",
				i, summaries[i].ScannedAt, i-1, summaries[i-1].ScannedAt)
		}
	}
}

func TestListSummaries_DifferentTarget(t *testing.T) {
	dir := t.TempDir()
	origFn := historyDirFn
	historyDirFn = func() (string, error) { return dir, nil }
	defer func() { historyDirFn = origFn }()

	targetA := "/home/alice"
	targetB := "/home/bob"

	for _, tgt := range []string{targetA, targetB} {
		Write(Snapshot{Version: 1, ScannedAt: time.Now().UTC().Format(time.RFC3339), Target: tgt})
		time.Sleep(time.Millisecond)
	}

	sums, _ := ListSummaries(targetA)
	if len(sums) != 1 {
		t.Fatalf("expected 1 summary for targetA, got %d", len(sums))
	}
	if sums[0].Target != targetA {
		t.Errorf("wrong target: %s", sums[0].Target)
	}
}

func TestReadSnapshot_TraversalRejected(t *testing.T) {
	dir := t.TempDir()
	origFn := historyDirFn
	historyDirFn = func() (string, error) { return dir, nil }
	defer func() { historyDirFn = origFn }()

	for _, bad := range []string{"../../etc/passwd", "../outside.json", "/absolute.json"} {
		_, err := ReadSnapshot(bad)
		if err == nil {
			t.Errorf("ReadSnapshot(%q): expected error, got nil", bad)
		}
	}
}

func TestComputeTrends(t *testing.T) {
	prev := []Entry{
		{Path: "/a", Size: 100},
		{Path: "/b", Size: 200},
		{Path: "/c", Size: 300},
	}
	curr := []Entry{
		{Path: "/a", Size: 150},  // grew
		{Path: "/b", Size: 200},  // same
		{Path: "/c", Size: 50},   // shrunk
		{Path: "/d", Size: 999},  // new
	}

	result := ComputeTrends(curr, prev)
	want := map[string]string{"/a": "up", "/b": "equal", "/c": "down", "/d": "new"}
	for _, e := range result {
		if e.Trend != want[e.Path] {
			t.Errorf("path %s: trend %q, want %q", e.Path, e.Trend, want[e.Path])
		}
	}
}

func TestHistoryDirResolution(t *testing.T) {
	// Just verify it returns a non-empty path without error.
	dir, err := HistoryDir()
	if err != nil {
		t.Fatalf("HistoryDir: %v", err)
	}
	if dir == "" {
		t.Fatal("HistoryDir returned empty string")
	}
}

func TestWriteFailureIsNonFatal(t *testing.T) {
	// Point to a read-only directory; Write should return an error, not panic.
	origFn := historyDirFn
	historyDirFn = func() (string, error) { return "/nonexistent/readonly/path", nil }
	defer func() { historyDirFn = origFn }()

	snap := Snapshot{
		Version:   1,
		ScannedAt: time.Now().UTC().Format(time.RFC3339),
		Target:    "/tmp",
	}
	err := Write(snap)
	// We expect an error (can't create dir), not a panic.
	if err == nil {
		t.Log("Write unexpectedly succeeded (maybe running as root)")
	}
}
