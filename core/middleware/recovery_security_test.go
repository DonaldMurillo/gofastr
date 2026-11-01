package middleware

import (
	"bytes"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func discardRecovery() Middleware {
	return RecoveryFn(func() *slog.Logger { return slog.New(slog.DiscardHandler) })
}

// defaultChainFor wraps h the way framework.defaultMiddlewareChain does
// with compression, tracing and metrics on: Recovery → RequestID →
// Compress → Tracing → Metrics → SecurityHeaders → Timeout → handler.
func defaultChainFor(h http.Handler) http.Handler {
	mws := []Middleware{
		discardRecovery(), RequestID(), Compress(), Tracing(), MetricsMiddleware(NewMetrics()),
		SecurityHeaders(SecurityHeadersConfig{}), Timeout(time.Minute),
	}
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// TestRecoveryCutsCommittedResponse: Recovery wrote its 500 after the
// response had started (a status sent, body bytes flushed), so net/http
// dropped the status as superfluous and spliced "Internal Server Error"
// onto the handler's 200 body: a client, or a cache in front of it,
// took the spliced text as the real answer. Once the response started,
// Recovery now logs and cuts the connection (http.ErrAbortHandler), so
// the client sees a truncated response. The default chain matters:
// Timeout re-raises the handler goroutine's panic on the request
// goroutine, and Compress sits between them.
func TestRecoveryCutsCommittedResponse(t *testing.T) {
	flushThenPanic := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "partial-"+strings.Repeat("x", 2048))
		http.NewResponseController(w).Flush()
		panic("boom")
	})
	writeThenPanic := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, "partial-")
		panic("boom")
	})
	implicitThenPanic := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "partial-") // implicit 200
		panic("boom")
	})
	bareFlushThenPanic := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		http.NewResponseController(w).Flush() // sends the 200 head, no body
		panic("boom")
	})
	chains := map[string]http.Handler{
		"recovery, bare flush":    discardRecovery()(bareFlushThenPanic),
		"recovery, flush":         discardRecovery()(flushThenPanic),
		"recovery, write":         discardRecovery()(writeThenPanic),
		"recovery, implicit 200":  discardRecovery()(implicitThenPanic),
		"recovery+timeout, flush": discardRecovery()(Timeout(time.Minute)(flushThenPanic)),
		"default chain, flush":    defaultChainFor(flushThenPanic),
	}
	for name, chain := range chains {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewUnstartedServer(chain)
			srv.Config.ErrorLog = log.New(io.Discard, "", 0)
			srv.Start()
			t.Cleanup(srv.Close)
			resp, err := srv.Client().Get(srv.URL + "/")
			if err != nil {
				// Cut before net/http sent its buffered head: no response
				// at all, which is also not a spliced one.
				return
			}
			defer resp.Body.Close()
			raw, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
			if bytes.Contains(raw, []byte("Internal Server Error")) || rerr == nil {
				t.Fatalf("SECURITY: status %d body %q readErr=%v: a panic after the response started must cut the connection, not splice a 500 onto it",
					resp.StatusCode, raw, rerr)
			}
		})
	}
}

// TestTimeoutMarksStreamedPanicCut: the second layer of the same fix.
// Timeout re-raises a streaming handler's panic on the request
// goroutine; it now marks it abortedStream there, so any recovery above
// it (not only one that tracked the writer) knows a 500 cannot follow.
// A panic in a buffered handler stays unmarked: its 500 is still owed.
func TestTimeoutMarksStreamedPanicCut(t *testing.T) {
	for name, tc := range map[string]struct {
		flush   bool
		wantCut bool
	}{"streamed": {true, true}, "buffered": {false, false}} {
		t.Run(name, func(t *testing.T) {
			_, v := serveRecovered(Timeout(time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, "partial-")
				if tc.flush {
					http.NewResponseController(w).Flush()
				}
				panic("boom")
			})))
			val, _, cut := unwrapPanic(v)
			if val != "boom" || cut != tc.wantCut {
				t.Fatalf("re-raised %#v: value %v cut %v, want boom cut %v", v, val, cut, tc.wantCut)
			}
		})
	}
}

// TestRecovery500AfterHintsOnly: a 103 Early Hints is not the response,
// so a panic after it still gets the 500.
func TestRecovery500AfterHintsOnly(t *testing.T) {
	h := discardRecovery()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", "</a.css>; rel=preload")
		w.WriteHeader(http.StatusEarlyHints)
		panic("boom")
	}))
	if code, _ := getFinal(t, h); code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", code)
	}
}

// TestRecovery500DropsHandlerRepresentationHeaders: Recovery's 500 went
// out under the headers the handler had set for the response it meant
// to send. A pre-compressed immutable asset handler that panicked sent
// "Internal Server Error" as Content-Encoding: gzip (undecodable),
// Cache-Control: public, max-age=31536000, immutable (a year-long cached
// error page at every shared cache) and the asset's ETag. The 500 now
// drops the representation headers and is no-store; cookies stay.
func TestRecovery500DropsHandlerRepresentationHeaders(t *testing.T) {
	h := discardRecovery()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hd := w.Header()
		hd.Set("Content-Encoding", "gzip")
		hd.Set("Cache-Control", "public, max-age=31536000, immutable")
		hd.Set("ETag", `"asset-v1"`)
		hd.Set("Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT")
		hd.Set("Content-Length", "123456")
		hd.Set("Content-Type", "application/javascript")
		hd.Set("Content-Range", "bytes 0-99/1000")
		hd.Set("Accept-Ranges", "bytes")
		hd.Set("Content-Disposition", `attachment; filename="app.js"`)
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "rotated"})
		hd.Set("X-Request-Id", "req-1")
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app.3f2a.js", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", rec.Code)
	}
	for _, k := range []string{"Content-Encoding", "ETag", "Last-Modified", "Content-Length", "Content-Range", "Accept-Ranges", "Content-Disposition"} {
		if v := rec.Header().Get(k); v != "" {
			t.Errorf("SECURITY: 500 carries the handler's %s: %q", k, v)
		}
	}
	if v := rec.Header().Get("Cache-Control"); v != "no-store" {
		t.Errorf("SECURITY: 500 Cache-Control = %q, want no-store", v)
	}
	if v := rec.Header().Get("Content-Type"); strings.Contains(v, "javascript") {
		t.Errorf("500 Content-Type = %q, the handler's, not the error body's", v)
	}
	if !strings.Contains(rec.Header().Get("Set-Cookie"), "sid=rotated") || rec.Header().Get("X-Request-Id") != "req-1" {
		t.Errorf("500 dropped headers it should keep: %v", rec.Header())
	}
}

// TestRecoveryKeepsReaderFrom pins that Recovery's writer forwards
// ReadFrom (the sendfile path) and that a ReadFrom starts the response,
// so a later panic cuts instead of splicing a 500 onto the file.
func TestRecoveryKeepsReaderFrom(t *testing.T) {
	var sawRF bool
	h := Recovery()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rf, ok := w.(io.ReaderFrom)
		sawRF = ok
		if ok {
			_, _ = rf.ReadFrom(strings.NewReader("file-bytes"))
		}
		panic("after the file")
	}))
	rec := httptest.NewRecorder()
	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Fatalf("recover = %v, want http.ErrAbortHandler once ReadFrom sent bytes", v)
		}
		if !sawRF {
			t.Fatal("Recovery's writer hides io.ReaderFrom")
		}
		if got := rec.Body.String(); got != "file-bytes" {
			t.Fatalf("body = %q, want only the file bytes", got)
		}
	}()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
}
