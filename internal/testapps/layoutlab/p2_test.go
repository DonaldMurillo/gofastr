package main

// In-process containment measurements — what a failing fill does to
// the HTTP response, on both the full-page and the SPA-partial path.
// The browser behaviours are in p2_e2e_test.go.

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/testkit"
)

// p2Cases: each broken route, the screen marker it must (not) render,
// and the aside content variant B must degrade to.
var p2Cases = []struct {
	route, screen, aside string
}{
	{"/broken/load", "SCREEN-BROKEN-LOAD", "ASIDE-HELP"},
	{"/broken/panic", "SCREEN-BROKEN-PANIC", "ASIDE-HELP"},
	{"/broken/boundary", "SCREEN-BROKEN-BOUNDARY", "ASIDE-BOUNDARY-ERROR"},
}

func captureLabSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// labServe builds the app under the current env and serves it.
func labServe(t *testing.T, wrappers ...func(testing.TB, http.Handler) http.Handler) *httptest.Server {
	t.Helper()
	fwApp := buildApp()
	if err := fwApp.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	var handler http.Handler = fwApp.Router()
	for _, wrap := range wrappers {
		handler = wrap(t, handler)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func labGet(t *testing.T, url string, hdr map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, string(body)
}

// labAssertNoHostile fails when the hostile error text (markup, C1 CSI,
// bidi override) appears raw in a response body.
func labAssertNoHostile(t *testing.T, body string) {
	t.Helper()
	for _, bad := range []string{"<img src=x", "\u009b", "\u202e", "onerror=alert"} {
		if strings.Contains(body, bad) {
			t.Errorf("hostile error text echoed into the response (%q)", bad)
		}
	}
}

// TestP2FullPagePerRoute: a direct (full) load of each broken route
// answers 200 with the screen rendered and only the outlet degraded —
// containment is the only behaviour (the losing page-error variant is
// gone).
func TestP2FullPagePerRoute(t *testing.T) {
	// Prove production keeps the screen's status when a fill panics.
	srv := labServe(t, testkit.AllowRenderPanics)
	logs := captureLabSlog(t)
	for _, tc := range p2Cases {
		res, body := labGet(t, srv.URL+tc.route, nil)
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s: status = %d, want 200 (page status stays the screen's)", tc.route, res.StatusCode)
		}
		if !strings.Contains(body, tc.screen) {
			t.Errorf("%s: screen must render under containment, got %s", tc.route, body)
		}
		if !strings.Contains(body, tc.aside) {
			t.Errorf("%s: aside must degrade to %s", tc.route, tc.aside)
		}
		labAssertNoHostile(t, body)
	}
	if !strings.Contains(logs.String(), "app: fill failed; outlet degraded to fallback") {
		t.Errorf("containment must log each contained failure, got: %q", logs.String())
	}
}

// TestP2PartialPerRoute: the SPA partial fetch for each broken route
// (the headers the runtime sends) gets the normal envelope with the
// degraded fill riding along — status stays 200, the not-found partial
// (a missed route, P14) is a different outcome a contained fill never
// reaches.
func TestP2PartialPerRoute(t *testing.T) {
	// Prove production sends a successful envelope with the recovered fill.
	srv := labServe(t, testkit.AllowRenderPanics)
	captureLabSlog(t)
	for _, tc := range p2Cases {
		res, body := labGet(t, srv.URL+tc.route, map[string]string{
			"X-Gofastr-Navigate": "1",
			"X-Gofastr-From":     "/",
			"X-Gofastr-Fills":    "2",
		})
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", tc.route, res.StatusCode)
		}
		if res.Header.Get("X-Gofastr-Partial") != "true" {
			t.Errorf("%s: missing X-Gofastr-Partial", tc.route)
		}
		if res.Header.Get("X-Gofastr-Envelope") != "2" {
			t.Errorf("%s: missing X-Gofastr-Envelope: 2", tc.route)
		}
		if !strings.Contains(body, `data-cui-fill="l:shell#aside"`) {
			t.Errorf("%s: envelope must carry the degraded aside fill", tc.route)
		}
		if !strings.Contains(body, tc.aside) {
			t.Errorf("%s: degraded aside fill must carry %s", tc.route, tc.aside)
		}
		if !strings.Contains(body, tc.screen) {
			t.Errorf("%s: primary payload must render the screen", tc.route)
		}
		labAssertNoHostile(t, body)
	}
}
