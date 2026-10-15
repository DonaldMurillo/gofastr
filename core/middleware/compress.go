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
// naming the gzip tag is rewritten back to the handler's tag before
// the handler runs, so a handler's own ETag comparison still answers
// 304, and the 304 echoes the gzip tag.
//
// A Flush from the handler flushes the gzip stream, so progressive
// rendering keeps painting.
//
// Responses marked Cache-Control: immutable are compressed once at
// the best level and the compressed copy is reused for the same bytes
// (keyed by the SHA-256 of the body, bounded in size). The handler
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
			}
			cw := &compressWriter{rw: w, accepts: accepts, revalidatingGzip: revalidatingGzip, status: http.StatusOK}
			next.ServeHTTP(cw, r)
			cw.finish()
		})
	}
}

const gzipETagSuffix = `-gzip"`

// acceptsGzip reports whether an Accept-Encoding value admits gzip:
// a gzip (or x-gzip, or *) coding whose q-value is not zero.
func acceptsGzip(header string) bool {
	if header == "" {
		return false
	}
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
		if q > 0 {
			return true
		}
	}
	return false
}

// stripGzipETags turns every gzip entity tag in an If-None-Match list
// back into the handler's tag ("x-gzip" → "x").
func stripGzipETags(inm string) string {
	return strings.ReplaceAll(inm, gzipETagSuffix, `"`)
}

// gzipETag derives the gzip representation's tag from the handler's.
func gzipETag(etag string) string {
	if !strings.HasSuffix(etag, `"`) || strings.HasSuffix(etag, gzipETagSuffix) {
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
	decided     bool
	encoding    bool // gzip chosen
	immutable   bool // buffer the whole body for the compressed-copy cache
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

func (w *compressWriter) WriteHeader(status int) {
	if w.headerSent || w.decided {
		return
	}
	if status >= 100 && status < 200 {
		w.rw.WriteHeader(status) // informational: forward, keep waiting
		return
	}
	w.status = status
}

func (w *compressWriter) Write(p []byte) (int, error) {
	if w.hijacked {
		return 0, http.ErrHijacked
	}
	if !w.decided {
		if w.Header().Get("Content-Type") == "" && w.buf.Len() == 0 && len(p) > 0 {
			// Do what net/http would do on its first write, so the
			// type decision sees the type the client will get.
			w.Header().Set("Content-Type", http.DetectContentType(p))
		}
		if !w.eligible() {
			w.decide(false)
			return w.rw.Write(p)
		}
		w.buf.Write(p)
		switch {
		case w.buf.Len() > compressCacheMaxBody:
			w.decide(true)
			if w.immutable { // too big to cache: stream it
				w.immutable = false
				w.startStream()
			}
		case w.buf.Len() >= compressMinBytes && !w.wantsImmutableBuffer():
			w.decide(true)
		}
		return len(p), nil
	}
	if w.passthrough {
		return w.rw.Write(p)
	}
	if w.immutable {
		w.buf.Write(p)
		if w.buf.Len() > compressCacheMaxBody {
			w.immutable = false // too big to cache: stream the rest
			w.startStream()
		}
		return len(p), nil
	}
	return w.zw.Write(p)
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

func (w *compressWriter) wantsImmutableBuffer() bool {
	return w.accepts && w.status == http.StatusOK &&
		strings.Contains(w.Header().Get("Cache-Control"), "immutable")
}

// decide commits the encoding choice and sends the headers. Any body
// held so far is written by the caller's path (finish or stream start).
func (w *compressWriter) decide(compress bool) {
	if w.decided {
		return
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
			_, _ = w.rw.Write(w.buf.Bytes())
			w.buf.Reset()
		}
		return
	}
	w.encoding = true
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	if et := h.Get("ETag"); et != "" {
		h.Set("ETag", gzipETag(et))
	}
	if w.wantsImmutableBuffer() {
		w.immutable = true
		return // headers go out with the cached body in finish
	}
	w.startStream()
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
		w.decide(w.eligible() && w.buf.Len() > 0)
	}
	if w.immutable {
		w.immutable = false // a flushing handler is streaming
		w.startStream()
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
	w.hijacked = true
	return http.NewResponseController(w.rw).Hijack()
}

// finish runs after the handler returns: it settles an undecided
// response and closes the gzip stream.
func (w *compressWriter) finish() {
	if w.hijacked {
		return
	}
	if !w.decided {
		big := w.buf.Len() >= compressMinBytes
		if big && w.wantsImmutableBuffer() && w.eligible() {
			w.decide(true)
		} else {
			if !big {
				w.Header().Del("Content-Length")
				if w.buf.Len() > 0 || w.status == http.StatusOK {
					w.Header().Set("Content-Length", strconv.Itoa(w.buf.Len()))
				}
			}
			w.decide(big && w.eligible())
		}
	}
	if w.immutable {
		body := cachedGzip(w.buf.Bytes())
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

// The compressed-copy cache for immutable responses: content-addressed
// by the SHA-256 of the identity body, so two URLs serving the same
// bytes share one entry and a changed body can never hit a stale one.
var compressCache = struct {
	sync.Mutex
	entries map[[sha256.Size]byte][]byte
	order   [][sha256.Size]byte
	bytes   int
}{entries: map[[sha256.Size]byte][]byte{}}

func cachedGzip(body []byte) []byte {
	key := sha256.Sum256(body)
	compressCache.Lock()
	if gz, ok := compressCache.entries[key]; ok {
		compressCache.Unlock()
		return gz
	}
	compressCache.Unlock()

	var out bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&out, gzip.BestCompression)
	_, _ = zw.Write(body)
	_ = zw.Close()
	gz := out.Bytes()

	compressCache.Lock()
	defer compressCache.Unlock()
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

// compressCacheLen reports how many compressed copies are cached.
func compressCacheLen() int {
	compressCache.Lock()
	defer compressCache.Unlock()
	return len(compressCache.entries)
}
