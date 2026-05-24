package middleware

import (
	"bytes"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCSRF_FormBodyHasMaxSize pins the DoS fix: an unauthenticated POST
// with an oversized form body must be rejected with 413 before the
// middleware loads N MB into memory looking for a _csrf field.
//
// Without the cap, ParseForm() in the middleware would buffer up to
// 10MB (form-urlencoded) or 32MB (multipart) per request — 100 attackers
// = 1-3GB resident.
func TestCSRF_FormBodyHasMaxSize(t *testing.T) {
	mw := CSRF(CSRFConfig{
		FormField:    "_csrf",
		MaxFormBytes: 1024, // tiny for the test
	})
	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	// 1) Prime cookie so we land on the body-fallback path (rather
	// than the missing-cookie 403).
	primeRec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).
		ServeHTTP(primeRec, httptest.NewRequest(http.MethodGet, "/", nil))
	var ck *http.Cookie
	for _, c := range primeRec.Result().Cookies() {
		if c.Name == "csrf_token" {
			ck = c
		}
	}
	if ck == nil {
		t.Fatal("no cookie from GET")
	}

	// 2) Oversized POST. 10 KB body, cookie present, no header — must
	// 413 BEFORE buffering the full body.
	big := bytes.Repeat([]byte("a=b&"), 2500)
	req := httptest.NewRequest(http.MethodPost, "/save", bytes.NewReader(big))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(ck)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called {
		t.Error("downstream handler invoked despite oversized form body")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413. body=%s", rec.Code, rec.Body.String())
	}
}

// TestCSRF_FormBodyDownstreamCanRead pins the buffer-and-restore fix: a
// CSRF-protected handler that legitimately wants to read r.Body
// downstream still gets the bytes, even though the middleware already
// parsed them looking for the _csrf field.
//
// Without this fix, ParseForm() drains r.Body to EOF and any
// io.Copy(dst, r.Body) downstream sees empty.
func TestCSRF_FormBodyDownstreamCanRead(t *testing.T) {
	mw := CSRF(CSRFConfig{FormField: "_csrf", MaxFormBytes: 1 << 20})

	// 1) GET to prime the cookie.
	primeRec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).
		ServeHTTP(primeRec, httptest.NewRequest(http.MethodGet, "/", nil))
	var ck *http.Cookie
	for _, c := range primeRec.Result().Cookies() {
		if c.Name == "csrf_token" {
			ck = c
		}
	}
	if ck == nil {
		t.Fatal("no csrf cookie from GET")
	}

	// 2) POST with the token in the form body. Downstream reads r.Body.
	bodyText := "name=alice&_csrf=" + ck.Value
	req := httptest.NewRequest(http.MethodPost, "/save", strings.NewReader(bodyText))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(ck)

	var seen string
	post := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		seen = string(buf)
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	post.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(seen, "name=alice") {
		t.Errorf("downstream r.Body empty or truncated after CSRF: %q", seen)
	}
}

// TestCSRF_FormBodyMaxSizeDefault confirms there's a sane non-zero
// default cap so callers who don't set MaxFormBytes still get
// protection.
// TestCSRF_MultipartBodyHasMaxSize pins the cap for the OTHER form
// content-type. Go's stdlib defaults to 32MB for multipart parsing —
// much bigger DoS surface than urlencoded's 10MB. The middleware's
// MaxFormBytes wraps both via the same readAndBufferCapped path.
func TestCSRF_MultipartBodyHasMaxSize(t *testing.T) {
	mw := CSRF(CSRFConfig{
		FormField:    "_csrf",
		MaxFormBytes: 1024,
	})

	// Prime cookie.
	primeRec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).
		ServeHTTP(primeRec, httptest.NewRequest(http.MethodGet, "/", nil))
	var ck *http.Cookie
	for _, c := range primeRec.Result().Cookies() {
		if c.Name == "csrf_token" {
			ck = c
		}
	}
	if ck == nil {
		t.Fatal("no cookie from GET")
	}

	// Build a 10KB multipart body — over the 1KB cap.
	boundary := "X-BOUNDARY"
	bodyText := "--" + boundary + "\r\n" +
		"Content-Disposition: form-data; name=\"field\"\r\n\r\n" +
		strings.Repeat("a", 10*1024) + "\r\n--" + boundary + "--\r\n"
	req := httptest.NewRequest(http.MethodPost, "/save", strings.NewReader(bodyText))
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	req.AddCookie(ck)
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("downstream handler invoked despite oversized multipart body")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized multipart status = %d, want 413. body=%s", rec.Code, rec.Body.String())
	}
}

func TestCSRF_FormBodyMaxSizeDefault(t *testing.T) {
	mw := CSRF(CSRFConfig{FormField: "_csrf"}) // no MaxFormBytes

	// Prime cookie.
	primeRec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).
		ServeHTTP(primeRec, httptest.NewRequest(http.MethodGet, "/", nil))
	var ck *http.Cookie
	for _, c := range primeRec.Result().Cookies() {
		if c.Name == "csrf_token" {
			ck = c
		}
	}

	// 9 MB body — should be rejected by the default (which the fix sets
	// to 1MB).
	big := make([]byte, 9<<20)
	_, _ = rand.Read(big)
	req := httptest.NewRequest(http.MethodPost, "/save", bytes.NewReader(big))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(ck)
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler invoked despite 9MB body and default cap")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("9MB body should 413: status=%d body=%s", rec.Code, rec.Body.String())
	}
}
