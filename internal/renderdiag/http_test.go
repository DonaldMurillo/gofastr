package renderdiag

import (
	"context"
	"errors"
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

func TestResponseDelegatesFlushAndUnwrap(t *testing.T) {
	under := newRecordingFlusher()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w, _ := TestResponse(under, req)
	flusher, ok := w.(http.Flusher)
	if !ok {
		t.Error("wrapped writer hides http.Flusher")
	} else {
		flusher.Flush()
	}
	rc := http.NewResponseController(w)
	if err := rc.Flush(); err != nil {
		t.Fatalf("ResponseController.Flush through the wrapper: %v", err)
	}
	if under.flushes != 2 {
		t.Fatalf("underlying writer flushed %d times, want 2", under.flushes)
	}
	if got := w.(interface{ Unwrap() http.ResponseWriter }).Unwrap(); got != http.ResponseWriter(under) {
		t.Fatalf("Unwrap returned %T, want the underlying writer", got)
	}
}

type failingFlusher struct {
	*recordingFlusher
	err error
}

func (w failingFlusher) FlushError() error { return w.err }

func TestResponsePropagatesFlushError(t *testing.T) {
	want := errors.New("flush failed")
	under := failingFlusher{recordingFlusher: newRecordingFlusher(), err: want}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w, _ := TestResponse(under, req)
	if _, ok := w.(interface{ FlushError() error }); !ok {
		t.Error("flush-capable wrapper lacks FlushError")
	}
	if err := http.NewResponseController(w).Flush(); !errors.Is(err, want) {
		t.Fatalf("ResponseController.Flush = %v, want %v", err, want)
	}
}

type plainWriter struct{ header http.Header }

func (p *plainWriter) Header() http.Header         { return p.header }
func (p *plainWriter) Write(b []byte) (int, error) { return len(b), nil }
func (p *plainWriter) WriteHeader(int)             {}

// A controller over the wrapper must not report a flush that never
// happened: the wrapper answered Flusher even over a writer that cannot
// flush, so rc.Flush() returned nil and hid the failure from the caller.
func TestResponseFlushAnswersFailureWhenTheWriterCannotFlush(t *testing.T) {
	under := &plainWriter{header: http.Header{}}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w, _ := TestResponse(under, req)
	if _, ok := w.(http.Flusher); ok {
		t.Error("wrapped writer advertises unsupported http.Flusher")
	}
	if err := http.NewResponseController(w).Flush(); !errors.Is(err, http.ErrNotSupported) {
		t.Fatalf("ResponseController.Flush = %v, want ErrNotSupported", err)
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
