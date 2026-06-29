// Package server implements the local HTTP server for dsize --ui mode.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dsize/internal/history"
	"dsize/internal/scan"
	"dsize/internal/sse"
)

// Server holds the HTTP server and its state.
type Server struct {
	mux      *http.ServeMux
	httpSrv  *http.Server
	assets   fs.FS
	scanFunc func(ctx context.Context, root string, onProgress func(scan.Progress)) (scan.Result, error)

	mu   sync.RWMutex
	root string // current scan root; the floor for re-scans and /open. May be empty.

	// opener reveals a path in the OS file manager. It is a field so tests
	// can substitute a fake instead of launching Finder/Explorer.
	opener func(path string, reveal bool) error
	// picker opens the native folder dialog and returns the chosen path.
	// Empty path with nil error means the user cancelled; errPickerUnsupported
	// means no dialog tool is available. A field so tests can fake it.
	picker func() (string, error)
}

// New creates a Server.
//   - assets: the embedded fs.FS (rooted at web/)
//   - target: the initial scan root, also the boundary for re-scans and /open
//     (neither may escape it). May be empty to start with no scan (the UI then
//     shows a welcome screen until the user picks a folder).
//   - scanFunc: caller-supplied function that scans a given root
func New(assets fs.FS, target string, scanFunc func(ctx context.Context, root string, onProgress func(scan.Progress)) (scan.Result, error)) *Server {
	if target != "" {
		if abs, err := filepath.Abs(target); err == nil {
			target = abs
		}
	}
	s := &Server{
		mux:      http.NewServeMux(),
		assets:   assets,
		scanFunc: scanFunc,
		root:     target,
		opener:   openInFileManager,
		picker:   pickFolder,
	}
	s.registerRoutes()
	return s
}

// getRoot returns the current scan root (the breadcrumb floor).
func (s *Server) getRoot() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.root
}

// setRoot updates the current scan root.
func (s *Server) setRoot(p string) {
	s.mu.Lock()
	s.root = p
	s.mu.Unlock()
}

// Start binds to 127.0.0.1:<port> and starts serving.
// Returns the URL the server is listening on.
func (s *Server) Start(port int) (string, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}

	s.httpSrv = &http.Server{
		Handler: loggingMiddleware(s),
	}

	go func() {
		_ = s.httpSrv.Serve(ln)
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	return url, nil
}

// ServeHTTP implements http.Handler so tests can pass *Server directly.
// It rejects path traversal on the raw request path before delegating to the
// mux, because http.ServeMux silently cleans ".." segments and issues a 301
// redirect to the cleaned path, which would bypass the per-handler guard.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.Path, "..") {
		http.Error(w, "invalid path: traversal not allowed", http.StatusBadRequest)
		return
	}
	s.mux.ServeHTTP(w, r)
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpSrv != nil {
		return s.httpSrv.Shutdown(ctx)
	}
	return nil
}

// registerRoutes wires up all HTTP endpoints.
func (s *Server) registerRoutes() {
	// Serve static assets (FR-2.2).
	staticFS, _ := fs.Sub(s.assets, "static")
	s.mux.Handle("/static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))

	// Serve index.html at / (FR-2.3).
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		f, err := s.assets.Open("index.html")
		if err != nil {
			http.Error(w, "index.html not found", http.StatusInternalServerError)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data, _ := fs.ReadFile(s.assets, "index.html")
		w.Write(data)
	})

	// SSE endpoint (FR-3.1).
	s.mux.HandleFunc("/events", s.handleEvents)

	// Current scan root (empty until the user picks one).
	s.mux.HandleFunc("/state", s.handleState)

	// Native folder picker.
	s.mux.HandleFunc("/pick", s.handlePick)

	// Open/reveal a path in the OS file manager.
	s.mux.HandleFunc("/open", s.handleOpen)

	// History endpoints (FR-8.4, FR-8.5).
	s.mux.HandleFunc("/history/", s.handleHistoryFile)
	s.mux.HandleFunc("/history", s.handleHistoryList)

	// Health check (NFR-3.3).
	s.mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})
}

