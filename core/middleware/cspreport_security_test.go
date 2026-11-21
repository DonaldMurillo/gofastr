package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSPReportingHeaders(t *testing.T) {
	h := SecurityHeaders(SecurityHeadersConfig{CSPReportURI: CSPReportPath})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	csp := w.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self' 'report-sample';", "; report-to " + CSPReportEndpoint, "; report-uri " + CSPReportPath} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q lacks %q", csp, want)
		}
	}
	if got, want := w.Header().Get("Reporting-Endpoints"), CSPReportEndpoint+`="`+CSPReportPath+`"`; got != want {
		t.Errorf("Reporting-Endpoints = %q, want %q", got, want)
	}

	// No reporting configured: no endpoint header and no report directives.
	w = httptest.NewRecorder()
	SecurityHeaders(SecurityHeadersConfig{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if csp := w.Header().Get("Content-Security-Policy"); strings.Contains(csp, "report") {
		t.Errorf("default CSP carries reporting with no CSPReportURI: %q", csp)
	}
	if w.Header().Get("Reporting-Endpoints") != "" {
		t.Error("Reporting-Endpoints sent with no CSPReportURI")
	}
}

// A policy that already reports somewhere is left alone: adding report-to
// beside a host's report-uri would make Chrome stop using the report-uri.
func TestCSPReportingKeepsHostDirectives(t *testing.T) {
	cfg := SecurityHeadersConfig{
		ContentSecurityPolicy: "default-src 'self'; report-uri https://r.example/a, script-src 'self'; report-to mine, img-src 'self';",
		CSPReportURI:          "https://r.example/b",
	}
	w := httptest.NewRecorder()
	SecurityHeaders(cfg)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	got := w.Header().Get("Content-Security-Policy")
	want := "default-src 'self'; report-uri https://r.example/a, script-src 'self'; report-to mine, " +
		"img-src 'self'; report-to " + CSPReportEndpoint + "; report-uri https://r.example/b"
	if got != want {
		t.Fatalf("CSP\n got %q\nwant %q", got, want)
	}
}

// CSP allows any ASCII whitespace after a directive name.
func TestCSPReportingSeesTabSeparatedDirectives(t *testing.T) {
	own := "default-src 'self'; report-uri\thttps://r.example/a"
	if got := cspOf(SecurityHeadersConfig{ContentSecurityPolicy: own, CSPReportURI: "https://r.example/b"}); got != own {
		t.Fatalf("a tab-separated report-uri was not seen: %q", got)
	}
	if got := cspOf(SecurityHeadersConfig{ContentSecurityPolicy: "default-src\t'self'", CSPReportURI: "https://r.example/b"}); !strings.HasSuffix(got, "; report-to "+CSPReportEndpoint+"; report-uri https://r.example/b") {
		t.Fatalf("a policy with no reporting directive got none: %q", got)
	}
}

func TestReportingEndpointsKeepsOuterEndpoints(t *testing.T) {
	ours := CSPReportEndpoint + `="` + CSPReportPath + `"`
	for _, tc := range []struct{ outer, want string }{
		{"", ours},
		{`nel="https://r.example/nel"`, `nel="https://r.example/nel", ` + ours},
		{CSPReportEndpoint + `="https://r.example/csp"`, CSPReportEndpoint + `="https://r.example/csp"`},
	} {
		h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
		outer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tc.outer != "" {
				w.Header().Set("Reporting-Endpoints", tc.outer)
			}
			SecurityHeaders(SecurityHeadersConfig{CSPReportURI: CSPReportPath})(h).ServeHTTP(w, r)
		})
		w := httptest.NewRecorder()
		outer.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		if got := w.Header().Get("Reporting-Endpoints"); got != tc.want {
			t.Errorf("outer %q: Reporting-Endpoints = %q, want %q", tc.outer, got, tc.want)
		}
	}
}

func TestCSPReportURIRefusesHeaderBreakers(t *testing.T) {
	for _, bad := range []string{"/r;script-src *", "/r, default-src *", "/r\nX: y", `/r"`, "/a b"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("CSPReportURI %q was accepted", bad)
				}
			}()
			SecurityHeaders(SecurityHeadersConfig{CSPReportURI: bad})
		}()
	}
}

func TestCSRFLetsCSPReportsThrough(t *testing.T) {
	reached := false
	h := CSRF(CSRFConfig{SecretKey: []byte("0123456789abcdef0123456789abcdef")})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, ct := range []string{"application/reports+json", "application/csp-report; charset=utf-8"} {
		reached = false
		r := httptest.NewRequest("POST", "/internal/csp", strings.NewReader("[]"))
		r.Header.Set("Content-Type", ct)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if !reached {
			t.Fatalf("CSRF refused a %s violation report with %d", ct, w.Code)
		}
	}
	// Only a POST is a report: another unsafe method with the report type still needs its token.
	for _, m := range []string{"PUT", "DELETE", "PATCH"} {
		reached = false
		r := httptest.NewRequest(m, CSPReportPath, strings.NewReader("[]"))
		r.Header.Set("Content-Type", "application/reports+json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if reached || w.Code != http.StatusForbidden {
			t.Fatalf("CSRF let a %s with a report type through (status %d)", m, w.Code)
		}
	}
	// A form-shaped post to the sink's path still needs its token.
	for _, ct := range []string{"application/x-www-form-urlencoded", "text/plain", ""} {
		reached = false
		r := httptest.NewRequest("POST", CSPReportPath, strings.NewReader("a=b"))
		if ct != "" {
			r.Header.Set("Content-Type", ct)
		}
		h.ServeHTTP(httptest.NewRecorder(), r)
		if reached {
			t.Fatalf("CSRF let a %q post through on the report path", ct)
		}
	}
}
