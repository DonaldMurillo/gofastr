package framework

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

func postCSPReport(app *App) int {
	r := httptest.NewRequest(http.MethodPost, middleware.CSPReportPath, strings.NewReader(
		`[{"type":"csp-violation","body":{"documentURL":"https://app.example/","blockedURL":"inline","effectiveDirective":"script-src-elem","disposition":"enforce"}}]`))
	r.Header.Set("Content-Type", "application/reports+json")
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, r)
	return w.Code
}

func TestWithCSPReportsMountsSinkAndPointsPolicyAtIt(t *testing.T) {
	var got []middleware.CSPReport
	app := NewApp(WithCSPReports(func(_ *http.Request, r middleware.CSPReport) { got = append(got, r) }))
	app, cleanup := startApp(t, app)
	defer cleanup()

	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "report-uri "+middleware.CSPReportPath) {
		t.Errorf("default policy does not report to the sink: %q", csp)
	}
	if code := postCSPReport(app); code != http.StatusNoContent {
		t.Fatalf("sink answered %d, want 204", code)
	}
	if len(got) != 1 || got[0].EffectiveDirective != "script-src-elem" {
		t.Fatalf("callback got %+v", got)
	}
}

func TestWithCSPReportsLogsByDefault(t *testing.T) {
	var buf bytes.Buffer
	app := NewApp(WithLogger(slog.New(slog.NewTextHandler(&buf, nil))), WithCSPReports(nil))
	app, cleanup := startApp(t, app)
	defer cleanup()
	if code := postCSPReport(app); code != http.StatusNoContent {
		t.Fatalf("sink answered %d, want 204", code)
	}
	if !strings.Contains(buf.String(), "content security policy violation") || !strings.Contains(buf.String(), "directive=script-src-elem") {
		t.Fatalf("no violation logged: %s", buf.String())
	}
}

func TestWithCSPReportsKeepsAConfiguredReportURI(t *testing.T) {
	app := NewApp(WithSecurityHeaders(middleware.SecurityHeadersConfig{CSPReportURI: "https://reports.example/csp"}), WithCSPReports(nil))
	app, cleanup := startApp(t, app)
	defer cleanup()
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "report-uri https://reports.example/csp") {
		t.Errorf("configured report URI replaced: %q", csp)
	}
}

func TestNoCSPReportSinkByDefault(t *testing.T) {
	app, cleanup := startApp(t, NewApp())
	defer cleanup()
	if code := postCSPReport(app); code == http.StatusNoContent {
		t.Fatal("the sink is mounted without WithCSPReports")
	}
}

// Anyone can post to the sink, so the default log takes at most
// cspLogPerWindow reports a minute and then says how many it dropped.
func TestCSPReportLogIsRateLimited(t *testing.T) {
	var buf bytes.Buffer
	app := NewApp(WithLogger(slog.New(slog.NewTextHandler(&buf, nil))), WithCSPReports(nil))
	app, cleanup := startApp(t, app)
	defer cleanup()
	for range cspLogPerWindow + 15 {
		if code := postCSPReport(app); code != http.StatusNoContent {
			t.Fatalf("sink answered %d", code)
		}
	}
	if n := strings.Count(buf.String(), `msg="content security policy violation"`); n != cspLogPerWindow {
		t.Fatalf("logged %d reports in one window, want %d", n, cspLogPerWindow)
	}
	ok, dropped := app.cspLog.take(time.Now().Add(cspLogWindow))
	if !ok || dropped != 15 {
		t.Fatalf("next window: ok=%v dropped=%d, want true and 15", ok, dropped)
	}
}
