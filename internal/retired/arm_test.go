package retired

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/renderdiag"
)

// page is a fixture page carrying the fixture registry's retired names.
const page = `<!DOCTYPE html><html><body><div class="ui-button">go</div><span data-fui-signal>x</span></body></html>`

// serve runs one request through a handler that has wrapped w with Arm
// and calls the returned finish, the way uihost does.
func serve(t *testing.T, r *http.Request, handler func(w http.ResponseWriter, r *http.Request), devMode bool) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	inner := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w, finish := Arm(w, req, devMode)
		handler(w, req)
		finish()
	})
	inner.ServeHTTP(rec, r)
	return rec
}

func TestArmDeliversToReporter(t *testing.T) {
	UseForTest(t, fixtureSet(t))
	var got []string
	ctx := renderdiagRetiredReporter(func(m string) { got = append(got, m) })
	r := httptest.NewRequest(http.MethodGet, "/old", nil).WithContext(ctx)
	rec := serve(t, r, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, page)
	}, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, the check must never change the response", rec.Code)
	}
	if rec.Body.String() != page {
		t.Fatalf("body changed: %q", rec.Body.String())
	}
	want := []string{
		`retired markup: class "ui-button" (v9.0.0: the ui-button class is gone); run gofastr upgrade`,
		`retired markup: attr "data-fui-signal" (v9.0.0: the ui-button class is gone); run gofastr upgrade`,
	}
	if !equalStrings(got, want) {
		t.Fatalf("reports = %q, want %q", got, want)
	}
}

// A clean page reports nothing: migrated spellings and kept marker
// values are not findings.
func TestArmCleanPageReportsNothing(t *testing.T) {
	UseForTest(t, fixtureSet(t))
	var got []string
	ctx := renderdiagRetiredReporter(func(m string) { got = append(got, m) })
	r := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	serve(t, r, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!DOCTYPE html><html><body><button class="fui-button" data-fui-comp="ui-sidebar">ok</button></body></html>`)
	}, false)
	if len(got) != 0 {
		t.Fatalf("clean page reported %q", got)
	}
}

// Without a reporter (an httptest server built by hand), the finding
// logs at warn level.
func TestArmWithoutReporterLogs(t *testing.T) {
	UseForTest(t, fixtureSet(t))
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(old)

	r := httptest.NewRequest(http.MethodGet, "/old", nil)
	serve(t, r, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, page) }, false)
	if !strings.Contains(logs.String(), "ui-button") || !strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("missing warn log: %s", logs.String())
	}
}

// Dev mode warns once per (path, name) per process: a livereload loop
// must not flood the console.
func TestArmDevDedupesPerPathAndName(t *testing.T) {
	UseForTest(t, fixtureSet(t))
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(old)

	resetDevWarned(t)
	for range 3 {
		serve(t, httptest.NewRequest(http.MethodGet, "/old", nil),
			func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, page) }, true)
	}
	// 3 requests x 2 findings, one warning each per (path, name).
	if n := strings.Count(logs.String(), "level=WARN"); n != 2 {
		t.Fatalf("%d warnings for 3 requests to one path, want 2: %s", n, logs.String())
	}
	// A different path warns again.
	logs.Reset()
	serve(t, httptest.NewRequest(http.MethodGet, "/other", nil),
		func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, page) }, true)
	if n := strings.Count(logs.String(), "level=WARN"); n != 2 {
		t.Fatalf("second path logged %d warnings, want 2: %s", n, logs.String())
	}
	// A test reporter beats the dedupe: every finding must reach it.
	var got []string
	ctx := renderdiagRetiredReporter(func(m string) { got = append(got, m) })
	for range 3 {
		serve(t, httptest.NewRequest(http.MethodGet, "/old", nil).WithContext(ctx),
			func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, page) }, true)
	}
	if len(got) != 6 {
		t.Fatalf("reporter saw %d findings across 3 requests, want 6 (no dedupe with a reporter)", len(got))
	}
}

// Production posture: outside dev mode, with the scan disarmed, the
// registry is never loaded even when the page carries retired names.
// The armed twin proves the same harness would have loaded it.
func TestArmProductionNeverLoadsRegistry(t *testing.T) {
	ResetForTest(t)
	prod := ProductionScan(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w, finish := Arm(w, r, false)
		fmt.Fprint(w, page)
		finish()
	}))
	prod.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if Loads() != 0 {
		t.Fatalf("production posture loaded the registry %d times", Loads())
	}
	// The guard is real: the same handler without the exemption loads.
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w, finish := Arm(rec, r, false)
	fmt.Fprint(w, page)
	finish()
	if Loads() != 1 {
		t.Fatalf("armed scan loaded the registry %d times, want 1", Loads())
	}
}

// Arm on a disarmed (production-posture) request still passes writes
// through untouched.
func TestArmProductionPassthrough(t *testing.T) {
	UseForTest(t, fixtureSet(t))
	var got []string
	ctx := renderdiagRetiredReporter(func(m string) { got = append(got, m) })
	prod := ProductionScan(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w, finish := Arm(w, r, false)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusTeapot)
		fmt.Fprint(w, page)
		finish()
	}))
	rec := httptest.NewRecorder()
	prod.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(ctx))
	if rec.Code != http.StatusTeapot || rec.Body.String() != page {
		t.Fatalf("production write path changed: %d %q", rec.Code, rec.Body.String())
	}
	if len(got) != 0 {
		t.Fatalf("disarmed request reported %q", got)
	}
}

// A handler that streams with Flush keeps working under the tee.
func TestArmFlushStreams(t *testing.T) {
	UseForTest(t, fixtureSet(t))
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w, finish := Arm(rec, r, false)
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, "<div class=\"ui-button\">")
	if err := rc.Flush(); err != nil {
		t.Fatalf("flush failed under the tee: %v", err)
	}
	fmt.Fprint(w, "</div>")
	finish()
	if !strings.Contains(rec.Body.String(), "ui-button") {
		t.Fatalf("streamed body lost: %q", rec.Body.String())
	}
}

// Double-Arm (a handler hooked at two layers) still scans once: the
// inner no-op keeps one response from reporting every finding twice.
func TestArmIdempotent(t *testing.T) {
	UseForTest(t, fixtureSet(t))
	var got []string
	ctx := renderdiagRetiredReporter(func(m string) { got = append(got, m) })
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	var innerFinish func()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w, finish := Arm(w, r, false)
		innerFinish = finish
		fmt.Fprint(w, page)
		finish()
	})
	w, finish := Arm(rec, r, false)
	handler(w, r)
	finish()
	innerFinish()
	if n := len(got); n != 2 {
		t.Fatalf("double arm reported %d findings, want exactly 2 (one class + one attr, scanned once): %q", n, got)
	}
}

func renderdiagRetiredReporter(report func(string)) context.Context {
	return renderdiag.WithRetiredReporter(context.Background(), report)
}

func resetDevWarned(t *testing.T) {
	t.Helper()
	devWarned.Range(func(k, _ any) bool {
		devWarned.Delete(k)
		return true
	})
}
