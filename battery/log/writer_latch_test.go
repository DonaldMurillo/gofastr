package log

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sendfileRecorder implements io.ReaderFrom like net/http's own writer.
type sendfileRecorder struct {
	*httptest.ResponseRecorder
	readFrom bool
}

func (r *sendfileRecorder) ReadFrom(src io.Reader) (int64, error) {
	r.readFrom = true
	return io.Copy(r.ResponseRecorder, src)
}

// TestAccessWriterLatchesOnFlush: a flush sends the headers (200 unless
// a status was set). The access-log writer latched wroteHeader only in
// Write and WriteHeader, so a WriteHeader(500) after a flush, which
// net/http drops, was logged as the status of a 200 stream.
func TestAccessWriterLatchesOnFlush(t *testing.T) {
	rw := &countingResponseWriter{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}
	rw.Flush()
	rw.WriteHeader(http.StatusInternalServerError)
	if rw.status != http.StatusOK {
		t.Fatalf("logged status %d after a flush, want the 200 the client got", rw.status)
	}
}

// TestAccessWriterKeepsReadFrom: embedding http.ResponseWriter does not
// promote ReadFrom, so a file served behind the access log lost the
// connection's sendfile path. The writer forwards it, latching the
// implicit 200 and counting the bytes.
func TestAccessWriterKeepsReadFrom(t *testing.T) {
	inner := &sendfileRecorder{ResponseRecorder: httptest.NewRecorder()}
	rw := &countingResponseWriter{ResponseWriter: inner, status: http.StatusOK}
	body := "file body"
	if _, err := io.Copy(rw, io.LimitReader(strings.NewReader(body), int64(len(body)))); err != nil {
		t.Fatal(err)
	}
	if !inner.readFrom {
		t.Fatal("the body went through Write: ReadFrom (sendfile) was hidden")
	}
	if rw.bytes != int64(len(body)) {
		t.Fatalf("bytes = %d, want %d", rw.bytes, len(body))
	}
	rw.WriteHeader(http.StatusInternalServerError)
	if rw.status != http.StatusOK {
		t.Fatalf("logged status %d after the body, want 200", rw.status)
	}
}
