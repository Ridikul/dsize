// Package sse implements Server-Sent Events encoding and a throttled writer.
package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Event represents a single SSE event (FR-3.2).
type Event struct {
	Type string // "progress" | "complete" | "error"
	Data any    // will be JSON-encoded
}

// Writer wraps an http.ResponseWriter and sends SSE events.
// Progress events are throttled to at most 10 Hz (FR-3.3).
type Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher

	mu            sync.Mutex
	lastProgress  time.Time
	minInterval   time.Duration
}

// NewWriter creates an SSE writer and sends required SSE headers.
// Returns nil if the underlying writer does not support flushing.
func NewWriter(w http.ResponseWriter) *Writer {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering if present

	// Send retry directive (FR-3.5).
	fmt.Fprintf(w, "retry: 3000\n\n")
	flusher.Flush()

	return &Writer{
		w:           w,
		flusher:     flusher,
		minInterval: 100 * time.Millisecond,
	}
}

// Emit sends the event to the client.
// If the event type is "progress" and it is too soon since the last emission,
// the event is silently dropped (FR-3.3).
// Returns an error if writing failed (client disconnected).
func (sw *Writer) Emit(e Event) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	if e.Type == "progress" {
		now := time.Now()
		if now.Sub(sw.lastProgress) < sw.minInterval {
			return nil // throttle
		}
		sw.lastProgress = now
	}

	return sw.write(e)
}

// write serialises and sends one SSE event.  Must be called with mu held.
func (sw *Writer) write(e Event) error {
	data, err := json.Marshal(e.Data)
	if err != nil {
		return fmt.Errorf("sse marshal: %w", err)
	}
	if _, err := fmt.Fprintf(sw.w, "event: %s\ndata: %s\n\n", e.Type, data); err != nil {
		return err
	}
	sw.flusher.Flush()
	return nil
}

// Flush pushes any buffered data to the client.
func (sw *Writer) Flush() {
	sw.flusher.Flush()
}
