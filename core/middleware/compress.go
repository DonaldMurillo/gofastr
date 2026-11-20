package middleware

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

const compressMinBytes = 1024

const (
	compressCacheMaxBody  = 4 << 20
	compressCacheMaxBytes = 32 << 20
)

// Compress gzips text-like response bodies of at least 1 KiB for clients that accept gzip, giving the gzip representation its own ETag and Vary: Accept-Encoding.
func Compress() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead || r.Header.Get("Range") != "" {
				next.ServeHTTP(w, r)
				return
			}
			accepts := acceptsGzip(strings.Join(r.Header.Values("Accept-Encoding"), ","))
			revalidatingGzip := false
			if accepts {
				if inm := r.Header.Get("If-None-Match"); strings.Contains(inm, gzipETagSuffix) {
					revalidatingGzip = true
					//gofastr:allow(controlbytes) rewrites the inbound request's own header, already CTL-checked by net/http; it never reaches a response or a log.
					r.Header.Set("If-None-Match", stripGzipETags(inm))
				}
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
				if cw.abort() {
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

func stripGzipETags(inm string) string {
	return strings.ReplaceAll(inm, gzipETagSuffix, `"`)
}

func gzipETag(etag string) string {
	if !strings.HasSuffix(etag, `"`) {
		return etag
	}
	return strings.TrimSuffix(etag, `"`) + gzipETagSuffix
}

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

type compressWriter struct {
	rw               http.ResponseWriter
	accepts          bool
	revalidatingGzip bool

	status      int
	wroteHeader bool
	decided     bool
	encoding    bool
	held        bool
	cacheable   bool
	passthrough bool
	hijacked    bool
	headerSent  bool
	buf         bytes.Buffer
	zw          *gzip.Writer
}

func (w *compressWriter) Header() http.Header { return w.rw.Header() }

func (w *compressWriter) Unwrap() http.ResponseWriter { return w.rw }

func (w *compressWriter) WriteHeader(status int) {
	if w.wroteHeader || w.headerSent || w.decided {
		return
	}
	if status >= 100 && status < 200 {
		w.rw.WriteHeader(status)
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
				return len(p), nil
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
			if w.held {
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
			w.spill(false)
		}
		return len(p), nil
	}
	return w.zw.Write(p)
}

func (w *compressWriter) ReadFrom(source io.Reader) (int64, error) {
	if w.hijacked {
		return 0, http.ErrHijacked
	}
	var copied int64
	if !w.decided {
		firstChunk := int64(1)
		if w.needsSniff() {
			firstChunk = int64(sniffLen - w.buf.Len())
		}
		var err error
		copied, err = io.Copy(struct{ io.Writer }{w}, io.LimitReader(source, firstChunk))
		if err != nil || copied < firstChunk {
			return copied, err
		}
	}
	if !w.decided && (!w.accepts || !w.eligible()) {
		if err := w.decide(false); err != nil {
			return copied, err
		}
	}
	var rest int64
	var err error
	if w.passthrough {
		rest, err = io.Copy(w.rw, source)
	} else {
		rest, err = io.Copy(struct{ io.Writer }{w}, source)
	}
	return copied + rest, err
}

const sniffLen = 512

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

func (w *compressWriter) sniff() {
	if w.buf.Len() > 0 && w.needsSniff() {
		w.Header().Set("Content-Type", http.DetectContentType(w.buf.Bytes()))
	}
}

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

func (w *compressWriter) holdsWhole() bool {
	if !w.accepts || w.status != http.StatusOK {
		return false
	}
	h := w.Header()
	if cacheControlImmutable(h) {
		return true
	}
	et := h.Get("ETag")
	return et != "" && !strings.HasPrefix(et, "W/")
}

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
	h.Del("Accept-Ranges")
	h.Set("Content-Encoding", "gzip")
	if et := h.Get("ETag"); et != "" {
		h.Set("ETag", gzipETag(et))
	}
	if w.holdsWhole() {
		w.held = true
		w.cacheable = cacheControlImmutable(h)
		return nil
	}
	w.startStream()
	return nil
}

func cacheControlImmutable(h http.Header) bool {
	return strings.Contains(strings.Join(h.Values("Cache-Control"), ","), "immutable")
}

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
		w.hijacked = true
	}
	return conn, rw, err
}

func (w *compressWriter) finish() {
	if w.hijacked {
		return
	}
	if !w.decided {
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

func (w *compressWriter) abort() bool {
	if w.zw != nil {
		w.zw.Reset(nil)
		gzipWriters.Put(w.zw)
		w.zw = nil
	}
	w.buf.Reset()
	return w.headerSent
}

type abortedStream struct{ v any }

func (a abortedStream) Error() string { return fmt.Sprint(a.v) }

func (a abortedStream) Unwrap() error { e, _ := a.v.(error); return e }

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

const compressSeenMax = 4096

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

func gzipOnce(body []byte, level int) []byte {
	var out bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&out, level)
	_, _ = zw.Write(body)
	_ = zw.Close()
	return out.Bytes()
}

func compressCacheLen() int {
	compressCache.Lock()
	defer compressCache.Unlock()
	return len(compressCache.entries)
}
