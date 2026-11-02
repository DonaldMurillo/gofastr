package latch

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"io"
	"net"
	"net/http"
)

// ---- (a) a sender that leaves the latch unset --------------------------

// c0d6e99e, pre-fix: core/middleware metricsResponseWriter. WriteHeader
// and Write latch wroteHeader; Flush sends the headers and does not, so
// a WriteHeader(500) after the flush (dropped by net/http) is recorded.
type metricsWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *metricsWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.ResponseWriter.WriteHeader(code)
	w.status = code
	w.wroteHeader = true
}

func (w *metricsWriter) Write(b []byte) (int, error) { // want `wrapwriter: metricsWriter forwards Write to the writer it wraps but declares no ReadFrom`
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *metricsWriter) Flush() { // want `wrapwriter: metricsWriter.Flush sends the response but leaves wroteHeader unset`
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack sends no status through the writer: quiet without the latch.
func (w *metricsWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *metricsWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// framework/experimental/webmcp statusRecorder: an integer latched to a
// constant by Write, and a Flush that leaves it at 0.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Flush() { // want `wrapwriter: statusRecorder.Flush sends the response but leaves status unset`
	http.NewResponseController(s.ResponseWriter).Flush()
}

// A ReadFrom that forwards the body around the latch.
func (s *statusRecorder) ReadFrom(src io.Reader) (int64, error) { // want `wrapwriter: statusRecorder.ReadFrom sends the response but leaves status unset`
	return io.Copy(s.ResponseWriter, src)
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// ---- (b) an observing Write with no ReadFrom ---------------------------
// ---- (c) a latch set before the forwarded WriteHeader returns ----------

// c0d6e99e + f44e9c7b + 39de7827, pre-fix: core/middleware
// recoveryWriter. started is set before WriteHeader forwards (an
// invalid code panics in net/http with nothing sent, and the recover
// below then cuts the connection instead of answering 500), and there
// is no ReadFrom, so a file served under Recovery loses sendfile.
type recoveryWriter struct {
	http.ResponseWriter
	started bool
}

func (w *recoveryWriter) WriteHeader(code int) {
	if code < 100 || code >= 200 {
		w.started = true // want `wrapwriter: recoveryWriter.WriteHeader sets started before the forwarded WriteHeader returns`
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *recoveryWriter) Write(b []byte) (int, error) { // want `wrapwriter: recoveryWriter forwards Write to the writer it wraps but declares no ReadFrom`
	w.started = true
	return w.ResponseWriter.Write(b)
}

func (w *recoveryWriter) Flush() {
	w.started = true
	http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *recoveryWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &recoveryWriter{ResponseWriter: w}
		defer func() {
			if v := recover(); v != nil {
				if rw.started {
					panic(http.ErrAbortHandler)
				}
				http.Error(w, "internal", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(rw, r)
	})
}

// ---- quiet: the fix spellings ----------------------------------------

// The fixed recoveryWriter: latch after the forward returns, Flush and
// ReadFrom latch through markStarted.
type fixedRecovery struct {
	http.ResponseWriter
	started bool
	status  int
}

func (w *fixedRecovery) WriteHeader(code int) {
	w.ResponseWriter.WriteHeader(code)
	if code >= 200 {
		w.markStarted(code)
	}
}

func (w *fixedRecovery) markStarted(code int) {
	w.started = true
	if w.status == 0 {
		w.status = code
	}
}

func (w *fixedRecovery) Write(b []byte) (int, error) {
	w.markStarted(http.StatusOK)
	return w.ResponseWriter.Write(b)
}

func (w *fixedRecovery) FlushError() error {
	w.markStarted(http.StatusOK)
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *fixedRecovery) Flush() { _ = w.FlushError() }

// A hijack sets started and leaves status at 0 on purpose.
func (w *fixedRecovery) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, brw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.started = true
	}
	return conn, brw, err
}

func (w *fixedRecovery) ReadFrom(src io.Reader) (int64, error) {
	w.markStarted(http.StatusOK)
	return io.Copy(w.ResponseWriter, src)
}

func (w *fixedRecovery) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func FixedRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &fixedRecovery{ResponseWriter: w}
		defer func() {
			if v := recover(); v != nil && rw.started {
				panic(http.ErrAbortHandler)
			}
		}()
		next.ServeHTTP(rw, r)
	})
}

// The fixed metrics writer: Flush latches; ReadFrom does what Write does
// first; a ReadFrom that routes the body through the receiver's own
// Write (io.Copy into a writerOnly) latches through it.
type fixedMetrics struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *fixedMetrics) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.ResponseWriter.WriteHeader(code)
	w.status = code
	w.wroteHeader = true
}

func (w *fixedMetrics) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *fixedMetrics) Flush() {
	w.wroteHeader = true
	http.NewResponseController(w.ResponseWriter).Flush()
}

