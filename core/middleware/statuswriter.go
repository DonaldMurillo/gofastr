package middleware

import (
	"bufio"
	"io"
	"net"
	"net/http"
)

// statusRecorder records the final status a response sent. A 1xx other
// than 101 is forwarded without being recorded, the first final status is
// latched, and a Write before any WriteHeader records an implicit 200.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusRecorder) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	// Latch only once the call returned: net/http panics on a code outside
	// 100-999 before sending anything, and Recovery still owes that 500.
	w.ResponseWriter.WriteHeader(code)
	w.status = code
	w.wroteHeader = true
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// statusWriter is the statusRecorder MetricsMiddleware and Tracing wrap
// the response in: it always offers Flush, Hijack and Push, and a flush
// commits the status.
type statusWriter struct {
	statusRecorder
}

func newStatusWriter(w http.ResponseWriter) *statusWriter {
	return &statusWriter{statusRecorder{ResponseWriter: w, status: http.StatusOK}}
}

func (w *statusWriter) ReadFrom(src io.Reader) (int64, error) {
	return copyResponse(w, w.ResponseWriter, src)
}

// copyResponse writes the first byte read from source through wrapped, so
// it records the implicit status, then copies the rest to underlying.
func copyResponse(wrapped io.Writer, underlying http.ResponseWriter, source io.Reader) (int64, error) {
	var first [1]byte
	for {
		count, readErr := source.Read(first[:])
		var written int
		if count > 0 {
			var writeErr error
			written, writeErr = wrapped.Write(first[:count])
			if writeErr != nil {
				return int64(written), writeErr
			}
			if written != count {
				return int64(written), io.ErrShortWrite
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				readErr = nil
			}
			return int64(written), readErr
		}
		if count > 0 {
			copied, err := io.Copy(underlying, source)
			return int64(written) + copied, err
		}
	}
}

func (w *statusWriter) Flush() {
	w.wroteHeader = true
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

func (w *statusWriter) Push(target string, opts *http.PushOptions) error {
	if pu, ok := w.ResponseWriter.(http.Pusher); ok {
		return pu.Push(target, opts)
	}
	return http.ErrNotSupported
}
