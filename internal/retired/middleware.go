package retired

import (
	"bufio"
	"bytes"
	"context"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/renderdiag"
)

// scanCap bounds how much of a response body the tee records. Pages are
// finite and whole; the cap only keeps a pathological dev-mode response
// from parking an unbounded buffer. The scan reads what was recorded.
const scanCap = 16 << 20

// Middleware arms the retired-markup scan for every response through
// next: pages, partials, island RPC answers, widget chrome and state,
// anything an app mounts. framework.App installs it on its router and
// uihost's standalone router installs it too; it is the only hook.
// Which bodies are read is decided by Content-Type (see classify), so
// stylesheets, scripts and streams pass through unrecorded.
//
// devMode is the `gofastr dev` predicate (framework/dev's Enabled),
// asked per request so this package stays leaf-level. In a Go test
// binary responses are always armed; in production the handler gets
// the writer it was given and the registry is never loaded.
func Middleware(devMode func() bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// A router nested under another armed router: the outer tee
			// already sees every byte, and a second scan would double
			// every report. The mark rides the request, because a
			// wrapper between the two may hide the tee.
			if r.Context().Value(armedKey{}) != nil || !armed(r.Context(), devMode) {
				next.ServeHTTP(w, r)
				return
			}
			tee := &teeWriter{ResponseWriter: w}
			r = r.WithContext(context.WithValue(r.Context(), armedKey{}, true))
			defer reportFindings(tee, r, devMode())
			next.ServeHTTP(reveal(tee), r)
		})
	}
}

// armedKey marks a request an outer Middleware already tees.
type armedKey struct{}

// armed is the one gate: a Go test binary or `gofastr dev`, minus the
// per-request production exemption a test can install.
func armed(ctx context.Context, devMode func() bool) bool {
	if d, ok := ctx.Value(productionScanKey{}).(*atomic.Bool); ok && d.Load() {
		return false
	}
	return testing.Testing() || devMode()
}

// reportFindings scans the recorded body and delivers each finding.
// With no reporter and dev mode on, each (path, name) is logged once
// per process: `gofastr dev`'s livereload re-serves the same page after
// every edit, and the console must not drown in repeats. A reporter
// (a test harness) sees every finding of every response.
func reportFindings(tee *teeWriter, r *http.Request, dev bool) {
	if tee.hijacked || tee.buf.Len() == 0 {
		return
	}
	switch tee.kind {
	case kindMarkup, kindJSON:
	default:
		return
	}
	set := Current()
	if !set.nonEmpty {
		return
	}
	ctx := r.Context()
	reporter := renderdiag.HasRetiredReporter(ctx)
	for _, f := range set.checkBody(tee.kind, tee.buf.Bytes()) {
		if !reporter && dev && devDedupe(r.URL.Path, f) {
			continue
		}
		renderdiag.ReportRetired(ctx, f.Message())
	}
}

// devWarned remembers which (path, finding) pairs were already logged
// in dev mode.
var devWarned sync.Map

type devWarnKey struct {
	path string
	kind string
	name string
}

func devDedupe(path string, f Finding) (skip bool) {
	key := devWarnKey{path: path, kind: f.Kind, name: f.Name}
	if _, loaded := devWarned.LoadOrStore(key, struct{}{}); loaded {
		return true
	}
	return false
}

type productionScanKey struct{}

