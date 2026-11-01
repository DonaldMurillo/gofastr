package middleware

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var bigHTML = "<!DOCTYPE html><html><body>" + strings.Repeat("<p>hello compressed world</p>", 200) + "</body></html>"

func gunzip(t *testing.T, b []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	return string(out)
}

func serveCompress(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	Compress()(h).ServeHTTP(rec, req)
	return rec
}

func htmlHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, body)
	})
}

func gzipReq(target string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.Header.Set("Accept-Encoding", "gzip, deflate, br")
	return r
}

func TestCompressGzipsLargeHTML(t *testing.T) {
	rec := serveCompress(htmlHandler(bigHTML), gzipReq("/"))
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("Vary = %q, want Accept-Encoding", rec.Header().Values("Vary"))
	}
	if rec.Header().Get("Content-Length") != "" {
		t.Fatalf("Content-Length kept on a compressed body: %q", rec.Header().Get("Content-Length"))
	}
	if rec.Body.Len() >= len(bigHTML)/4 {
		t.Fatalf("compressed %d bytes from %d, expected a real reduction", rec.Body.Len(), len(bigHTML))
	}
	if got := gunzip(t, rec.Body.Bytes()); got != bigHTML {
		t.Fatal("decompressed body differs from the handler's")
	}
}

func TestCompressSkipsWithoutAcceptGzip(t *testing.T) {
	for _, ae := range []string{"", "br", "identity", "gzip;q=0", "gzip; q=0.0, br"} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if ae != "" {
			r.Header.Set("Accept-Encoding", ae)
		}
		rec := serveCompress(htmlHandler(bigHTML), r)
		if rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != bigHTML {
			t.Fatalf("Accept-Encoding %q: got encoding %q", ae, rec.Header().Get("Content-Encoding"))
		}
		if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
			t.Fatalf("Accept-Encoding %q: identity answer of a compressible type must still Vary", ae)
		}
	}
}

func TestCompressAcceptsGzipWithQuality(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "br;q=1.0, GZIP;q=0.5")
	if rec := serveCompress(htmlHandler(bigHTML), r); rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("gzip;q=0.5 should compress, got %q", rec.Header().Get("Content-Encoding"))
	}
}

func TestCompressSkipsSmallBody(t *testing.T) {
	rec := serveCompress(htmlHandler("<p>tiny</p>"), gzipReq("/"))
	if rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != "<p>tiny</p>" {
		t.Fatalf("small body compressed: encoding %q", rec.Header().Get("Content-Encoding"))
	}
}

func TestCompressSkipsBinaryTypes(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, bigHTML)
	})
	if rec := serveCompress(h, gzipReq("/x.png")); rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("image/png compressed")
	}
}

func TestCompressSniffsUntypedBody(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, bigHTML)
	})
	rec := serveCompress(h, gzipReq("/"))
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("sniffed text/html body not compressed")
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("Content-Type = %q, want the sniffed text/html", rec.Header().Get("Content-Type"))
	}
}

func TestCompressLeavesEncodedBodies(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Encoding", "br")
		_, _ = io.WriteString(w, bigHTML)
	})
	rec := serveCompress(h, gzipReq("/"))
	if rec.Header().Get("Content-Encoding") != "br" || rec.Body.String() != bigHTML {
		t.Fatal("already-encoded body was re-encoded")
	}
}

func TestCompressSkipsRangeAndHead(t *testing.T) {
	r := gzipReq("/")
	r.Header.Set("Range", "bytes=0-10")
	if rec := serveCompress(htmlHandler(bigHTML), r); rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("Range request compressed")
	}
	head := gzipReq("/")
	head.Method = http.MethodHead
	if rec := serveCompress(htmlHandler(bigHTML), head); rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("HEAD request compressed")
	}
}

func TestCompressSkipsEventStream(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: "+bigHTML+"\n\n")
		w.(http.Flusher).Flush()
	})
	rec := serveCompress(h, gzipReq("/sse"))
	if rec.Header().Get("Content-Encoding") != "" || !rec.Flushed {
		t.Fatalf("SSE: encoding %q flushed %v", rec.Header().Get("Content-Encoding"), rec.Flushed)
	}
}

