package webmcp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type sendfileRecorder struct {
	*httptest.ResponseRecorder
	readFrom bool
}

func (r *sendfileRecorder) ReadFrom(src io.Reader) (int64, error) {
	r.readFrom = true
	return io.Copy(r.ResponseRecorder, src)
}

// TestStatusRecorderLatchesOnFlush: a flush sends the headers (200
// unless a status was set). The observer's recorder latched only in
// Write, so a WriteHeader(500) after a flush, which net/http drops, was
// reported as an http_500 outcome for a call the client got as 200.
func TestStatusRecorderLatchesOnFlush(t *testing.T) {
	rec := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	rec.Flush()
	rec.WriteHeader(http.StatusInternalServerError)
	if rec.status != http.StatusOK {
		t.Fatalf("status %d after a flush, want 200", rec.status)
	}
}

// TestStatusRecorderKeepsReadFrom: embedding http.ResponseWriter does
// not promote ReadFrom, so a file a tool route served lost the
// connection's sendfile path under the observer.
func TestStatusRecorderKeepsReadFrom(t *testing.T) {
	inner := &sendfileRecorder{ResponseRecorder: httptest.NewRecorder()}
	rec := &statusRecorder{ResponseWriter: inner}
	if _, err := io.Copy(rec, io.LimitReader(strings.NewReader("x"), 1)); err != nil {
		t.Fatal(err)
	}
	if !inner.readFrom {
		t.Fatal("the body went through Write: ReadFrom (sendfile) was hidden")
	}
	if rec.status != http.StatusOK {
		t.Fatalf("status %d, want the implicit 200", rec.status)
	}
}