type writerOnly struct{ io.Writer }

func (w *fixedMetrics) ReadFrom(src io.Reader) (int64, error) {
	return io.Copy(writerOnly{w}, src)
}

func (w *fixedMetrics) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// ---- quiet: postures -------------------------------------------------

// The idempotency recorder: Flush reaches FlushError, which sets the
// recorder's own streamed flag, and nothing it recorded is replayed. Its
// Write also tees into a buffer, so it is not a pass-through either.
type idemRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	streamed    bool
	body        bytes.Buffer
}

func (r *idemRecorder) WriteHeader(code int) {
	r.status = code
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *idemRecorder) Write(p []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	r.body.Write(p)
	return r.ResponseWriter.Write(p)
}

func (r *idemRecorder) FlushError() error {
	r.streamed = true
	return http.NewResponseController(r.ResponseWriter).Flush()
}

func (r *idemRecorder) Flush() { _ = r.FlushError() }

func (r *idemRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// A buffering wrapper (the Timeout writer): it forwards only once
// streaming, so its wroteHeader is the handler's intent, not what was
// sent, and a flush that commits the buffer owes it nothing.
type bufWriter struct {
	w           http.ResponseWriter
	h           http.Header
	buf         bytes.Buffer
	streaming   bool
	wroteHeader bool
}

func (b *bufWriter) Header() http.Header { return b.h }

func (b *bufWriter) WriteHeader(code int) {
	b.wroteHeader = true
	if b.streaming {
		b.w.WriteHeader(code)
	}
}

func (b *bufWriter) Write(p []byte) (int, error) {
	b.wroteHeader = true
	if b.streaming {
		return b.w.Write(p)
	}
	return b.buf.Write(p)
}

func (b *bufWriter) Flush() {
	b.streaming = true
	http.NewResponseController(b.w).Flush()
}

func (b *bufWriter) Unwrap() http.ResponseWriter { return b.w }

// The same buffering shape with a Flush that sets no flag of its own:
// still quiet, the forward under the if is what marks it.
type holdWriter struct {
	w           http.ResponseWriter
	h           http.Header
	held        bool
	wroteHeader bool
}

func (b *holdWriter) Header() http.Header { return b.h }

func (b *holdWriter) WriteHeader(code int) {
	b.wroteHeader = true
	if !b.held {
		b.w.WriteHeader(code)
	}
}

func (b *holdWriter) Write(p []byte) (int, error) { return len(p), nil }

func (b *holdWriter) Flush() { http.NewResponseController(b.w).Flush() }

func (b *holdWriter) Unwrap() http.ResponseWriter { return b.w }

// A status recorded, not latched (`status = code`, a constructor
// default of 200): Flush owes it nothing.
type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Flush() { http.NewResponseController(r.ResponseWriter).Flush() }

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// A transform: Write deflates, so a ReadFrom forwarding raw bytes would
// be wrong, and none is asked for.
type gzipWriter struct {
	http.ResponseWriter
	zw   *gzip.Writer
	pass bool
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if g.pass {
		return g.ResponseWriter.Write(b)
	}
	return g.zw.Write(b)
}

func (g *gzipWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

// Near miss for (c): the latch is set before the forward, but no
// deferred recover reads it (the logging wrapper's statusCode).
type logWriter struct {
	http.ResponseWriter
	wrote bool
}

func (l *logWriter) WriteHeader(code int) {
	l.wrote = true
	l.ResponseWriter.WriteHeader(code)
}

func (l *logWriter) Unwrap() http.ResponseWriter { return l.ResponseWriter }

// Never handed to a handler: no production chain holds it.
type orphan struct {
	http.ResponseWriter
	wrote bool
}

func (o *orphan) Write(b []byte) (int, error) {
	o.wrote = true
	return o.ResponseWriter.Write(b)
}

func (o *orphan) Flush() {}

func Chain(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&metricsWriter{ResponseWriter: w, status: 200}, r)
		next.ServeHTTP(&statusRecorder{ResponseWriter: w}, r)
		next.ServeHTTP(&fixedMetrics{ResponseWriter: w, status: 200}, r)
		next.ServeHTTP(&idemRecorder{ResponseWriter: w}, r)
		next.ServeHTTP(&bufWriter{w: w, h: http.Header{}}, r)
		next.ServeHTTP(&holdWriter{w: w, h: http.Header{}}, r)
		next.ServeHTTP(&recorder{ResponseWriter: w, status: 200}, r)
		next.ServeHTTP(&gzipWriter{ResponseWriter: w}, r)
		next.ServeHTTP(&logWriter{ResponseWriter: w}, r)
		defer func() {
			if recover() != nil {
				_ = (&orphan{}).wrote
			}
		}()
	})
}
