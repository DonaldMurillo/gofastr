package a

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"time"
)

// ---- arm one: wrappers with no Unwrap ---------------------------------

// P1, reduced: core/middleware timeoutWriter. Flush and Hijack are
// forwarded, but SetWriteDeadline through ResponseController is not.
type timeoutWriter struct { // want `wrapwriter: timeoutWriter wraps http.ResponseWriter and intercepts Write/WriteHeader with no Unwrap`
	w    http.ResponseWriter
	h    http.Header
	code int
}

func (tw *timeoutWriter) Header() http.Header         { return tw.h }
func (tw *timeoutWriter) WriteHeader(code int)        { tw.code = code }
func (tw *timeoutWriter) Write(p []byte) (int, error) { return tw.w.Write(p) }
func (tw *timeoutWriter) Flush()                      { tw.w.(http.Flusher).Flush() }
func (tw *timeoutWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return tw.w.(http.Hijacker).Hijack()
}

func newTimeoutWriter(w http.ResponseWriter) *timeoutWriter {
	return &timeoutWriter{w: w, h: w.Header().Clone()}
}

func Timeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tw := newTimeoutWriter(w)
		done := make(chan struct{})
		go func() {
			defer close(done)
			next.ServeHTTP(tw, r)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	})
}

// P40, reduced: the idempotency recorder, embedded writer, Write and
// WriteHeader only.
type idempotencyRecorder struct { // want `wrapwriter: idempotencyRecorder wraps`
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (r *idempotencyRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *idempotencyRecorder) Write(p []byte) (int, error) {
	r.body.Write(p)
	return r.ResponseWriter.Write(p)
}

func Idempotency(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &idempotencyRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
	})
}

// B16, reduced: the cache recorder buffers on purpose. Still reported:
// buffering does not decide what happens to a stream.
type responseRecorder struct { // want `wrapwriter: responseRecorder wraps`
	header http.Header
	body   *bytes.Buffer
	w      http.ResponseWriter // written only on overflow
}

func (r *responseRecorder) Header() http.Header         { return r.header }
func (r *responseRecorder) WriteHeader(int)             {}
func (r *responseRecorder) Write(b []byte) (int, error) { return r.body.Write(b) }

func Cache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &responseRecorder{header: make(http.Header), body: &bytes.Buffer{}, w: w}
		next.ServeHTTP(rec, r)
		// Headers set on the real writer before the replay: a different
		// writer than the handler got (arm two stays quiet).
		w.Header().Set("X-Cache", "MISS")
		for k, v := range rec.header {
			w.Header()[k] = v
		}
		_, _ = w.Write(rec.body.Bytes())
	})
}

// Value receivers, dispatched through an interface variable and a
// handler-func value.
type valueWriter struct { // want `wrapwriter: valueWriter wraps`
	http.ResponseWriter
}

func (v valueWriter) Write(b []byte) (int, error) { return v.ResponseWriter.Write(b) }

func Value(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w = valueWriter{w}
		next(w, r)
	}
}

// An Unwrap with the wrong signature does not count.
type wrongUnwrap struct { // want `wrapwriter: wrongUnwrap wraps`
	http.ResponseWriter
}

func (w *wrongUnwrap) WriteHeader(code int) { w.ResponseWriter.WriteHeader(code) }
func (w *wrongUnwrap) Unwrap() error        { return errors.New("no") }

func Wrong(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&wrongUnwrap{w}, r)
	})
}

// A field whose type is an interface that embeds ResponseWriter.
type flushingWriter interface {
	http.ResponseWriter
	http.Flusher
}

type ifaceField struct { // want `wrapwriter: ifaceField wraps`
	fw flushingWriter
}

func (i *ifaceField) Header() http.Header         { return i.fw.Header() }
func (i *ifaceField) WriteHeader(c int)           { i.fw.WriteHeader(c) }
func (i *ifaceField) Write(b []byte) (int, error) { return i.fw.Write(b) }

func Iface(next http.Handler, fw flushingWriter, r *http.Request) {
	next.ServeHTTP(&ifaceField{fw: fw}, r)
}

// A wrapper chain: the outer holds the inner wrapper in a named field,
// so nothing is promoted, and neither declares Unwrap. Only the outer
// reaches a handler.
type innerWrap struct{ http.ResponseWriter }

func (i *innerWrap) Write(b []byte) (int, error) { return i.ResponseWriter.Write(b) }

type outerWrap struct { // want `wrapwriter: outerWrap wraps`
	in *innerWrap
}

func (o *outerWrap) Header() http.Header         { return o.in.Header() }
func (o *outerWrap) WriteHeader(c int)           { o.in.WriteHeader(c) }
func (o *outerWrap) Write(b []byte) (int, error) { return o.in.Write(b) }

func Chain(next http.Handler, w http.ResponseWriter, r *http.Request) {
	next.ServeHTTP(&outerWrap{in: &innerWrap{w}}, r)
}

// ---- arm one: quiet ---------------------------------------------------

// The fix spelling: Unwrap on the pointer receiver.
type tracingWriter struct {
	http.ResponseWriter
	status int
}