func TestCompressFlushSendsDecodablePrefix(t *testing.T) {
	first := strings.Repeat("<li>first chunk</li>", 100)
	var mid []byte
	var rec *httptest.ResponseRecorder
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, first)
		w.(http.Flusher).Flush()
		mid = append([]byte(nil), rec.Body.Bytes()...)
		_, _ = io.WriteString(w, "<li>tail</li>")
	})
	rec = httptest.NewRecorder()
	Compress()(h).ServeHTTP(rec, gzipReq("/"))
	zr, err := gzip.NewReader(bytes.NewReader(mid))
	if err != nil {
		t.Fatalf("flushed prefix is not a gzip stream: %v", err)
	}
	got := make([]byte, len(first))
	if _, err := io.ReadFull(zr, got); err != nil || string(got) != first {
		t.Fatalf("flushed prefix did not decode to the first chunk: %v", err)
	}
	if gunzip(t, rec.Body.Bytes()) != first+"<li>tail</li>" {
		t.Fatal("full body mismatch after flush")
	}
}

func etagHandler(etag string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = io.WriteString(w, bigHTML)
	})
}

func TestCompressGivesGzipItsOwnETag(t *testing.T) {
	rec := serveCompress(etagHandler(`"abc"`), gzipReq("/a.js"))
	if got := rec.Header().Get("ETag"); got != `"abc-gzip"` {
		t.Fatalf("ETag = %q, want the gzip representation's own tag", got)
	}
	plain := httptest.NewRequest(http.MethodGet, "/a.js", nil)
	if got := serveCompress(etagHandler(`"abc"`), plain).Header().Get("ETag"); got != `"abc"` {
		t.Fatalf("identity ETag = %q, want unchanged", got)
	}
}

func TestCompressRevalidatesGzipETag(t *testing.T) {
	r := gzipReq("/a.js")
	r.Header.Set("If-None-Match", `"abc-gzip"`)
	rec := serveCompress(etagHandler(`"abc"`), r)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304 for the gzip tag", rec.Code)
	}
	if got := rec.Header().Get("ETag"); got != `"abc-gzip"` {
		t.Fatalf("304 ETag = %q, want the gzip tag echoed", got)
	}
	if rec.Header().Get("Content-Encoding") != "" || rec.Body.Len() != 0 {
		t.Fatal("304 carried an encoding or a body")
	}
}

func TestCompressReusesImmutableBodies(t *testing.T) {
	calls := 0
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/css")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		_, _ = io.WriteString(w, bigHTML)
	})
	mw := Compress()(h)
	var bodies [][]byte
	for range 3 {
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, gzipReq("/app.css?v=1"))
		if rec.Header().Get("Content-Encoding") != "gzip" {
			t.Fatal("immutable asset not compressed")
		}
		bodies = append(bodies, rec.Body.Bytes())
	}
	if calls != 3 {
		t.Fatalf("handler ran %d times; the cache must not skip the handler", calls)
	}
	if gunzip(t, bodies[0]) != bigHTML {
		t.Fatal("first compressed body does not decode")
	}
	if !bytes.Equal(bodies[1], bodies[2]) || gunzip(t, bodies[2]) != bigHTML {
		t.Fatal("cached compressed body differs or does not decode")
	}
	if cached := compressCacheLen(); cached == 0 {
		t.Fatal("immutable body was not cached")
	}
}

// An immutable body is cached at the best level; a handler that flushes
// streams at the default level instead. The two encodings differ, so
// the streamed one cannot carry the strong tag that names the cached
// bytes.
func TestCompressFlushedImmutableTagIsWeak(t *testing.T) {
	serve := func(flush bool) *httptest.ResponseRecorder {
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/css")
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			w.Header().Set("ETag", `"v1"`)
			_, _ = io.WriteString(w, bigHTML[:len(bigHTML)/2])
			if flush {
				w.(http.Flusher).Flush()
			}
			_, _ = io.WriteString(w, bigHTML[len(bigHTML)/2:])
		})
		return serveCompress(h, gzipReq("/app.css?v=1"))
	}
	buffered, flushed := serve(false), serve(true)
	if gunzip(t, buffered.Body.Bytes()) != bigHTML || gunzip(t, flushed.Body.Bytes()) != bigHTML {
		t.Fatal("a body does not decode")
	}
	if bytes.Equal(buffered.Body.Bytes(), flushed.Body.Bytes()) {
		t.Fatal("the encodings match; the test no longer covers two levels")
	}
	if got := buffered.Header().Get("ETag"); got != `"v1-gzip"` {
		t.Errorf("buffered ETag = %q, want the strong gzip tag", got)
	}
	if got := flushed.Header().Get("ETag"); got != `W/"v1-gzip"` {
		t.Errorf("flushed ETag = %q, want it weakened", got)
	}
}

