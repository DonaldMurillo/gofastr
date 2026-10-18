package renderdiag

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingFlusher struct {
	header  http.Header
	flushes int
	wrote   bool
}

func newRecordingFlusher() *recordingFlusher {
	return &recordingFlusher{header: http.Header{}}
}

func (r *recordingFlusher) Header() http.Header         { return r.header }
func (r *recordingFlusher) Write(b []byte) (int, error) { r.wrote = true; return len(b), nil }
func (r *recordingFlusher) WriteHeader(int)             {}
func (r *recordingFlusher) Flush()                      { r.flushes++ }

// TestResponseDelegatesFlushAndUnwrap pins the wrapper's passthrough
// behaviour: http.NewResponseController must reach the underlying
// writer's Flush through Unwrap (and the wrapper's own Flush method),
// and Unwrap must return that writer. Before these existed, a flush on
// a page path silently no-op'd only in test binaries.
func TestResponseDelegatesFlushAndUnwrap(t *testing.T) {
	under := newRecordingFlusher()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w, _ := TestResponse(under, req)
	rc := http.NewResponseController(w)
	if err := rc.Flush(); err != nil {
		t.Fatalf("ResponseController.Flush through the wrapper: %v", err)
	}
	if under.flushes == 0 {
		t.Fatal("Flush did not reach the underlying writer")
	}
	if got := w.(interface{ Unwrap() http.ResponseWriter }).Unwrap(); got != http.ResponseWriter(under) {
		t.Fatalf("Unwrap returned %T, want the underlying writer", got)
	}
}

type kindSubject struct{}

// TestReportNamesWhatPanicked pins the message wording: the kind prefix
// says what panicked, so a layout build's panic is not logged as a
// component's. The generated e2e gate greps for "component render
// panic:", so the component spelling is load-bearing too.
func TestReportNamesWhatPanicked(t *testing.T) {
	cases := map[string]string{
		"component":     "component render panic: renderdiag.kindSubject:",
		"layout build":  "layout build render panic: renderdiag.kindSubject:",
		"layout area":   "layout area render panic: renderdiag.kindSubject:",
		"fill fallback": "fill fallback render panic: renderdiag.kindSubject:",
	}
	for kind, want := range cases {
		var got string
		ctx := WithObserver(context.Background(), func(msg string) { got = msg })
		Report(ctx, kind, kindSubject{}, "boom")
		if !strings.HasPrefix(got, want) {
			t.Errorf("Report(%q) message = %q, want prefix %q", kind, got, want)
		}
	}
}

// A request built on ProductionContext keeps its writer, and the same
// request without it gets the test-only wrapper.
func TestProductionContextKeepsWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w, _ := TestResponse(rec, req.WithContext(ProductionContext(t, context.Background())))
	if w != http.ResponseWriter(rec) {
		t.Fatalf("production context wrapped the writer: %T", w)
	}
	if w, _ := TestResponse(rec, req); w == http.ResponseWriter(rec) {
		t.Fatal("a request without the exemption kept its writer")
	}
}
