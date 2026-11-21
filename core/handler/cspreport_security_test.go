package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSPReportHandler(t *testing.T) {
	var got []CSPReport
	h := CSPReportHandler(func(_ *http.Request, r CSPReport) { got = append(got, r) })
	post := func(ct, body string) int {
		r := httptest.NewRequest("POST", "/__gofastr/csp-report", strings.NewReader(body))
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
	h.ServeHTTP(w, httptest.NewRequest("GET", "/__gofastr/csp-report", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: status %d, want 405", w.Code)
	}
}

// A body that repeats a key, or spells one in another case, is refused
// rather than resolved by whichever copy the decoder keeps.
func TestCSPReportHandlerRefusesSmuggledKeys(t *testing.T) {
	h := CSPReportHandler(func(*http.Request, CSPReport) { t.Fatal("a smuggled report reached fn") })
	for _, tc := range []struct{ ct, body string }{
		{"application/reports+json", `[{"type":"csp-violation","type":"x","body":{}}]`},
		{"application/reports+json", `[{"type":"csp-violation","body":{"documentURL":"a","DocumentURL":"b"}}]`},
		{"application/csp-report", `{"csp-report":{"blocked-uri":"a"},"csp-report":{"blocked-uri":"b"}}`},
	} {
		r := httptest.NewRequest("POST", "/__gofastr/csp-report", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", tc.ct)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", tc.body, w.Code)
		}
	}
}