type hijackRecorder struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (h *hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	return nil, nil, nil
}

func TestCompressForwardsHijack(t *testing.T) {
	rec := &hijackRecorder{ResponseRecorder: httptest.NewRecorder()}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := http.NewResponseController(w).Hijack(); err != nil {
			t.Fatalf("hijack: %v", err)
		}
	})
	Compress()(h).ServeHTTP(rec, gzipReq("/ws"))
	if !rec.hijacked {
		t.Fatal("Hijack did not reach the underlying writer")
	}
}

func TestCompressKeepsFirstStatus(t *testing.T) {
	twice := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusCreated)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "ok")
	})
	if rec := serveCompress(twice, gzipReq("/")); rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want the first WriteHeader (201)", rec.Code)
	}
	writeFirst := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "ok")
		w.WriteHeader(http.StatusInternalServerError)
	})
	if rec := serveCompress(writeFirst, gzipReq("/")); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: the first Write commits it", rec.Code)
	}
}

func TestCompressHonorsGzipQZero(t *testing.T) {
	for _, ae := range []string{"gzip;q=0, *;q=1", "*;q=1, gzip;q=0", "x-gzip;q=0, *"} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Accept-Encoding", ae)
		if got := serveCompress(htmlHandler(bigHTML), r).Header().Get("Content-Encoding"); got != "" {
			t.Errorf("Accept-Encoding %q: Content-Encoding %q, want identity", ae, got)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "br, *;q=0.1")
	if got := serveCompress(htmlHandler(bigHTML), r).Header().Get("Content-Encoding"); got != "gzip" {
		t.Errorf("wildcard alone: Content-Encoding %q, want gzip", got)
	}
}

// TestCompressIfMatchGzipTag: Compress hands gzip-accepting clients the
// tag "x-gzip" and mapped it back to "x" for If-None-Match only. An
// optimistic-concurrency PUT echoing that tag in If-Match reached the
// handler unchanged and got 412 on every write. If-Match now gets the
// same rewrite.
func TestCompressIfMatchGzipTag(t *testing.T) {
	const tag = `"rev-7"`
	body := `{"items":[` + strings.Repeat(`{"name":"widget","qty":1},`, 100) + `{}]}`
	h := Compress()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			if r.Header.Get("If-Match") != tag {
				w.WriteHeader(http.StatusPreconditionFailed)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", tag)
		_, _ = io.WriteString(w, body)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, gzipReq("/items"))
	issued := rec.Header().Get("ETag")
	if rec.Header().Get("Content-Encoding") != "gzip" || issued != `"rev-7-gzip"` {
		t.Fatalf("premise: GET not gzipped under the gzip tag (ce=%q etag=%q)", rec.Header().Get("Content-Encoding"), issued)
	}
	put := httptest.NewRequest(http.MethodPut, "/items", strings.NewReader(`{}`))
	put.Header.Set("Accept-Encoding", "gzip")
	put.Header.Set("If-Match", issued)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, put)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("PUT If-Match %s (the tag Compress issued) answered %d, want 204", issued, rec.Code)
	}
}

type noHijackRecorder struct{ *httptest.ResponseRecorder }

func TestCompressFailedHijackKeepsWriter(t *testing.T) {
	rec := noHijackRecorder{httptest.NewRecorder()}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := http.NewResponseController(w).Hijack(); err == nil {
			t.Fatal("premise: the recorder cannot hijack")
		}
		http.Error(w, "upgrade unavailable", http.StatusNotImplemented)
	})
	Compress()(h).ServeHTTP(rec, gzipReq("/ws"))
	if rec.Code != http.StatusNotImplemented || !strings.Contains(rec.Body.String(), "upgrade unavailable") {
		t.Fatalf("after a failed hijack: %d %q, want the handler's 501", rec.Code, rec.Body.String())
	}
}

