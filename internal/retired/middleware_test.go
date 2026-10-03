package retired

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// serveMW runs one request through Middleware and returns what the
// reporter saw.
func serveMW(t *testing.T, handler http.HandlerFunc) (*httptest.ResponseRecorder, []string) {
	t.Helper()
	UseForTest(t, fixtureSet(t))
	var got []string
	ctx := renderdiagRetiredReporter(func(m string) { got = append(got, m) })
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/rpc", nil).WithContext(ctx)
	Middleware(func() bool { return false })(handler).ServeHTTP(rec, r)
	return rec, got
}

var wantPageFindings = []string{
	`retired markup: class "ui-button" (v9.0.0: the ui-button class is gone); run gofastr upgrade`,
	`retired markup: attr "data-fui-signal" (v9.0.0: the ui-button class is gone); run gofastr upgrade`,
}

// An island RPC answering with an HTML fragment is scanned like a page.
func TestMiddlewareScansHTMLFragment(t *testing.T) {
	_, got := serveMW(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<div class="ui-button"><span data-fui-signal>1</span></div>`)
	})
	if !equalStrings(got, wantPageFindings) {
		t.Fatalf("reports = %q, want %q", got, wantPageFindings)
	}
}

// A fragment with no Content-Type is sniffed, as net/http would.
func TestMiddlewareSniffsUntypedHTML(t *testing.T) {
	_, got := serveMW(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, page)
	})
	if !equalStrings(got, wantPageFindings) {
		t.Fatalf("reports = %q, want %q", got, wantPageFindings)
	}
}

// The runtime routes a text/plain RPC answer into an html-mode signal
// as markup, so text/plain is read as markup too.
func TestMiddlewareScansPlainText(t *testing.T) {
	_, got := serveMW(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, `<div class="ui-button"><span data-fui-signal>1</span></div>`)
	})
	if !equalStrings(got, wantPageFindings) {
		t.Fatalf("reports = %q, want %q", got, wantPageFindings)
	}
}

// Widget /state snapshots and JSON RPC answers carry html-mode signal
// values as JSON strings: each string holding markup is scanned, and a
// name found in two strings is reported once.
func TestMiddlewareScansJSONStrings(t *testing.T) {
	_, got := serveMW(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"list":  `<ul class="ui-button"><li data-fui-signal>a</li></ul>`,
			"again": []any{`<p class="ui-button">b</p>`, 3, true, nil},
			"count": 2,
		})
	})
	if !equalStrings(got, wantPageFindings) {
		t.Fatalf("reports = %q, want %q", got, wantPageFindings)
	}
}

// A JSON string with no markup is data, not a fragment: a record whose
// text happens to read "ui-button" is not a finding.
func TestMiddlewareJSONDataSilent(t *testing.T) {
	_, got := serveMW(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"title":"ui-button data-fui-signal","n":1}`)
	})
	if len(got) != 0 {
		t.Fatalf("plain JSON data reported: %q", got)
	}
}

// Stylesheets, scripts and binary bodies are not markup.
func TestMiddlewareSkipsOtherTypes(t *testing.T) {
	for _, ct := range []string{"text/css; charset=utf-8", "application/javascript", "image/svg+xml", "application/octet-stream", "text/markdown; charset=utf-8"} {
		_, got := serveMW(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ct)
			fmt.Fprint(w, `<div class="ui-button" data-fui-signal></div>`)
		})
		if len(got) != 0 {
			t.Errorf("%s body reported: %q", ct, got)
		}
	}
}

// An event stream is long-lived: the tee must not record it.
func TestMiddlewareSkipsEventStream(t *testing.T) {
	var recorded int
	serveMW(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: <div class=\"ui-button\"></div>\n\n")
		recorded = teeOf(w).buf.Len()
	})
	if recorded != 0 {
		t.Fatalf("event stream recorded %d bytes", recorded)
	}
}

