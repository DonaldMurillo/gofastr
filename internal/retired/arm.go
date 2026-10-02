package retired

import (
	"bytes"
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/renderdiag"
)

// scanCap bounds how much of a response body the tee records. Pages are
// finite and whole; the cap only keeps a pathological dev-mode response
// from parking an unbounded buffer. The scan reads what was recorded.
const scanCap = 16 << 20

// Arm decides whether this response is scanned for retired markup and,
// when it is, wraps w with a tee that records the body. The returned
// finish scans what was written and reports each finding: through the
// request's renderdiag reporter when one is installed (TestHarness
// turns those into test failures), else at warn level. When not armed —
// production, or a request under ProductionScan — it returns w and a
// no-op, and the registry is never loaded.
//
// devMode is the caller's `gofastr dev` predicate (framework/dev's
// Enabled); uihost passes it so this package stays leaf-level. In a Go
// test binary responses are always armed. finish must be called after
// the handler returns, before w escapes.
func Arm(w http.ResponseWriter, r *http.Request, devMode bool) (http.ResponseWriter, func()) {
	if _, already := w.(*teeWriter); already {
		// A handler hooked at two layers (PageHandler into handlePage):
		// the outer tee already sees every byte; a second scan would
		// double every report.
		return w, func() {}
	}
	if !armed(r.Context(), devMode) {
		return w, func() {}
	}
	tee := &teeWriter{ResponseWriter: w}
	req := r
	return tee, func() { reportFindings(tee, req) }
}

// armed is the one gate: a Go test binary or `gofastr dev`, minus the
// per-request production exemption a test can install.
func armed(ctx context.Context, devMode bool) bool {
	if d, ok := ctx.Value(productionScanKey{}).(*atomic.Bool); ok && d.Load() {
		return false
	}
	return devMode || testing.Testing()
}

// reportFindings scans the recorded body and delivers each finding.
// With no reporter and dev mode on, each (path, name) is logged once
// per process: `gofastr dev`'s livereload re-serves the same page after
// every edit, and the console must not drown in repeats. A reporter
// (a test harness) sees every finding of every response.
func reportFindings(tee *teeWriter, r *http.Request) {
	if tee.buf.Len() == 0 {
		return
	}
	set := Current()
	if !set.nonEmpty {
		return
	}
	ctx := r.Context()
	reporter := renderdiag.HasRetiredReporter(ctx)
	for _, f := range set.Check(tee.buf.Bytes()) {
		if !reporter && devDedupe(r.URL.Path, f) {
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

// teeWriter records what the handler writes so finish can scan it.
// Every method passes through unchanged: a retired name is a migration
// finding, never a change to the response.
type teeWriter struct {
	http.ResponseWriter
	buf    bytes.Buffer
	capped bool
}

func (w *teeWriter) Write(p []byte) (int, error) {
	if !w.capped {
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

// Flush delegates so streamed responses keep streaming under the tee.
func (w *teeWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