func TestCompressSuffixesGzipLookingETag(t *testing.T) {
	rec := serveCompress(etagHandler(`"v1-gzip"`), gzipReq("/a.js"))
	tag := rec.Header().Get("ETag")
	if tag != `"v1-gzip-gzip"` {
		t.Fatalf("ETag = %q, want the suffix added even to a tag ending in -gzip", tag)
	}
	r := gzipReq("/a.js")
	r.Header.Set("If-None-Match", tag)
	rec = serveCompress(etagHandler(`"v1-gzip"`), r)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("revalidating %s: status %d, want 304", tag, rec.Code)
	}
	if got := rec.Header().Get("ETag"); got != tag {
		t.Fatalf("304 ETag = %q, want %q", got, tag)
	}
}

func TestCompressSniffsBufferedBytes(t *testing.T) {
	// A template that opens with a newline before its doctype: the
	// first Write alone sniffs as text/plain, the buffered 512 bytes as
	// HTML, which is what net/http would send without Compress.
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "\n")
		_, _ = io.WriteString(w, bigHTML)
	})
	rec := serveCompress(h, gzipReq("/"))
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html sniffed from the buffered body", ct)
	}
	if rec.Header().Get("Content-Encoding") != "gzip" || gunzip(t, rec.Body.Bytes()) != "\n"+bigHTML {
		t.Fatal("body not compressed intact")
	}
	small := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, " ")
		_, _ = io.WriteString(w, "<!DOCTYPE html><p>hi</p>")
	})
	if ct := serveCompress(small, gzipReq("/")).Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("small body: Content-Type = %q, want text/html", ct)
	}
}

func TestCompressSniffsOnlyWhereNetHTTPDoes(t *testing.T) {
	encoded := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "br")
		_, _ = io.WriteString(w, "\x1f\x8b\x08 not really")
	})
	// A real server, not a recorder: httptest.ResponseRecorder sniffs
	// even an encoded body, net/http does not (go.dev/issue/31753).
	srv := httptest.NewServer(Compress()(encoded))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		t.Errorf("encoded body: Content-Type = %q, want none (net/http does not sniff it)", ct)
	}
	suppressed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Content-Type"] = nil
		_, _ = io.WriteString(w, bigHTML)
	})
	rec := serveCompress(suppressed, gzipReq("/"))
	if _, ok := rec.Header()["Content-Type"]; ok && rec.Header().Get("Content-Type") != "" {
		t.Errorf("suppressed type: Content-Type = %q, want none", rec.Header().Get("Content-Type"))
	}
}

func TestCompressDropsAcceptRanges(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.html", time.Time{}, strings.NewReader(bigHTML))
	})
	rec := serveCompress(h, gzipReq("/a.html"))
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("premise: the body was not compressed")
	}
	if ar := rec.Header().Get("Accept-Ranges"); ar != "" {
		t.Fatalf("Accept-Ranges = %q on a gzip body: a resumed range would be identity bytes", ar)
	}
	plain := httptest.NewRequest(http.MethodGet, "/a.html", nil)
	if serveCompress(h, plain).Header().Get("Accept-Ranges") != "bytes" {
		t.Fatal("identity response lost Accept-Ranges")
	}
}

func TestCompressSniffsAtFlush(t *testing.T) {
	// Progressive rendering with no Content-Type: the early Flush
	// commits the headers, so the type is sniffed from what is
	// buffered then, as net/http does, and the stream is compressed.
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "<!DOCTYPE html><html><head><title>t</title></head>")
		http.NewResponseController(w).Flush()
		_, _ = io.WriteString(w, bigHTML)
	})
	rec := serveCompress(h, gzipReq("/"))
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("streamed HTML was not compressed")
	}
}

func TestCompressCachesOnSecondSighting(t *testing.T) {
	body := bigHTML + "<!-- one-off -->"
	before := compressCacheLen()
	cachedGzip([]byte(body))
	if got := compressCacheLen(); got != before {
		t.Fatalf("a body seen once entered the cache (%d → %d)", before, got)
	}
	cachedGzip([]byte(body))
	if got := compressCacheLen(); got != before+1 {
		t.Fatalf("a body seen twice is not cached (%d → %d)", before, got)
	}
}