// The tee exposes exactly the optional interfaces of the writer it
// wraps: a websocket upgrade asserting http.Hijacker must still find
// one, and a recorder (no Hijacker) must not grow one.
func TestMiddlewareKeepsWriterInterfaces(t *testing.T) {
	UseForTest(t, fixtureSet(t))
	mw := Middleware(func() bool { return false })
	check := func(name string, w http.ResponseWriter, wantFlush, wantHijack bool) {
		t.Helper()
		mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, fl := w.(http.Flusher)
			_, hj := w.(http.Hijacker)
			if fl != wantFlush || hj != wantHijack {
				t.Errorf("%s: Flusher=%v Hijacker=%v, want %v %v", name, fl, hj, wantFlush, wantHijack)
			}
		})).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	}
	check("recorder", httptest.NewRecorder(), true, false)
	check("hijackable", &hijackRecorder{ResponseRecorder: httptest.NewRecorder()}, true, true)
	check("bare", bareWriter{httptest.NewRecorder()}, false, false)
}

// After a hijack the connection belongs to the handler: nothing is
// scanned.
func TestMiddlewareStopsAtHijack(t *testing.T) {
	UseForTest(t, fixtureSet(t))
	var got []string
	ctx := renderdiagRetiredReporter(func(m string) { got = append(got, m) })
	hr := &hijackRecorder{ResponseRecorder: httptest.NewRecorder()}
	Middleware(func() bool { return false })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<div class="ui-button">`)
		if _, _, err := w.(http.Hijacker).Hijack(); err != nil {
			t.Fatal(err)
		}
	})).ServeHTTP(hr, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx))
	if !hr.hijacked {
		t.Fatal("Hijack did not reach the underlying writer")
	}
	if len(got) != 0 {
		t.Fatalf("hijacked response scanned: %q", got)
	}
}

// A router nested under another armed router scans each body once,
// even when a wrapper between the two hides the outer tee.
func TestMiddlewareNestedScansOnce(t *testing.T) {
	inner := Middleware(func() bool { return false })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, page)
	}))
	_, got := serveMW(t, func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(bareWriter{w}, r)
	})
	if !equalStrings(got, wantPageFindings) {
		t.Fatalf("reports = %q, want each finding once: %q", got, wantPageFindings)
	}
}

// An RPC that writes JSON with no Content-Type is sniffed as text; its
// markup sits behind escaped quotes, so it is walked as JSON anyway.
func TestMiddlewareUntypedJSON(t *testing.T) {
	_, got := serveMW(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"list":"<ul class=\"ui-button\"><li data-fui-signal>a</li></ul>"}`)
	})
	if !equalStrings(got, wantPageFindings) {
		t.Fatalf("reports = %q, want %q", got, wantPageFindings)
	}
}

// Production: the middleware hands the handler the writer it was given.
func TestMiddlewareProductionPassthrough(t *testing.T) {
	ResetForTest(t)
	rec := httptest.NewRecorder()
	var seen http.ResponseWriter
	h := Middleware(func() bool { return false })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = w
		fmt.Fprint(w, page)
	}))
	ProductionScan(t, h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if seen != http.ResponseWriter(rec) {
		t.Fatalf("production wrapped the writer: %T", seen)
	}
	if Loads() != 0 {
		t.Fatalf("production loaded the registry %d times", Loads())
	}
}

type hijackRecorder struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (h *hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	c1, c2 := net.Pipe()
	_ = c2.Close()
	return c1, bufio.NewReadWriter(bufio.NewReader(c1), bufio.NewWriter(c1)), nil
}

// bareWriter hides every optional interface, and Unwrap, of the writer
// it wraps: a middleware that wraps the response without exposing what
// is underneath.
type bareWriter struct{ rec http.ResponseWriter }

func (b bareWriter) Header() http.Header         { return b.rec.Header() }
func (b bareWriter) Write(p []byte) (int, error) { return b.rec.Write(p) }
func (b bareWriter) WriteHeader(code int)        { b.rec.WriteHeader(code) }

// teeOf finds the tee under reveal's wrappers.
func teeOf(w http.ResponseWriter) *teeWriter {
	if t, ok := w.(interface{ tee() *teeWriter }); ok {
		return t.tee()
	}
	return nil
}
