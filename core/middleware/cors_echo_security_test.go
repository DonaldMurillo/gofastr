package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func preflight(origin, method, headers string) *http.Request {
	r := httptest.NewRequest(http.MethodOptions, "/api/things", nil)
	r.Header.Set("Origin", origin)
	r.Header.Set("Access-Control-Request-Method", method)
	if headers != "" {
		r.Header.Set("Access-Control-Request-Headers", headers)
	}
	return r
}

var noop = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

// The #635 case: a preflight asking for X-Request-Tag is answered with
// it when AllowAnyHeader is on, so the browser sends the request.
func TestCORSAllowAnyHeaderEchoesRequestedHeaders(t *testing.T) {
	h := CORS(CORSConfig{AllowedOrigins: []string{"http://localhost:5173"}, AllowAnyHeader: true, AllowAnyMethod: true})(noop)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, preflight("http://localhost:5173", "PURGE", "X-Request-Tag, content-type"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight = %d; want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "x-request-tag, content-type" {
		t.Fatalf("Allow-Headers = %q; want the requested headers", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "PURGE" {
		t.Fatalf("Allow-Methods = %q; want the requested method", got)
	}
	vary := strings.Join(rec.Header().Values("Vary"), ",")
	for _, v := range []string{"Origin", "Access-Control-Request-Headers", "Access-Control-Request-Method"} {
		if !strings.Contains(vary, v) {
			t.Fatalf("Vary = %q; want %s in it", vary, v)
		}
	}
}

// Without the option the configured list stands, as before.
func TestCORSWithoutAllowAnyHeaderKeepsList(t *testing.T) {
	h := CORS(CORSConfig{AllowedOrigins: []string{"http://localhost:5173"}})(noop)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, preflight("http://localhost:5173", "POST", "X-Request-Tag"))
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type, Authorization" {
		t.Fatalf("Allow-Headers = %q; want the default list", got)
	}
}

// A rejected origin learns nothing, echo or not.
func TestCORSAllowAnyHeaderRejectedOriginGetsNothing(t *testing.T) {
	h := CORS(CORSConfig{AllowedOrigins: []string{"http://localhost:5173"}, AllowAnyHeader: true, AllowAnyMethod: true})(noop)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, preflight("https://evil.example", "POST", "X-Request-Tag"))
	if rec.Code != http.StatusForbidden || rec.Header().Get("Access-Control-Allow-Headers") != "" || rec.Header().Get("Access-Control-Allow-Methods") != "" {
		t.Fatalf("rejected origin = %d, headers %v; want 403 and no Allow-*", rec.Code, rec.Header())
	}
}

// Only well-formed names are echoed: a malformed entry cannot smuggle a
// byte into the response header, and the list is capped.
func TestCORSAllowAnyHeaderEchoesOnlyTokens(t *testing.T) {
	h := CORS(CORSConfig{AllowedOrigins: []string{"*"}, AllowAnyHeader: true, AllowAnyMethod: true})(noop)
	r := preflight("http://a.example", "GET\x7f", "")
	many := make([]string, 200)
	for i := range many {
		many[i] = fmt.Sprintf("x-h%d", i)
	}
	r.Header["Access-Control-Request-Headers"] = []string{"x-ok, bad header, x\"q, x-ok, " + strings.Join(many, ",")}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	got := rec.Header().Get("Access-Control-Allow-Headers")
	if strings.Contains(got, "bad header") || strings.Contains(got, `"`) {
		t.Fatalf("Allow-Headers = %q; echoed a malformed name", got)
	}
	if strings.Count(got, "x-ok") != 1 {
		t.Fatalf("Allow-Headers = %q; want x-ok once", got)
	}
	if n := len(strings.Split(got, ",")); n > maxEchoedHeaders {
		t.Fatalf("Allow-Headers carries %d names; want at most %d", n, maxEchoedHeaders)
	}
	if m := rec.Header().Get("Access-Control-Allow-Methods"); strings.ContainsRune(m, 0x7f) || m != "GET, POST, PUT, DELETE, PATCH, OPTIONS" {
		t.Fatalf("Allow-Methods = %q; a malformed requested method must fall back to the list", m)
	}
}