// handleEvents streams SSE events (FR-3). An optional ?root=<path> query
// selects a directory to scan instead of the startup target; it must resolve
// to a directory within the startup target.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	base := s.getRoot()
	root := base
	if q := r.URL.Query().Get("root"); q != "" {
		resolved, ok := s.resolveWithin(q)
		if !ok {
			http.Error(w, "invalid root: must be a directory within the scan root", http.StatusBadRequest)
			return
		}
		if fi, err := os.Stat(resolved); err != nil || !fi.IsDir() {
			http.Error(w, "invalid root: not a directory", http.StatusBadRequest)
			return
		}
		root = resolved
	}
	if root == "" {
		http.Error(w, "no folder selected to scan", http.StatusBadRequest)
		return
	}

	sw := sse.NewWriter(w)
	if sw == nil {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()

	result, err := s.scanFunc(ctx, root, func(p scan.Progress) {
		_ = sw.Emit(sse.Event{
			Type: "progress",
			Data: map[string]any{
				"currentPath": p.CurrentPath,
				"totalSize":   p.TotalSize,
				"fileCount":   p.FileCount,
				"dirCount":    p.DirCount,
				"elapsedMs":   p.ElapsedMs,
				"filesPerSec": p.FilesPerSec,
				"errorCount":  p.ErrorCount,
			},
		})
	})

	if err != nil {
		_ = sw.Emit(sse.Event{
			Type: "error",
			Data: map[string]any{"message": err.Error()},
		})
		return
	}

	// Compute trends vs latest prior snapshot (FR-8.6).
	histEntries := scanEntriesToHistory(result.Entries)
	prev, _ := history.LatestSnapshot(root)
	if prev != nil {
		histEntries = history.ComputeTrends(histEntries, prev.Entries)
	} else {
		for i := range histEntries {
			histEntries[i].Trend = "new"
		}
	}

	// Build complete payload.
	completeEntries := make([]map[string]any, len(histEntries))
	for i, e := range histEntries {
		completeEntries[i] = map[string]any{
			"path":  e.Path,
			"size":  e.Size,
			"isDir": e.IsDir,
			"trend": e.Trend,
		}
	}

	_ = sw.Emit(sse.Event{
		Type: "complete",
		Data: map[string]any{
			"target":     root,
			"base":       base, // current scan root: the breadcrumb floor
			"totalSize":  result.TotalSize,
			"fileCount":  result.FileCount,
			"dirCount":   result.DirCount,
			"elapsedMs":  result.DurationMs,
			"errorCount": result.ErrorCount,
			"entries":    completeEntries,
		},
	})

	// Persist snapshot asynchronously so the SSE response closes promptly.
	go func() {
		snap := history.Snapshot{
			Version:    1,
			ScannedAt:  time.Now().UTC().Format(time.RFC3339),
			Target:     root,
			TotalSize:  result.TotalSize,
			FileCount:  result.FileCount,
			DirCount:   result.DirCount,
			DurationMs: result.DurationMs,
			Entries:    histEntries,
		}
		if err := history.Write(snap); err != nil {
			// Non-fatal per FR-7.6; caller logs.
			fmt.Printf("warning: could not write history snapshot: %v\n", err)
		}
	}()
}