func TestCompressSightingsShareBytes(t *testing.T) {
	// One strong "-gzip" ETag labels every compressed copy of a body, so
	// the first sighting (not cached) and the second (cached) must be
	// the same bytes.
	var b strings.Builder
	for i := range 4000 {
		fmt.Fprintf(&b, "<li data-n=%d>row %d of %d</li>", i*7919%1000, i, i%13)
	}
	first := cachedGzip([]byte(b.String()))
	second := cachedGzip([]byte(b.String()))
	if !bytes.Equal(first, second) {
		t.Fatalf("one body, two gzip encodings under one ETag (%d vs %d bytes)", len(first), len(second))
	}
}

func TestCompressPanicMidStreamCutsTheResponse(t *testing.T) {
	// A panic after the gzip stream started: the status line and part
	// of the body are on the wire, so no 500 can follow. The connection
	// is cut, which the client reads as a truncated response, instead
	// of plain text landing after the compressed bytes.
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, bigHTML)
		http.NewResponseController(w).Flush()
		panic("boom")
	})
	srv := httptest.NewUnstartedServer(RecoveryFn(func() *slog.Logger { return slog.New(slog.DiscardHandler) })(Compress()(h)))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.Start()
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatal("premise: the stream had not started")
	}
	raw, err := io.ReadAll(resp.Body)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("reading the body: err = %v, want a truncated response", err)
	}
	if bytes.Contains(raw, []byte("Internal Server Error")) {
		t.Fatal("the 500 text was appended to the gzip stream")
	}
}

func TestRecoveryLetsAbortHandlerThrough(t *testing.T) {
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })
	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Fatalf("recovered %v, want http.ErrAbortHandler re-raised for net/http", v)
		}
	}()
	RecoveryFn(func() *slog.Logger { return slog.New(slog.DiscardHandler) })(h).ServeHTTP(httptest.NewRecorder(), gzipReq("/"))
	t.Fatal("Recovery swallowed http.ErrAbortHandler")
}

func TestCompressFlushedTaggedBodyTagIsWeak(t *testing.T) {
	// A tagged body that is not immutable: held whole, so a request
	// that does not flush gets one deterministic encoding under the
	// strong tag, and one that flushes streams under a weak one.
	serve := func(flush bool) *httptest.ResponseRecorder {
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("ETag", `"v1"`)
			_, _ = io.WriteString(w, bigHTML[:len(bigHTML)/2])
			if flush {
				w.(http.Flusher).Flush()
			}
			_, _ = io.WriteString(w, bigHTML[len(bigHTML)/2:])
		})
		return serveCompress(h, gzipReq("/page"))
	}
	buffered, flushed := serve(false), serve(true)
	if gunzip(t, buffered.Body.Bytes()) != bigHTML || gunzip(t, flushed.Body.Bytes()) != bigHTML {
		t.Fatal("a body does not decode")
	}
	if got := buffered.Header().Get("ETag"); got != `"v1-gzip"` {
		t.Errorf("buffered ETag = %q, want the strong gzip tag", got)
	}
	if got := flushed.Header().Get("ETag"); got != `W/"v1-gzip"` {
		t.Errorf("flushed ETag = %q, want it weakened", got)
	}
}

func TestCompressPanicBeforeDecisionIs500(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<p>partial</p>")
		panic("boom")
	})
	rec := httptest.NewRecorder()
	RecoveryFn(func() *slog.Logger { return slog.New(slog.DiscardHandler) })(Compress()(h)).ServeHTTP(rec, gzipReq("/"))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "partial") {
		t.Fatalf("panic while buffering: %d %q, want a clean 500", rec.Code, rec.Body.String())
	}
}

func TestCompressImmutablePanicIsPlain500(t *testing.T) {
	// An immutable body is held whole until the handler returns. A
	// panic first must leave no gzip label for Recovery's plain 500:
	// http.Error keeps Content-Encoding (go.dev/issue/66343).
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("ETag", `"v1"`)
		_, _ = io.WriteString(w, bigHTML)
		panic("boom")
	})
	rec := httptest.NewRecorder()
	RecoveryFn(func() *slog.Logger { return slog.New(slog.DiscardHandler) })(Compress()(h)).ServeHTTP(rec, gzipReq("/a.css"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if ce := rec.Header().Get("Content-Encoding"); ce != "" {
		t.Fatalf("the 500's Content-Encoding = %q over a plain-text body", ce)
	}
}