func (w *tracingWriter) WriteHeader(code int)        { w.status = code; w.ResponseWriter.WriteHeader(code) }
func (w *tracingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Unwrap on a value receiver, type used by value.
type stripWriter struct{ http.ResponseWriter }

func (s stripWriter) Write(b []byte) (int, error) { return s.ResponseWriter.Write(b) }
func (s stripWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Holds a writer but is not one (no Header): core/stream's SSEWriter.
type sseWriter struct{ w http.ResponseWriter }

func (s *sseWriter) Write(b []byte) (int, error) { return s.w.Write(b) }

// A concrete field: the Unwrap is promoted from *tracingWriter.
type flushWriter struct{ *tracingWriter }

func (f flushWriter) Write(b []byte) (int, error) { return f.tracingWriter.Write(b) }

// A concrete buffer field: there is no connection behind it to reach.
type recWrap struct{ rec *httptest.ResponseRecorder }

func (w *recWrap) Header() http.Header         { return w.rec.Header() }
func (w *recWrap) WriteHeader(code int)        { w.rec.WriteHeader(code) }
func (w *recWrap) Write(b []byte) (int, error) { return w.rec.Write(b) }

// Dispatched only from a _test.go file: no production chain holds it.
type onlyTested struct{ http.ResponseWriter }

func (o *onlyTested) Write(b []byte) (int, error) { return o.ResponseWriter.Write(b) }

// Embeds and intercepts nothing.
type carrier struct {
	http.ResponseWriter
	id string
}

// Handed only to a declared function, never to a handler value:
// core/static's notFoundWriter.
type notFoundWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *notFoundWriter) WriteHeader(code int) {
	w.wrote = true
	w.ResponseWriter.WriteHeader(http.StatusNotFound)
}

func serveFile(w http.ResponseWriter, r *http.Request) bool { return true }

func serveNotFound(w http.ResponseWriter, r *http.Request) bool {
	return serveFile(&notFoundWriter{ResponseWriter: w}, r)
}

func Quiet(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&tracingWriter{ResponseWriter: w}, r)
		next.ServeHTTP(stripWriter{w}, r)
		next.ServeHTTP(flushWriter{&tracingWriter{ResponseWriter: w}}, r)
		next.ServeHTTP(&carrier{ResponseWriter: w}, r)
		next.ServeHTTP(&recWrap{rec: httptest.NewRecorder()}, r)
	})
}

// ---- arm two: header after next ---------------------------------------

type carrierMap map[string][]string

func inject(c carrierMap) { c["Traceparent"] = []string{"00-x"} }

// P42, reduced: the header goes in through a conversion after the
// handler ran.
func Tracing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := &tracingWriter{ResponseWriter: w}
		next.ServeHTTP(ww, r)
		inject(carrierMap(ww.Header())) // want `wrapwriter: response header changed after the next handler ran`
	})
}

// Set, Add, Del and an index write on the same writer.
func After(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		w.Header().Set("X-A", "1")        // want `wrapwriter: response header changed after`
		w.Header().Add("X-B", "1")        // want `wrapwriter: response header changed after`
		w.Header().Del("X-C")             // want `wrapwriter: response header changed after`
		w.Header()["X-D"] = []string{"1"} // want `wrapwriter: response header changed after`
		(w.Header()).Set("X-E", "1")      // want `wrapwriter: response header changed after`
	})
}

// A pass-through wrapper shares the outer writer's header map.
func Aliased(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := &tracingWriter{ResponseWriter: w}
		next.ServeHTTP(ww, r)
		w.Header().Set("X-Status", "done") // want `wrapwriter: response header changed after`
	})
}

// A handler-func value, and a closure deferred before the dispatch.
func Deferred(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			w.Header().Set("X-Elapsed", "1ms") // want `wrapwriter: response header changed after`
		}()
		next(w, r)
	}
}

// A nested block after the dispatch is still dominated.
func Nested(next http.Handler, flag bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		if flag {
			w.Header().Set("X-Flag", "1") // want `wrapwriter: response header changed after`
		}
	})
}

// ---- arm two: quiet ---------------------------------------------------

// The fix spelling: set before the handler.
func Before(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Traceparent", "00-x")
		inject(carrierMap(w.Header()))
		next.ServeHTTP(w, r)
	})
}

// Reads after the handler.
func Reads(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		_ = w.Header().Get("Content-Type")
		_ = w.Header().Values("Vary")
		_ = w.Header().Clone()
		logHeaders(w.Header())
	})
}

func logHeaders(h http.Header) {}

// A dispatch in a branch that returns does not dominate the set below.
func Bypass(next http.Handler, bypass bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if bypass {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("X-Wrapped", "1")
		next.ServeHTTP(w, r)
	})
}

// A deferred closure counts only against a dispatch in its own block.
// Here the dispatch is conditional, so the rule stays quiet (a miss
// when flag holds, accepted for precision).
func DeferOtherBlock(next http.Handler, flag bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { w.Header().Set("X-Q", "1") }()
		if flag {
			next.ServeHTTP(w, r)
		}
	})
}

// A declared function with the handler signature is not the next link.
func writeJSON(w http.ResponseWriter, r *http.Request) {}

func Helper(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r)
	w.Header().Set("X-After-Helper", "1")
}

// A header set inside the wrapper's WriteHeader hook: no dispatch.
func (w *idempotencyRecorder) hook() { w.Header().Set("X-Hook", "1") }
