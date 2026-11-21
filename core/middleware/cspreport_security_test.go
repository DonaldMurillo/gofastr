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

func TestCSPReportingKeepsHostDirectives(t *testing.T) {
	cfg := SecurityHeadersConfig{
		ContentSecurityPolicy: "default-src 'self'; report-uri https://r.example/a, script-src 'self'; report-to mine;",
		CSPReportURI:          "https://r.example/b",
	}
	w := httptest.NewRecorder()
	SecurityHeaders(cfg)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	got := w.Header().Get("Content-Security-Policy")
	want := "default-src 'self'; report-uri https://r.example/a; report-to " + CSPReportEndpoint +
		", script-src 'self'; report-to mine; report-uri https://r.example/b"
	if got != want {
		t.Fatalf("CSP\n got %q\nwant %q", got, want)
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

func TestCSPReportHandler(t *testing.T) {
	var got []CSPReport
	h := CSPReportHandler(func(_ *http.Request, r CSPReport) { got = append(got, r) })
	post := func(ct, body string) int {
		r := httptest.NewRequest("POST", CSPReportPath, strings.NewReader(body))
		r.Header.Set("Content-Type", ct)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}

	if c := post("application/reports+json", `[
		{"type":"csp-violation","body":{"documentURL":"https://app.example/p?token=secret#x","blockedURL":"trusted-types-sink","effectiveDirective":"require-trusted-types-for","disposition":"enforce","sample":"Element innerHTML|<img src=x onerror=\u0007alert(1)>","sourceFile":"https://app.example/a.js?v=1","lineNumber":3,"columnNumber":9,"statusCode":200}},
		{"type":"deprecation","body":{}}
	]`); c != http.StatusNoContent {
		t.Fatalf("reports+json: status %d", c)
	}
	if len(got) != 1 {
		t.Fatalf("got %d reports, want 1 (non-CSP entries skipped)", len(got))
	}
	r := got[0]
	if r.DocumentURL != "https://app.example/p" || r.SourceFile != "https://app.example/a.js" {
		t.Errorf("query or fragment kept: %q %q", r.DocumentURL, r.SourceFile)
	}
	if strings.ContainsRune(r.Sample, 0x07) || !strings.Contains(r.Sample, "%07") {
		t.Errorf("control byte not escaped in sample %q", r.Sample)
	}
	if r.EffectiveDirective != "require-trusted-types-for" || r.LineNumber != 3 || r.Disposition != "enforce" {
		t.Errorf("fields lost: %+v", r)
	}

	got = nil
	if c := post("application/csp-report", `{"csp-report":{"document-uri":"http://app.example/","violated-directive":"script-src-elem 'self'","blocked-uri":"inline","script-sample":"alert(1)"}}`); c != http.StatusNoContent {
		t.Fatalf("csp-report: status %d", c)
	}
	if len(got) != 1 || got[0].EffectiveDirective != "script-src-elem" || got[0].BlockedURL != "inline" {
		t.Fatalf("legacy report: %+v", got)
	}

	got = nil
	many := "[" + strings.TrimSuffix(strings.Repeat(`{"type":"csp-violation","body":{"sample":"`+strings.Repeat("a", 1000)+`"}},`, 40), ",") + "]"
	if c := post("application/reports+json", many); c != http.StatusNoContent {
		t.Fatalf("batch: status %d", c)
	}
	if len(got) != maxCSPReportsPerReq {
		t.Errorf("batch delivered %d reports, want the cap %d", len(got), maxCSPReportsPerReq)
	}
	if len(got[0].Sample) > maxCSPReportField {
		t.Errorf("sample is %d bytes, over the %d cap", len(got[0].Sample), maxCSPReportField)
	}

	for _, tc := range []struct {
		ct, body string
		want     int
	}{
		{"application/json", `[]`, http.StatusUnsupportedMediaType},
		{"text/plain", `x`, http.StatusUnsupportedMediaType},
		{"application/reports+json", `{`, http.StatusBadRequest},
		{"application/csp-report", `{}`, http.StatusBadRequest},
		{"application/reports+json", "[" + strings.Repeat(" ", maxCSPReportBody) + "]", http.StatusBadRequest},
	} {
		if c := post(tc.ct, tc.body); c != tc.want {
			t.Errorf("%s %.20q: status %d, want %d", tc.ct, tc.body, c, tc.want)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", CSPReportPath, nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: status %d, want 405", w.Code)
	}
}

func TestCSRFLetsCSPReportsThrough(t *testing.T) {
	reached := false
	h := CSRF(CSRFConfig{SecretKey: []byte("0123456789abcdef0123456789abcdef")})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	}))
	r := httptest.NewRequest("POST", CSPReportPath, strings.NewReader("[]"))
	r.Header.Set("Content-Type", "application/reports+json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !reached {
		t.Fatalf("CSRF refused a violation report with %d", w.Code)
	}
	reached = false
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", CSPReportPath+"x", nil))
	if reached {
		t.Fatal("CSRF exemption reaches past the report path")
	}
}
