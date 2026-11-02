package middleware

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"math"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// compressMinBytes is the smallest body Compress encodes. Below it the
// gzip header and trailer (18 bytes) plus the lost Content-Length cost
// more than the deflate saving.
const compressMinBytes = 1024

// compressCacheMaxBody and compressCacheMaxBytes bound the compressed
// copies kept for immutable responses: one entry never exceeds the
// first, all entries together never exceed the second.
const (
	compressCacheMaxBody  = 4 << 20
	compressCacheMaxBytes = 32 << 20
)

// Compress gzips response bodies for clients that accept gzip.
//
// It encodes only text-like types (HTML, CSS, JavaScript, JSON, XML,
// SVG, plain text, wasm) at least compressMinBytes long, and leaves
// alone: HEAD and Range requests, SSE (text/event-stream), bodies a
// handler already encoded, 204/206/304 and 1xx answers, and hijacked
// connections. Every compressible answer gets Vary: Accept-Encoding,
// compressed or not, so a shared cache never hands gzip to a client
// that did not ask for it.
//
// The gzip representation gets its own entity tag: a strong "x"
// becomes "x-gzip" (weak tags keep their W/ prefix). An If-None-Match
// or If-Match naming the gzip tag is rewritten back to the handler's
// tag before the handler runs, so a handler's own ETag comparison still
// answers 304 (which echoes the gzip tag), and a conditional write that
// names the tag the client was handed is not refused with 412.
//
// A Flush from the handler flushes the gzip stream, so progressive
// rendering keeps painting. A body under a strong ETag is held until
// the handler returns and compressed in one pass, since a stream's
// bytes depend on where the handler flushes and a strong tag names one
// encoding. A held body that flushes streams instead, and its tag
// turns weak.
//
// Responses marked Cache-Control: immutable are compressed once at
// the best level, from the second time the same bytes are served, and
// the compressed copy is reused (keyed by the SHA-256 of the body,
// bounded in size). The handler
// still runs on every request; only the deflate work is skipped.
//
// Only gzip is offered: the standard library has no Brotli encoder.
func Compress() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead || r.Header.Get("Range") != "" {
				next.ServeHTTP(w, r)
				return
			}
			accepts := acceptsGzip(r.Header.Get("Accept-Encoding"))
			revalidatingGzip := false
			if accepts {
				if inm := r.Header.Get("If-None-Match"); strings.Contains(inm, gzipETagSuffix) {
					revalidatingGzip = true
					//gofastr:allow(controlbytes) rewrites the inbound request's own header, already CTL-checked by net/http; it never reaches a response or a log.
					r.Header.Set("If-None-Match", stripGzipETags(inm))
				}
				// If-Match too: a client that read the gzip representation
				// echoes its tag on the PUT that follows, and the handler
				// compares against its own tag, so without the rewrite every
				// optimistic-concurrency write from that client was 412.
				if im := r.Header.Get("If-Match"); strings.Contains(im, gzipETagSuffix) {
					//gofastr:allow(controlbytes) rewrites the inbound request's own header, already CTL-checked by net/http; it never reaches a response or a log.
					r.Header.Set("If-Match", stripGzipETags(im))
				}
			}
			cw := &compressWriter{rw: w, accepts: accepts, revalidatingGzip: revalidatingGzip, status: http.StatusOK}
			returned := false
			defer func() {
				if returned {
					return
				}
				// A panic before anything was sent stays as it is, for
				// Recovery's 500. One after the headers went out is
				// re-raised as abortedStream (the original frames are
				// still below this one, so the stack Recovery logs
				// holds them).
				if cw.abort() {
					// nil: runtime.Goexit, which must keep unwinding.
					switch v := recover(); v {
					case nil:
					case http.ErrAbortHandler:
						panic(v)
					default:
						panic(abortedStream{v})
					}
				}
			}()
			next.ServeHTTP(cw, r)
			returned = true
			cw.finish()
		})
	}
}

const gzipETagSuffix = `-gzip"`

