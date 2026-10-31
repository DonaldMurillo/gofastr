package framework

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
)

// A module's response reaches the host origin, so the child names only
// cache and language headers. It cannot set a cookie, replace the host's
// security headers, open CORS, redirect, or label a body as HTML: the
// Content-Type is the body kind's, and nosniff keeps a browser to it.
func TestModuleResponseHeadersAllowListed(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Security-Policy", "default-src 'self'")
	rec.Header().Set("X-Frame-Options", "DENY")
	res := &moduleproto.HTTPResponseResult{
		Status: 200,
		Headers: map[string]string{
			"Content-Type":                "text/html",
			"Set-Cookie":                  "sid=forged; Path=/",
			"set-cookie":                  "sid=forged2; Path=/",
			"Content-Security-Policy":     "default-src *",
			"X-Frame-Options":             "ALLOWALL",
			"X-Content-Type-Options":      "",
			"Access-Control-Allow-Origin": "*",
			"Location":                    "https://evil.example/",
			"Refresh":                     "0; url=https://evil.example/",
			"connection":                  "keep-alive",
			"Cache-Control":               "max-age=60",
			"etag":                        `"v1"`,
			"Vary":                        "Accept-Language",
		},
		Body: moduleproto.HTTPResponseBody{Kind: moduleproto.BodyKindText, Value: jsonRaw("<script>alert(1)</script>")},
	}
	commitBufferedResponse(rec, res, nil)
	h := rec.Header()
	for name, want := range map[string]string{
		"Content-Type":            "text/plain; charset=utf-8",
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'self'",
		"X-Frame-Options":         "DENY",
		"Cache-Control":           "max-age=60",
		"Etag":                    `"v1"`,
		"Vary":                    "Accept-Language",
	} {
		if got := h.Values(name); len(got) != 1 || got[0] != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	for _, name := range []string{"Set-Cookie", "Access-Control-Allow-Origin", "Location", "Refresh", "Connection"} {
		if got := h.Values(name); len(got) != 0 {
			t.Errorf("%s = %q, want none", name, got)
		}
	}
}

// A module's Vary adds to the host's, never replaces it: the locale
// middleware's Accept-Language stays.
func TestModuleResponseVaryAdds(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Vary", "Accept-Language")
	commitBufferedResponse(rec, &moduleproto.HTTPResponseResult{
		Status:  200,
		Headers: map[string]string{"Vary": "Accept"},
		Body:    moduleproto.HTTPResponseBody{Kind: moduleproto.BodyKindJSON, Value: jsonRaw(`{}`)},
	}, nil)
	if got := rec.Header().Values("Vary"); len(got) != 2 || got[0] != "Accept-Language" || got[1] != "Accept" {
		t.Fatalf("Vary = %q, want the host's then the module's", got)
	}
}

// A status a handler cannot write (0, 1xx, past 599) is a broken child,
// answered 503 rather than a panic in WriteHeader or an informational
// response the client waits past.
func TestModuleResponseBadStatus503(t *testing.T) {
	for _, status := range []int{0, 101, 199, 600, 1000} {
		rec := httptest.NewRecorder()
		commitBufferedResponse(rec, &moduleproto.HTTPResponseResult{
			Status: status,
			Body:   moduleproto.HTTPResponseBody{Kind: moduleproto.BodyKindJSON, Value: jsonRaw(`{}`)},
		}, nil)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status %d: answered %d, want 503", status, rec.Code)
		}
	}
}
