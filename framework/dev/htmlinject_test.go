package dev

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func injectedReq(t *testing.T, req *http.Request, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	LiveReloadHTMLInjector()(handler).ServeHTTP(rec, req)
	return rec
}

func injected(t *testing.T, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	return injectedReq(t, httptest.NewRequest(http.MethodGet, "/", nil), handler)
}

func TestInjectorAddsScriptBeforeBodyClose(t *testing.T) {
	rec := injected(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body><h1>raw file</h1></body></html>"))
	})
	body := rec.Body.String()
	if !strings.Contains(body, `<script src="`+LiveReloadScriptURL+`"`) {
		t.Fatalf("script not injected:\n%s", body)
	}
	if !strings.Contains(body, `</script></body></html>`) {
		t.Fatalf("script must land before </body>:\n%s", body)
	}
	if cl := rec.Header().Get("Content-Length"); cl != "" {
		if n, _ := strconv.Atoi(cl); n != len(body) {
			t.Fatalf("Content-Length %s does not match injected body %d", cl, len(body))
		}
	}
}

// Fragments — island RPC responses, SPA-nav partials (X-Gofastr-Partial), any
// HTML without </body> — are swapped into a live DOM by the runtime; an
// appended script node would corrupt :last-child styling and the screen
// cache. No </body>, no injection.
func TestInjectorLeavesFragmentsAlone(t *testing.T) {
	rec := injected(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("X-Gofastr-Partial", "true")
		_, _ = w.Write([]byte("<h1>fragment</h1>"))
	})
	if strings.Contains(rec.Body.String(), LiveReloadScriptURL) {
		t.Fatalf("fragment was injected:\n%s", rec.Body.String())
	}
	if rec.Body.String() != "<h1>fragment</h1>" {
		t.Fatalf("fragment bytes modified:\n%s", rec.Body.String())
	}
}

func TestInjectorSkipsPagesThatAlreadyHaveTheClient(t *testing.T) {
	page := `<html><body><script src="` + LiveReloadScriptURL + `" defer></script></body></html>`
	rec := injected(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	})
	if got := strings.Count(rec.Body.String(), LiveReloadScriptURL); got != 1 {
		t.Fatalf("uihost-style page double-injected (%d occurrences):\n%s", got, rec.Body.String())
	}
}

// A page that merely MENTIONS the livereload URL in prose or a code sample
// (the docs site rendering dev-livereload.md) still needs the real client.
func TestInjectorGuardMatchesScriptTagNotProse(t *testing.T) {
	page := `<html><body><p>The client lives at ` + LiveReloadScriptURL + `.</p></body></html>`
	rec := injected(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	})
	if !strings.Contains(rec.Body.String(), `<script src="`+LiveReloadScriptURL+`"`) {
		t.Fatalf("prose mention suppressed injection:\n%s", rec.Body.String())
	}
}