// acceptsGzip reports whether an Accept-Encoding value admits gzip: a
// gzip (or x-gzip) coding whose q-value is not zero, or, when neither
// is named, a * whose q-value is not zero. A named gzip wins over the
// wildcard in either order, so "gzip;q=0, *" refuses gzip.
func acceptsGzip(header string) bool {
	if header == "" {
		return false
	}
	gzipQ, starQ := -1.0, -1.0
	for part := range strings.SplitSeq(header, ",") {
		coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		coding = strings.ToLower(strings.TrimSpace(coding))
		if coding != "gzip" && coding != "x-gzip" && coding != "*" {
			continue
		}
		q := 1.0
		for p := range strings.SplitSeq(params, ";") {
			k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
			if ok && strings.EqualFold(strings.TrimSpace(k), "q") {
				f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
				if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
					f = 0
				}
				q = f
			}
		}
		if coding == "*" {
			starQ = max(starQ, q)
		} else {
			gzipQ = max(gzipQ, q)
		}
	}
	if gzipQ >= 0 {
		return gzipQ > 0
	}
	return starQ > 0
}

// stripGzipETags turns every gzip entity tag in an If-None-Match or
// If-Match list back into the handler's tag ("x-gzip" → "x").
func stripGzipETags(inm string) string {
	return strings.ReplaceAll(inm, gzipETagSuffix, `"`)
}

// gzipETag derives the gzip representation's tag from the handler's.
// The suffix goes on every quoted tag, one already ending in -gzip
// included, because stripGzipETags takes exactly one off: skipping it
// here made such a tag lose its own -gzip on revalidation and never
// match. The one ambiguity left: a client revalidating an identity
// copy whose handler tag ends in -gzip gets a 200, never a wrong 304.
func gzipETag(etag string) string {
	if !strings.HasSuffix(etag, `"`) {
		return etag
	}
	return strings.TrimSuffix(etag, `"`) + gzipETagSuffix
}

// compressibleType reports whether a Content-Type is worth deflating.
func compressibleType(ct string) bool {
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	switch {
	case mt == "text/event-stream":
		return false
	case strings.HasPrefix(mt, "text/"):
		return true
	case strings.HasSuffix(mt, "+json"), strings.HasSuffix(mt, "+xml"):
		return true
	}
	switch mt {
	case "application/javascript", "application/x-javascript", "application/ecmascript",
		"application/json", "application/manifest+json", "application/xml",
		"image/svg+xml", "application/wasm":
		return true
	}
	return false
}

var gzipWriters = sync.Pool{New: func() any {
	zw, _ := gzip.NewWriterLevel(nil, gzip.DefaultCompression)
	return zw
}}

// compressWriter holds the response until it can decide: the decision
// waits for compressMinBytes of body, a Flush, or the end of the
// handler, whichever comes first.
type compressWriter struct {
	rw               http.ResponseWriter
	accepts          bool
	revalidatingGzip bool

	status      int
	wroteHeader bool // the status is committed: later WriteHeader calls are no-ops
	decided     bool
	encoding    bool // gzip chosen
	held        bool // buffer the whole body: one encoding per strong tag
	cacheable   bool // a held body marked immutable: use the compressed-copy cache
	passthrough bool // identity, headers sent
	hijacked    bool
	headerSent  bool
	buf         bytes.Buffer
	zw          *gzip.Writer
}

func (w *compressWriter) Header() http.Header { return w.rw.Header() }

// Unwrap lets http.ResponseController reach the underlying writer for
// the calls this wrapper does not intercept (deadlines).
func (w *compressWriter) Unwrap() http.ResponseWriter { return w.rw }

// WriteHeader keeps the first final status, as net/http does: a second
// call, or one after the first Write, changes nothing.
func (w *compressWriter) WriteHeader(status int) {
	if w.wroteHeader || w.headerSent || w.decided {
		return
	}
	if status >= 100 && status < 200 {
		w.rw.WriteHeader(status) // informational: forward, keep waiting
		return
	}
	w.wroteHeader = true
	w.status = status
}

