package live

import (
	"bytes"
	"net/http"
)

// capturingRecorder buffers a response so we can inspect status before
// committing bytes to the real ResponseWriter. Used by serveApp's
// fallback path: if the wrapped router returns 404 and the client wants
// HTML, we discard the buffered 404 and emit the host page instead.
type capturingRecorder struct {
	header http.Header
	body   bytes.Buffer
	code   int
}

func newCapturingRecorder() *capturingRecorder {
	return &capturingRecorder{header: http.Header{}, code: http.StatusOK}
}

func (r *capturingRecorder) Header() http.Header { return r.header }

func (r *capturingRecorder) Write(b []byte) (int, error) { return r.body.Write(b) }

func (r *capturingRecorder) WriteHeader(code int) { r.code = code }

// flushTo writes the captured response to a real ResponseWriter.
func (r *capturingRecorder) flushTo(w http.ResponseWriter) {
	for k, vs := range r.header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	if r.code != http.StatusOK {
		w.WriteHeader(r.code)
	}
	_, _ = w.Write(r.body.Bytes())
}