// ProductionScan exempts requests through next from the retired-markup
// scan the way renderdiag.ProductionStatus exempts them from the
// test-only 500: a test can drive the production posture (no scan, no
// registry load) inside a test binary. The exemption ends at t.Cleanup,
// is invisible to any HTTP request, and pans outside a test binary.
func ProductionScan(t testing.TB, next http.Handler) http.Handler {
	t.Helper()
	if !testing.Testing() {
		panic("retired: ProductionScan requires a test binary")
	}
	active := new(atomic.Bool)
	active.Store(true)
	t.Cleanup(func() { active.Store(false) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), productionScanKey{}, active)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// bodyKind is how a response body is read, decided from its
// Content-Type at the first WriteHeader or Write.
type bodyKind int

const (
	kindUndecided bodyKind = iota
	kindMarkup             // text/html, xhtml, text/plain: start tags
	kindJSON               // JSON: each string value holding markup
	kindSkip               // anything else: not recorded
)

// classify maps a Content-Type to a bodyKind. text/plain is markup
// because the runtime applies a text RPC answer to an html-mode signal
// as markup; JSON because widget state and JSON RPC answers carry
// html-mode signal values as strings. Event streams, stylesheets,
// scripts, images and downloads are skipped and never recorded.
func classify(contentType string) bodyKind {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return kindSkip
	}
	switch {
	case mt == "text/html", mt == "application/xhtml+xml", mt == "text/plain":
		return kindMarkup
	case mt == "application/json", strings.HasSuffix(mt, "+json"):
		return kindJSON
	}
	return kindSkip
}

// teeWriter records what the handler writes so finish can scan it.
// Every method passes through unchanged: a retired name is a migration
// finding, never a change to the response. reveal hands the handler a
// wrapper exposing exactly the optional interfaces w has.
type teeWriter struct {
	http.ResponseWriter
	buf      bytes.Buffer
	capped   bool
	kind     bodyKind
	hijacked bool
}

func (w *teeWriter) tee() *teeWriter { return w }

// decide fixes the body kind from the Content-Type header, sniffing the
// first chunk the way net/http does when the handler set none. A
// WriteHeader with no Content-Type leaves the decision to the first
// Write, where net/http sniffs too.
func (w *teeWriter) decide(first []byte) {
	if w.kind != kindUndecided {
		return
	}
	ct := w.Header().Get("Content-Type")
	if ct == "" {
		if first == nil {
			return
		}
		ct = http.DetectContentType(first)
	}
	w.kind = classify(ct)
}

func (w *teeWriter) WriteHeader(code int) {
	w.decide(nil)
	w.ResponseWriter.WriteHeader(code)
}

func (w *teeWriter) Write(p []byte) (int, error) {
	w.decide(p)
	if (w.kind == kindMarkup || w.kind == kindJSON) && !w.capped && !w.hijacked {
		if room := scanCap - w.buf.Len(); room > 0 {
			if len(p) > room {
				w.buf.Write(p[:room])
				w.capped = true
			} else {
				w.buf.Write(p)
			}
		} else {
			w.capped = true
		}
	}
	return w.ResponseWriter.Write(p)
}

// Unwrap lets http.NewResponseController reach the real writer; without
// it a Flush (or any future controller capability) would silently fail
// only where the scan is armed.
func (w *teeWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

type teeFlusher struct{ t *teeWriter }

func (f teeFlusher) Flush() { f.t.ResponseWriter.(http.Flusher).Flush() }

type teeHijacker struct{ t *teeWriter }

// Hijack hands the connection to the handler; from then on nothing it
// does is a response body, so the scan is dropped.
func (h teeHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.t.hijacked = true
	return h.t.ResponseWriter.(http.Hijacker).Hijack()
}

// reveal wraps the tee so a handler's interface assertion sees what the
// underlying writer supports and nothing more: a websocket upgrade
// asserting http.Hijacker must still find one under the tee, and a
// writer that cannot flush must not appear to. http.Pusher is not
// carried: no current browser accepts HTTP/2 push, and the tee only
// runs in tests and `gofastr dev`.
func reveal(t *teeWriter) http.ResponseWriter {
	_, fl := t.ResponseWriter.(http.Flusher)
	_, hj := t.ResponseWriter.(http.Hijacker)
	switch {
	case fl && hj:
		return struct {
			*teeWriter
			teeFlusher
			teeHijacker
		}{t, teeFlusher{t}, teeHijacker{t}}
	case fl:
		return struct {
			*teeWriter
			teeFlusher
		}{t, teeFlusher{t}}
	case hj:
		return struct {
			*teeWriter
			teeHijacker
		}{t, teeHijacker{t}}
	}
	return t
}