func (w *compressWriter) Write(p []byte) (int, error) {
	if w.hijacked {
		return 0, http.ErrHijacked
	}
	w.wroteHeader = true
	if !w.decided {
		w.buf.Write(p)
		if w.needsSniff() {
			if w.buf.Len() < sniffLen {
				return len(p), nil // not enough to sniff yet
			}
			w.sniff()
		}
		if !w.eligible() {
			if err := w.decide(false); err != nil {
				return 0, err
			}
			return len(p), nil
		}
		switch {
		case w.buf.Len() > compressCacheMaxBody:
			_ = w.decide(true)
			if w.held { // too big to hold: stream it
				w.spill(false)
			}
		case w.buf.Len() >= compressMinBytes && !w.holdsWhole():
			_ = w.decide(true)
		}
		return len(p), nil
	}
	if w.passthrough {
		return w.rw.Write(p)
	}
	if w.held {
		w.buf.Write(p)
		if w.buf.Len() > compressCacheMaxBody {
			w.spill(false) // too big to hold: stream the rest
		}
		return len(p), nil
	}
	return w.zw.Write(p)
}

// sniffLen is how much body net/http's DetectContentType reads.
const sniffLen = 512

// needsSniff reports whether net/http would sniff a Content-Type for
// this response: no Content-Type key (a nil value is the handler
// suppressing it), no Content-Encoding (go.dev/issue/31753), no
// Transfer-Encoding, and a status that allows a body.
func (w *compressWriter) needsSniff() bool {
	h := w.Header()
	if _, ok := h["Content-Type"]; ok {
		return false
	}
	if h.Get("Content-Encoding") != "" || h.Get("Transfer-Encoding") != "" {
		return false
	}
	switch w.status {
	case http.StatusNoContent, http.StatusNotModified:
		return false
	}
	return true
}

// sniff sets the Content-Type net/http would, from the buffered body,
// so the type decision sees the type the client gets. Sniffing the
// first Write alone typed a template that opens with a newline as
// text/plain.
func (w *compressWriter) sniff() {
	if w.buf.Len() > 0 && w.needsSniff() {
		w.Header().Set("Content-Type", http.DetectContentType(w.buf.Bytes()))
	}
}

// eligible reports whether the response could be compressed, before
// its size is known.
func (w *compressWriter) eligible() bool {
	h := w.Header()
	switch w.status {
	case http.StatusNoContent, http.StatusPartialContent, http.StatusNotModified:
		return false
	}
	if ce := h.Get("Content-Encoding"); ce != "" && !strings.EqualFold(ce, "identity") {
		return false
	}
	if h.Get("Content-Range") != "" {
		return false
	}
	if !compressibleType(h.Get("Content-Type")) {
		return false
	}
	if cl := h.Get("Content-Length"); cl != "" {
		if n, err := strconv.Atoi(cl); err == nil && n < compressMinBytes {
			return false
		}
	}
	return true
}

// holdsWhole reports whether the body is held until the handler
// returns and compressed in one pass: an immutable body, for the
// compressed-copy cache, or one under a strong ETag, which must name
// one encoding. A stream's bytes depend on where the handler flushes.
func (w *compressWriter) holdsWhole() bool {
	if !w.accepts || w.status != http.StatusOK {
		return false
	}
	h := w.Header()
	if strings.Contains(h.Get("Cache-Control"), "immutable") {
		return true
	}
	et := h.Get("ETag")
	return et != "" && !strings.HasPrefix(et, "W/")
}

// spill turns a held body into a stream, with the headers still
// unsent. A body that outgrew the hold streams on every request, so
// its stream is its one encoding and the tag stays. A flush streams
// only this request, so the tag turns weak: it now names two.
func (w *compressWriter) spill(flushed bool) {
	w.held, w.cacheable = false, false
	if flushed {
		weakenETag(w.Header())
	}
	w.startStream()
}

func weakenETag(h http.Header) {
	if et := h.Get("ETag"); et != "" && !strings.HasPrefix(et, "W/") {
		h.Set("ETag", "W/"+et)
	}
}

