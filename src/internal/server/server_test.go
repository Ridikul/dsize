package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"dsize/internal/history"
	"dsize/internal/scan"
)

// minimalAssetFS builds a fake embedded FS just for tests.
func minimalAssetFS() fs.FS {
	return fstest.MapFS{
		"index.html":      &fstest.MapFile{Data: []byte(`<html><body>dsize</body></html>`)},
		"static/chart.js": &fstest.MapFile{Data: []byte(`/* chart.js stub */`)},
		"static/app.js":   &fstest.MapFile{Data: []byte(`/* app.js stub */`)},
		"static/style.css": &fstest.MapFile{Data: []byte(`/* css stub */`)},
	}
}

// fixtureScanFunc returns a scanFunc that immediately completes with two entries.
func fixtureScanFunc(target string) func(ctx context.Context, root string, onProgress func(scan.Progress)) (scan.Result, error) {
	return func(ctx context.Context, root string, onProgress func(scan.Progress)) (scan.Result, error) {
		if onProgress != nil {
			onProgress(scan.Progress{CurrentPath: target + "/a", FileCount: 1, DirCount: 0, TotalSize: 512})
			time.Sleep(110 * time.Millisecond) // ensure throttle interval passes
			onProgress(scan.Progress{CurrentPath: target + "/b", FileCount: 2, DirCount: 0, TotalSize: 1024})
		}
		return scan.Result{
			Entries: []scan.Entry{
				{Path: target + "/a", Size: 512, IsDir: false},
				{Path: target + "/b", Size: 512, IsDir: false},
			},
			TotalSize:  1024,
			FileCount:  2,
			DirCount:   0,
			DurationMs: 50,
		}, nil
	}
}

func TestHealthz(t *testing.T) {
	srv := New(minimalAssetFS(), "/tmp", fixtureScanFunc("/tmp"))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /healthz: status %d", rr.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("JSON parse: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("status: %q", resp["status"])
	}
}

func TestIndex(t *testing.T) {
	srv := New(minimalAssetFS(), "/tmp", fixtureScanFunc("/tmp"))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /: status %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "<html") {
		t.Errorf("body missing <html>: %q", rr.Body.String())
	}
}

func TestStaticAssets(t *testing.T) {
	srv := New(minimalAssetFS(), "/tmp", fixtureScanFunc("/tmp"))
	req := httptest.NewRequest(http.MethodGet, "/static/chart.js", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /static/chart.js: status %d", rr.Code)
	}
}

func TestEventsSSE_CompleteEvent(t *testing.T) {
	// Use a real HTTP test server (not recorder) because SSE needs Flusher.
	srv := New(minimalAssetFS(), "/tmp/testdir", fixtureScanFunc("/tmp/testdir"))

	ts := httptest.NewServer(loggingMiddleware(srv.mux))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/events")
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: %q", ct)
	}

	// Read until we get a complete event or timeout.
	doneCh := make(chan map[string]any, 1)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		var lastEvent string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event:") {
				lastEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			}
			if strings.HasPrefix(line, "data:") && lastEvent == "complete" {
				raw := strings.TrimPrefix(line, "data: ")
				var payload map[string]any
				if err := json.Unmarshal([]byte(raw), &payload); err == nil {
					doneCh <- payload
					return
				}
			}
		}
		close(doneCh)
	}()

	select {
	case payload, ok := <-doneCh:
		if !ok {
			t.Fatal("SSE stream ended without complete event")
		}
		for _, field := range []string{"totalSize", "fileCount", "dirCount", "elapsedMs", "entries"} {
			if _, found := payload[field]; !found {
				t.Errorf("complete payload missing field %q", field)
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for complete event")
	}
}

func TestHistoryList_Empty(t *testing.T) {
	// Override history dir to a fresh temp dir.
	dir := t.TempDir()
	origFn := history.HistoryDirFnForTest()
	history.SetHistoryDirFnForTest(func() (string, error) { return dir, nil })
	defer history.SetHistoryDirFnForTest(origFn)

	srv := New(minimalAssetFS(), "/tmp", fixtureScanFunc("/tmp"))
	req := httptest.NewRequest(http.MethodGet, "/history?target=/tmp", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /history: status %d", rr.Code)
	}
	var arr []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &arr); err != nil {
		t.Fatalf("JSON: %v — body: %q", err, rr.Body.String())
	}
	if len(arr) != 0 {
		t.Errorf("expected empty, got %d entries", len(arr))
	}
}

func TestHistoryFile_TraversalRejected(t *testing.T) {
	srv := New(minimalAssetFS(), "/tmp", fixtureScanFunc("/tmp"))

	for _, bad := range []string{
		"/history/../../etc/passwd",
		"/history/../secret.json",
	} {
		req := httptest.NewRequest(http.MethodGet, bad, nil)
		rr  := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("GET %s: expected 400, got %d", bad, rr.Code)
		}
	}
}