func TestInjectorPassesThroughNonHTMLUntouched(t *testing.T) {
	rec := injected(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	if rec.Code != http.StatusCreated || rec.Body.String() != `{"ok":true}` {
		t.Fatalf("non-HTML response modified: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

// Compressed bodies are opaque bytes even when labeled text/html — a host
// gzip middleware sitting inside the injector must pass through unmodified.
func TestInjectorSkipsEncodedBodies(t *testing.T) {
	rec := injected(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write([]byte("\x1f\x8bfake-gzip-bytes"))
	})
	if rec.Body.String() != "\x1f\x8bfake-gzip-bytes" {
		t.Fatalf("encoded body modified:\n%q", rec.Body.String())
	}
}

// HEAD responses (http.ServeFile sets the real file's Content-Length with no
// body) and Range requests must never be buffered or rewritten.
func TestInjectorLeavesHeadAndRangeAlone(t *testing.T) {
	head := injectedReq(t, httptest.NewRequest(http.MethodHead, "/page.html", nil), func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Length", "12345")
		w.WriteHeader(http.StatusOK)
	})
	if got := head.Header().Get("Content-Length"); got != "12345" {
		t.Fatalf("HEAD Content-Length clobbered: %q", got)
	}
	if head.Body.Len() != 0 {
		t.Fatalf("HEAD response grew a body: %q", head.Body.String())
	}

	rangeReq := httptest.NewRequest(http.MethodGet, "/page.html", nil)
	rangeReq.Header.Set("Range", "bytes=0-3")
	ranged := injectedReq(t, rangeReq, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Range", "bytes 0-3/100")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("<htm"))
	})
	if ranged.Body.String() != "<htm" || ranged.Code != http.StatusPartialContent {
		t.Fatalf("ranged response modified: code=%d body=%q", ranged.Code, ranged.Body.String())
	}
}

func TestInjectorStreamsSSEWithFlush(t *testing.T) {
	flushes := 0
	rec := httptest.NewRecorder()
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("injector must expose Flusher for non-HTML streams")
		}
		_, _ = w.Write([]byte("data: one\n\n"))
		f.Flush()
		flushes++
		_, _ = w.Write([]byte("data: two\n\n"))
		f.Flush()
		flushes++
	}
	LiveReloadHTMLInjector()(http.HandlerFunc(handler)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if flushes != 2 || !rec.Flushed {
		t.Fatalf("SSE stream not flushed through the injector (handler flushes=%d, recorder flushed=%v)", flushes, rec.Flushed)
	}
	if strings.Contains(rec.Body.String(), LiveReloadScriptURL) {
		t.Fatalf("SSE stream must never be injected:\n%s", rec.Body.String())
	}
}

// An explicit Flush on an HTML response is a streaming signal: the buffered
// prefix goes out raw, injection is abandoned, and later writes+flushes
// stream through — progressive server-rendered pages keep painting in dev.
func TestInjectorFlushSwitchesHTMLToStreaming(t *testing.T) {
	rec := httptest.NewRecorder()
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><p>first paint</p>"))
		w.(http.Flusher).Flush()
		if !rec.Flushed {
			t.Fatal("flush did not reach the wire — progressive HTML stopped painting")
		}
		if got := rec.Body.String(); got != "<html><body><p>first paint</p>" {
			t.Fatalf("buffered prefix not released on flush: %q", got)
		}
		_, _ = w.Write([]byte("<p>second paint</p></body></html>"))
	}
	LiveReloadHTMLInjector()(http.HandlerFunc(handler)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	if strings.Contains(body, LiveReloadScriptURL) {
		t.Fatalf("streamed HTML must not be spliced:\n%s", body)
	}
	if !strings.HasSuffix(body, "</body></html>") {
		t.Fatalf("streamed tail lost:\n%s", body)
	}
}

func TestInjectorPreservesHTMLStatusCode(t *testing.T) {
	rec := injected(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<html><body>not found</body></html>"))
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("HTML status rewritten: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), LiveReloadScriptURL) {
		t.Fatal("styled error pages are part of the dev loop too — inject them")
	}
}

// hijackableRecorder lets a WebSocket-style handler assert http.Hijacker
// through the wrapper (core/stream.Upgrade does exactly this).
type hijackableRecorder struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (h *hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	return nil, nil, errors.New("fake hijack: no real conn in tests")
}

func TestInjectorForwardsHijackForWebSockets(t *testing.T) {
	rec := &hijackableRecorder{ResponseRecorder: httptest.NewRecorder()}
	handler := func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("injector hides http.Hijacker — WebSocket upgrades break under gofastr dev")
		}
		_, _, _ = hj.Hijack()
	}
	LiveReloadHTMLInjector()(http.HandlerFunc(handler)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ws", nil))
	if !rec.hijacked {
		t.Fatal("Hijack not forwarded to the underlying writer")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("injector wrote to a hijacked response: %q", rec.Body.String())
	}
}