// decide commits the encoding choice and sends the headers. An
// identity body held so far is written here; a compressed one by the
// caller's path (finish or stream start). The error is the identity
// write's.
func (w *compressWriter) decide(compress bool) error {
	if w.decided {
		return nil
	}
	w.decided = true
	h := w.Header()
	if compressibleType(h.Get("Content-Type")) {
		addVary(h, "Accept-Encoding")
	}
	if w.status == http.StatusNotModified && w.revalidatingGzip {
		if et := h.Get("ETag"); et != "" {
			h.Set("ETag", gzipETag(et))
		}
	}
	if !compress || !w.accepts {
		w.passthrough = true
		w.sendHeader()
		if w.buf.Len() > 0 {
			_, err := w.rw.Write(w.buf.Bytes())
			w.buf.Reset()
			return err
		}
		return nil
	}
	w.encoding = true
	h.Del("Content-Length")
	// A byte range of this body would index the identity bytes (Range
	// requests skip compression), so it advertises none.
	h.Del("Accept-Ranges")
	h.Set("Content-Encoding", "gzip")
	if et := h.Get("ETag"); et != "" {
		h.Set("ETag", gzipETag(et))
	}
	if w.holdsWhole() {
		w.held = true
		w.cacheable = strings.Contains(h.Get("Cache-Control"), "immutable")
		return nil // headers go out with the body in finish
	}
	w.startStream()
	return nil
}

// startStream sends the headers and starts deflating, writing whatever
// is buffered first.
func (w *compressWriter) startStream() {
	w.sendHeader()
	w.zw = gzipWriters.Get().(*gzip.Writer)
	w.zw.Reset(w.rw)
	if w.buf.Len() > 0 {
		_, _ = w.zw.Write(w.buf.Bytes())
		w.buf.Reset()
	}
}

func (w *compressWriter) sendHeader() {
	if w.headerSent {
		return
	}
	w.headerSent = true
	w.rw.WriteHeader(w.status)
}

func (w *compressWriter) Flush() {
	if w.hijacked {
		return
	}
	if !w.decided {
		w.sniff()
		_ = w.decide(w.eligible() && w.buf.Len() > 0)
	}
	if w.held {
		w.spill(true)
	}
	if w.zw != nil {
		_ = w.zw.Flush()
	}
	if !w.headerSent {
		w.sendHeader()
	}
	if f, ok := w.rw.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *compressWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if w.headerSent || w.buf.Len() > 0 {
		return nil, nil, fmt.Errorf("compress: hijack after the response started")
	}
	conn, rw, err := http.NewResponseController(w.rw).Hijack()
	if err == nil {
		// Only a hijack that happened owns the connection: after
		// ErrNotSupported the handler still answers through this writer.
		w.hijacked = true
	}
	return conn, rw, err
}

// finish runs after the handler returns: it settles an undecided
// response and closes the gzip stream.
func (w *compressWriter) finish() {
	if w.hijacked {
		return
	}
	if !w.decided {
		// Sniff here too: sendHeader calls WriteHeader before the body,
		// so a writer below that sniffs only on an implicit header
		// would never type it.
		w.sniff()
		big := w.buf.Len() >= compressMinBytes
		if big && w.holdsWhole() && w.eligible() {
			_ = w.decide(true)
		} else {
			if !big {
				w.Header().Del("Content-Length")
				if w.buf.Len() > 0 || w.status == http.StatusOK {
					w.Header().Set("Content-Length", strconv.Itoa(w.buf.Len()))
				}
			}
			_ = w.decide(big && w.eligible())
		}
	}
	if w.held {
		var body []byte
		if w.cacheable {
			body = cachedGzip(w.buf.Bytes())
		} else {
			body = gzipOnce(w.buf.Bytes(), gzip.DefaultCompression)
		}
		w.buf.Reset()
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.sendHeader()
		_, _ = w.rw.Write(body)
		return
	}
	if w.zw != nil {
		_ = w.zw.Close()
		w.zw.Reset(nil)
		gzipWriters.Put(w.zw)
		w.zw = nil
	}
	if !w.headerSent {
		w.sendHeader()
	}
}