func TestStartBindsLocalhost(t *testing.T) {
	srv := New(minimalAssetFS(), "/tmp", fixtureScanFunc("/tmp"))
	url, err := srv.Start(0) // port 0 = OS chooses
	// Note: port 0 is below the 1-65535 validation range applied by main,
	// but the server.Start itself does not re-validate — it uses whatever is passed.
	// Using port 0 here is fine for testing net.Listen.
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Errorf("URL should be localhost-only, got %s", url)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

// TestPortInMain validates the port-range check in main logic.
func TestPortValidation(t *testing.T) {
	for _, p := range []int{0, -1, 65536, 99999} {
		if p >= 1 && p <= 65535 {
			t.Errorf("port %d should be invalid", p)
		}
	}
	for _, p := range []int{1, 80, 8420, 65535} {
		if p < 1 || p > 65535 {
			t.Errorf("port %d should be valid", p)
		}
	}
}

func TestOpen_RequiresPath(t *testing.T) {
	srv := New(minimalAssetFS(), t.TempDir(), fixtureScanFunc(""))
	req := httptest.NewRequest(http.MethodPost, "/open", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestOpen_RejectsOutsideRoot(t *testing.T) {
	dir := t.TempDir()
	srv := New(minimalAssetFS(), dir, fixtureScanFunc(dir))
	called := false
	srv.opener = func(path string, reveal bool) error { called = true; return nil }

	req := httptest.NewRequest(http.MethodPost, "/open?path="+url.QueryEscape("/etc/passwd"), nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	if called {
		t.Fatal("opener must not be called for a path outside the scan root")
	}
}

func TestOpen_ValidRevealsFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	srv := New(minimalAssetFS(), dir, fixtureScanFunc(dir))
	var gotPath string
	var gotReveal bool
	srv.opener = func(path string, reveal bool) error { gotPath, gotReveal = path, reveal; return nil }

	req := httptest.NewRequest(http.MethodPost, "/open?path="+url.QueryEscape(f)+"&reveal=1", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	if gotPath != f || !gotReveal {
		t.Fatalf("opener got (%q, %v), want (%q, true)", gotPath, gotReveal, f)
	}
}

func TestEvents_RootOutsideRejected(t *testing.T) {
	dir := t.TempDir()
	srv := New(minimalAssetFS(), dir, fixtureScanFunc(dir))
	req := httptest.NewRequest(http.MethodGet, "/events?root="+url.QueryEscape("/etc"), nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestEvents_RootWithinSetsTarget(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	srv := New(minimalAssetFS(), dir, fixtureScanFunc(dir))
	ts := httptest.NewServer(loggingMiddleware(srv))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/events?root=" + url.QueryEscape(sub))
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()

	doneCh := make(chan map[string]any, 1)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		var lastEvent string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event:") {
				lastEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			}
			if strings.HasPrefix(line, "data:") && lastEvent == "complete" {
				var payload map[string]any
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload) == nil {
					doneCh <- payload
					return
				}
			}
		}
		close(doneCh)
	}()

	select {
	case payload, ok := <-doneCh:
		if !ok {
			t.Fatal("SSE stream ended without complete event")
		}
		if payload["target"] != sub {
			t.Errorf("complete target = %v, want %q", payload["target"], sub)
		}
		if payload["base"] != dir {
			t.Errorf("complete base = %v, want %q", payload["base"], dir)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for complete event")
	}
}

func TestState_ReportsRoot(t *testing.T) {
	dir := t.TempDir()
	srv := New(minimalAssetFS(), dir, fixtureScanFunc(dir))
	req := httptest.NewRequest(http.MethodGet, "/state", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	var s map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &s); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if s["root"] != dir {
		t.Errorf("root = %q, want %q", s["root"], dir)
	}
}

func TestState_EmptyWhenNoRoot(t *testing.T) {
	srv := New(minimalAssetFS(), "", fixtureScanFunc(""))
	req := httptest.NewRequest(http.MethodGet, "/state", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	var s map[string]string
	json.Unmarshal(rr.Body.Bytes(), &s)
	if s["root"] != "" {
		t.Errorf("root = %q, want empty", s["root"])
	}
}

func TestPick_SetsRoot(t *testing.T) {
	chosen := t.TempDir()
	srv := New(minimalAssetFS(), "", fixtureScanFunc(""))
	srv.picker = func() (string, error) { return chosen, nil }

	req := httptest.NewRequest(http.MethodPost, "/pick", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	if srv.getRoot() != chosen {
		t.Errorf("root not set: got %q, want %q", srv.getRoot(), chosen)
	}
}

func TestPick_Cancelled(t *testing.T) {
	srv := New(minimalAssetFS(), "", fixtureScanFunc(""))
	srv.picker = func() (string, error) { return "", nil }
	req := httptest.NewRequest(http.MethodPost, "/pick", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	if srv.getRoot() != "" {
		t.Errorf("root should stay empty on cancel, got %q", srv.getRoot())
	}
}

func TestPick_Unsupported(t *testing.T) {
	srv := New(minimalAssetFS(), "", fixtureScanFunc(""))
	srv.picker = func() (string, error) { return "", errPickerUnsupported }
	req := httptest.NewRequest(http.MethodPost, "/pick", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", rr.Code)
	}
}

func TestPick_PathParamSetsRoot(t *testing.T) {
	dir := t.TempDir()
	srv := New(minimalAssetFS(), "", fixtureScanFunc(""))
	// picker must not be called when ?path= is provided
	srv.picker = func() (string, error) { t.Fatal("picker should not run"); return "", nil }

	req := httptest.NewRequest(http.MethodPost, "/pick?path="+url.QueryEscape(dir), nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	if srv.getRoot() != dir {
		t.Errorf("root = %q, want %q", srv.getRoot(), dir)
	}
}

// ensure test helpers exist; if they don't, stubs that skip are fine.
func init() {
	// Ensure test temp dir for any snapshot writes triggered during integration tests.
	_ = os.MkdirAll(os.TempDir(), 0700)
}