// handleOpen reveals a path in the OS file manager. ?path=<p> is required and
// must resolve within the scan root. ?reveal=1 selects a file inside its
// parent folder; otherwise the path is opened as a directory.
func (s *Server) handleOpen(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		http.Error(w, "path query parameter required", http.StatusBadRequest)
		return
	}
	resolved, ok := s.resolveWithin(p)
	if !ok {
		http.Error(w, "invalid path: must be within the scan root", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(resolved); err != nil {
		http.Error(w, "path does not exist", http.StatusNotFound)
		return
	}
	reveal := r.URL.Query().Get("reveal") == "1"
	if err := s.opener(resolved, reveal); err != nil {
		http.Error(w, "could not open path: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

// resolveWithin cleans p to an absolute path and verifies it lies within the
// current scan root (or equals it). Returns the cleaned path and whether it is
// allowed.
func (s *Server) resolveWithin(p string) (string, bool) {
	root := s.getRoot()
	if root == "" {
		return "", false
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	abs = filepath.Clean(abs)
	base := filepath.Clean(root)
	if abs == base {
		return abs, true
	}
	if strings.HasPrefix(abs, base+string(filepath.Separator)) {
		return abs, true
	}
	return "", false
}

// handleState reports the current scan root (empty if none selected yet).
func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"root": s.getRoot()})
}

// handlePick opens the native OS folder dialog. On success it becomes the new
// scan root and is returned; 204 if the user cancelled; 501 if no native
// dialog tool is available (the UI then offers a path field instead).
func (s *Server) handlePick(w http.ResponseWriter, r *http.Request) {
	// Path-field fallback: ?path=<dir> sets the root directly without a dialog.
	if p := r.URL.Query().Get("path"); p != "" {
		abs, err := filepath.Abs(p)
		if err != nil {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		abs = filepath.Clean(abs)
		if fi, statErr := os.Stat(abs); statErr != nil || !fi.IsDir() {
			http.Error(w, "not a directory: "+abs, http.StatusBadRequest)
			return
		}
		s.setRoot(abs)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"path": abs})
		return
	}

	path, err := s.picker()
	if errors.Is(err, errPickerUnsupported) {
		http.Error(w, "no native folder dialog available", http.StatusNotImplemented)
		return
	}
	if err != nil {
		http.Error(w, "folder dialog failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if path == "" {
		w.WriteHeader(http.StatusNoContent) // cancelled
		return
	}
	if fi, statErr := os.Stat(path); statErr != nil || !fi.IsDir() {
		http.Error(w, "selected path is not a directory", http.StatusBadRequest)
		return
	}
	s.setRoot(filepath.Clean(path))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"path": s.getRoot()})
}

// handleHistoryList returns JSON array of summaries for a target (FR-8.4).
func (s *Server) handleHistoryList(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	if target == "" {
		http.Error(w, "target query parameter required", http.StatusBadRequest)
		return
	}

	summaries, err := history.ListSummaries(target)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if summaries == nil {
		summaries = []history.Summary{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summaries)
}

// handleHistoryFile returns a full snapshot by filename (FR-8.5, NFR-2.2).
func (s *Server) handleHistoryFile(w http.ResponseWriter, r *http.Request) {
	// Extract bare filename from the path.
	raw := strings.TrimPrefix(r.URL.Path, "/history/")

	// Reject path traversal (NFR-2.2).
	if strings.Contains(raw, "..") || strings.Contains(raw, "/") || strings.Contains(raw, "\\") {
		http.Error(w, "invalid filename: path traversal not allowed", http.StatusBadRequest)
		return
	}
	if raw == "" {
		http.Error(w, "filename required", http.StatusBadRequest)
		return
	}
	// Additional check using filepath.Clean.
	if cleaned := filepath.Clean(raw); cleaned != raw {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	snap, err := history.ReadSnapshot(raw)
	if err != nil {
		if strings.Contains(err.Error(), "traversal") {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(snap)
}

// loggingMiddleware logs each request with method, path, status, latency (NFR-3.1).
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		fmt.Fprintf(os.Stderr, "[dsize] %s %s %d %s\n",
			r.Method, r.URL.Path, rw.status, time.Since(start))
	})
}

// statusRecorder captures the HTTP status code for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Flush forwards to the underlying ResponseWriter's Flusher so that SSE
// streaming keeps working when wrapped by this middleware. Without it, the
// embedded http.ResponseWriter interface does not promote Flush(), so the SSE
// writer's http.Flusher assertion fails and /events returns 500.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// scanEntriesToHistory converts scan.Entry to history.Entry (no import cycle).
func scanEntriesToHistory(entries []scan.Entry) []history.Entry {
	out := make([]history.Entry, len(entries))
	for i, e := range entries {
		out[i] = history.Entry{
			Path:  e.Path,
			Size:  e.Size,
			IsDir: e.IsDir,
		}
	}
	return out
}
