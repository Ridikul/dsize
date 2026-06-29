package sse

import (
	"bufio"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewWriter_SetsHeaders(t *testing.T) {
	rr := httptest.NewRecorder()
	sw := NewWriter(rr)
	if sw == nil {
		t.Fatal("NewWriter returned nil")
	}
	ct := rr.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type: got %q want %q", ct, "text/event-stream")
	}
	cc := rr.Header().Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("Cache-Control: got %q want %q", cc, "no-cache")
	}
	// The initial retry directive should be written.
	if !strings.Contains(rr.Body.String(), "retry: 3000") {
		t.Errorf("missing retry directive in body: %q", rr.Body.String())
	}
}

func TestEmit_WritesEvent(t *testing.T) {
	rr := httptest.NewRecorder()
	sw := NewWriter(rr)

	err := sw.Emit(Event{Type: "complete", Data: map[string]any{"status": "ok"}})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "event: complete") {
		t.Errorf("missing event line: %q", body)
	}
	if !strings.Contains(body, `"status"`) {
		t.Errorf("missing data: %q", body)
	}
}

func TestProgressThrottle(t *testing.T) {
	rr := httptest.NewRecorder()
	sw := NewWriter(rr)
	sw.minInterval = 100 * time.Millisecond

	// Emit 5 progress events rapidly — only the first should go through.
	for i := 0; i < 5; i++ {
		sw.Emit(Event{Type: "progress", Data: map[string]any{"n": i}})
	}

	body := rr.Body.String()
	count := strings.Count(body, "event: progress")
	if count != 1 {
		t.Errorf("expected 1 progress event (throttled), got %d", count)
	}
}

func TestProgressThrottle_AllowsAfterInterval(t *testing.T) {
	rr := httptest.NewRecorder()
	sw := NewWriter(rr)
	sw.minInterval = 1 * time.Millisecond

	sw.Emit(Event{Type: "progress", Data: map[string]any{"n": 0}})
	time.Sleep(2 * time.Millisecond)
	sw.Emit(Event{Type: "progress", Data: map[string]any{"n": 1}})

	body := rr.Body.String()
	count := strings.Count(body, "event: progress")
	if count != 2 {
		t.Errorf("expected 2 progress events, got %d", count)
	}
}

func TestSSE_EventFormat(t *testing.T) {
	rr := httptest.NewRecorder()
	sw := NewWriter(rr)
	sw.Emit(Event{Type: "error", Data: map[string]any{"message": "boom"}})

	// Each event must have event: line, data: line, and blank line terminator.
	scanner := bufio.NewScanner(strings.NewReader(rr.Body.String()))
	var eventLines, dataLines, blankLines int
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") { eventLines++ }
		if strings.HasPrefix(line, "data:")  { dataLines++ }
		if line == ""                         { blankLines++ }
	}
	if eventLines == 0 { t.Error("no event: lines") }
	if dataLines  == 0 { t.Error("no data: lines") }
	if blankLines == 0 { t.Error("no blank-line terminators") }
}
