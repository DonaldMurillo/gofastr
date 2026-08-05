package log

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type recordingReporter struct {
	mu      sync.Mutex
	reports []ErrorReport
}

func (r *recordingReporter) Report(rep ErrorReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reports = append(r.reports, rep)
}

// TestRecoveryMiddleware_ReportsToReporter pins the error-reporting seam:
// a panic recovered by the log plugin's recovery middleware reaches the
// configured ErrorReporter carrying the panic value, a stack trace, and the
// request context (method + path + route).
func TestRecoveryMiddleware_ReportsToReporter(t *testing.T) {
	rep := &recordingReporter{}
	h := recoveryMiddleware(rep)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/things", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	rep.mu.Lock()
	defer rep.mu.Unlock()
	if len(rep.reports) != 1 {
		t.Fatalf("got %d reports, want 1: %+v", len(rep.reports), rep.reports)
	}
	rp := rep.reports[0]
	if rp.Error != "boom" {
		t.Errorf("Error = %q, want %q", rp.Error, "boom")
	}
	if !strings.Contains(rp.Stack, "goroutine") {
		t.Errorf("Stack missing goroutine trace: %q", rp.Stack)
	}
	if rp.Method != http.MethodPost {
		t.Errorf("Method = %q, want POST", rp.Method)
	}
	if rp.Path != "/things" {
		t.Errorf("Path = %q, want /things", rp.Path)
	}
}

// TestRecoveryMiddleware_NilReporterIsSafe ensures a nil reporter never
// turns a panic into a secondary nil-panic.
func TestRecoveryMiddleware_NilReporterIsSafe(t *testing.T) {
	h := recoveryMiddleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// TestSlogErrorReporter_PreservesPanicShape pins that the default reporter
// keeps the http.panic log shape existing tests + operators rely on (msg
// "http.panic", attr "panic" carrying the value).
func TestSlogErrorReporter_PreservesPanicShape(t *testing.T) {
	sink := &memSink{}
	logger := slog.New(newFanoutHandler([]Sink{sink}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	SlogErrorReporter{Logger: logger}.Report(ErrorReport{
		Message: "http.panic",
		Error:   "explode",
		Method:  "POST",
		Path:    "/things",
		Stack:   "goroutine 1 [running]:\nmain.x",
	})
	found := false
	for _, line := range sink.lines() {
		var e map[string]any
		if err := json.Unmarshal(line, &e); err != nil {
			t.Fatalf("entry not valid JSON: %v: %s", err, line)
		}
		if e["msg"] == "http.panic" && e["panic"] == "explode" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected http.panic with panic=explode")
	}
}

// TestHTTPErrorReporter_Delivers confirms the generic HTTP sink POSTs a JSON
// report to the configured URL, reusing the webhook-sink delivery machinery.
func TestHTTPErrorReporter_Delivers(t *testing.T) {
	var (
		mu   sync.Mutex
		body []byte
		got  int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = append(body, b...)
		got++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rep := NewHTTPErrorReporter(srv.URL, WebhookOpts{BatchSize: 1, BatchInterval: 0})
	rep.Report(ErrorReport{Message: "http.panic", Error: "boom", Method: "POST", Path: "/x"})
	_ = rep.Close()

	// Close flushes the pending batch synchronously.
	mu.Lock()
	defer mu.Unlock()
	if got == 0 {
		t.Fatalf("no POST received; body=%s", string(body))
	}
	var env struct {
		Entries []json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope: %v (body=%s)", err, string(body))
	}
	if len(env.Entries) == 0 {
		t.Fatal("empty entries envelope")
	}
	var rep0 ErrorReport
	if err := json.Unmarshal(env.Entries[0], &rep0); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if rep0.Error != "boom" || rep0.Message != "http.panic" {
		t.Errorf("decoded report = %+v, want Error=boom Message=http.panic", rep0)
	}
}