// abort runs when the handler panics, and reports whether the headers
// were already sent. A gzip stream in progress is dropped without its
// trailer: the response is cut short, and a complete-looking gzip
// member would hide that. A response still buffered is dropped:
// Recovery's 500 replaces it. (A held body sets its gzip headers only
// in finish, so a panic never finds them set and unsent.)
func (w *compressWriter) abort() bool {
	if w.zw != nil {
		w.zw.Reset(nil)
		gzipWriters.Put(w.zw)
		w.zw = nil
	}
	w.buf.Reset()
	return w.headerSent
}

// abortedStream carries a handler's panic past a response whose
// headers were already sent. Recovery logs v and cuts the connection
// (http.ErrAbortHandler) instead of writing a 500. Without Recovery,
// net/http logs it and closes the connection all the same.
type abortedStream struct{ v any }

func (a abortedStream) Error() string { return fmt.Sprint(a.v) }

// Unwrap hands an outer recover the handler's own error.
func (a abortedStream) Unwrap() error { e, _ := a.v.(error); return e }

// addVary appends a token to Vary unless it is already listed.
func addVary(h http.Header, token string) {
	for _, v := range h.Values("Vary") {
		for part := range strings.SplitSeq(v, ",") {
			p := strings.TrimSpace(part)
			if p == "*" || strings.EqualFold(p, token) {
				return
			}
		}
	}
	h.Add("Vary", token)
}

// compressSeenMax bounds the doorkeeper set of body hashes seen once.
const compressSeenMax = 4096

// The compressed-copy cache for immutable responses: content-addressed
// by the SHA-256 of the identity body, so two URLs serving the same
// bytes share one entry and a changed body can never hit a stale one.
//
// A body enters only on its second sighting (the seen set is the
// doorkeeper), so one-off bodies from a client that mints immutable
// URLs (the component bundle takes any list of names) never displace
// the hot entries. A client that asks for each minted URL twice still
// can; the byte bound is what caps the cache then. Every sighting
// compresses at the same level: flate is deterministic, so the one
// strong "-gzip" ETag always names the same bytes, before admission
// and after an eviction alike.
var compressCache = struct {
	sync.Mutex
	entries map[[sha256.Size]byte][]byte
	order   [][sha256.Size]byte
	bytes   int
	seen    map[[sha256.Size]byte]struct{}
}{entries: map[[sha256.Size]byte][]byte{}, seen: map[[sha256.Size]byte]struct{}{}}

func cachedGzip(body []byte) []byte {
	key := sha256.Sum256(body)
	compressCache.Lock()
	if gz, ok := compressCache.entries[key]; ok {
		compressCache.Unlock()
		return gz
	}
	_, again := compressCache.seen[key]
	if !again {
		if len(compressCache.seen) >= compressSeenMax {
			clear(compressCache.seen)
		}
		compressCache.seen[key] = struct{}{}
	}
	compressCache.Unlock()

	gz := gzipOnce(body, gzip.BestCompression)
	if !again {
		return gz
	}

	compressCache.Lock()
	defer compressCache.Unlock()
	delete(compressCache.seen, key)
	if _, ok := compressCache.entries[key]; !ok && len(gz) <= compressCacheMaxBody {
		for compressCache.bytes+len(gz) > compressCacheMaxBytes && len(compressCache.order) > 0 {
			old := compressCache.order[0]
			compressCache.order = compressCache.order[1:]
			compressCache.bytes -= len(compressCache.entries[old])
			delete(compressCache.entries, old)
		}
		compressCache.entries[key] = gz
		compressCache.order = append(compressCache.order, key)
		compressCache.bytes += len(gz)
	}
	return gz
}

// gzipOnce compresses a held body in one pass, so the same bytes at the
// same level always encode the same way.
func gzipOnce(body []byte, level int) []byte {
	var out bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&out, level)
	_, _ = zw.Write(body)
	_ = zw.Close()
	return out.Bytes()
}

// compressCacheLen reports how many compressed copies are cached.
func compressCacheLen() int {
	compressCache.Lock()
	defer compressCache.Unlock()
	return len(compressCache.entries)
}
