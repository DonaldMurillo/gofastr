package tracing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

func TestRecorderCapturesSpanTree(t *testing.T) {
	dir := t.TempDir()
	r := NewRecorder(dir, ids.NewSessionID())
	root := r.Start(SpanID{}, "turn", map[string]any{"turn": 1})
	mw := r.Start(root, "request-middleware-chain", nil)
	time.Sleep(5 * time.Millisecond)
	r.End(mw, "ok", nil)
	provider := r.Start(root, "provider.chat", map[string]any{"model": "fake"})
	time.Sleep(5 * time.Millisecond)
	r.End(provider, "ok", nil)
	r.End(root, "ok", nil)

	path, err := r.Done()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var tr Trace
	if err := json.Unmarshal(data, &tr); err != nil {
		t.Fatal(err)
	}
	if len(tr.Spans) != 3 {
		t.Fatalf("spans = %d, want 3", len(tr.Spans))
	}
	for _, s := range tr.Spans {
		if s.DurationNS <= 0 {
			t.Errorf("span %q duration = %d", s.Name, s.DurationNS)
		}
	}
	// Hierarchy: two spans should have ParentID set (the children of root).
	childCount := 0
	for _, s := range tr.Spans {
		if s.ParentID != "" {
			childCount++
		}
	}
	if childCount != 2 {
		t.Errorf("children = %d, want 2", childCount)
	}
}

func TestTraceContextHeader(t *testing.T) {
	tid := NewTraceID()
	sid := NewSpanID()
	h := TraceContextHeader(tid, sid)
	if !strings.HasPrefix(h, "00-") {
		t.Errorf("traceparent prefix wrong: %q", h)
	}
	if !strings.HasSuffix(h, "-01") {
		t.Errorf("traceparent suffix wrong: %q", h)
	}
	parts := strings.Split(h, "-")
	if len(parts) != 4 || len(parts[1]) != 32 || len(parts[2]) != 16 {
		t.Errorf("traceparent shape wrong: %q", h)
	}
}

func TestDoneWritesToSessionDir(t *testing.T) {
	dir := t.TempDir()
	sess := ids.NewSessionID()
	r := NewRecorder(dir, sess)
	root := r.Start(SpanID{}, "turn", nil)
	r.End(root, "ok", nil)
	path, err := r.Done()
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := filepath.Join(dir, string(sess))
	if !strings.HasPrefix(path, wantPrefix) {
		t.Errorf("path = %q, want prefix %q", path, wantPrefix)
	}
}
