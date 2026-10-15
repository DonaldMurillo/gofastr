package middleware

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	for range 2 {
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, gzipReq("/app.css?v=1"))
		if rec.Header().Get("Content-Encoding") != "gzip" {
			t.Fatal("immutable asset not compressed")
		}
		bodies = append(bodies, rec.Body.Bytes())
	}
	if calls != 2 {
		t.Fatalf("handler ran %d times; the cache must not skip the handler", calls)
	}
	if !bytes.Equal(bodies[0], bodies[1]) || gunzip(t, bodies[1]) != bigHTML {
		t.Fatal("cached compressed body differs or does not decode")
	}
	if cached := compressCacheLen(); cached == 0 {
		t.Fatal("immutable body was not cached")
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
