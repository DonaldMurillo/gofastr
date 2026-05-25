package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCSRF_GETSetsCookie(t *testing.T) {
	mw := CSRF()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d", rec.Code)
	}
	found := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == CSRFCookieName && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %s cookie on GET; cookies=%v", CSRFCookieName, rec.Result().Cookies())
	}
}

func TestCSRF_POSTRejectsMissingToken(t *testing.T) {
	mw := CSRF()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("downstream handler should NOT have been called")
	}))
	req := httptest.NewRequest(http.MethodPost, "/save", strings.NewReader("data=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 on POST w/o token; got %d", rec.Code)
	}
}

func TestCSRF_POSTAcceptsFormBodyToken(t *testing.T) {
	mw := CSRF()
	// 1. Issue a GET to obtain the token cookie.
	getReq := httptest.NewRequest(http.MethodGet, "/", nil)
	getRec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(getRec, getReq)

	var cookie *http.Cookie
	for _, c := range getRec.Result().Cookies() {
		if c.Name == CSRFCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no CSRF cookie from GET")
	}

	// 2. POST the same token in the form body — should pass.
	called := false
	postHandler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	form := url.Values{CSRFFormField: {cookie.Value}, "data": {"x"}}
	postReq := httptest.NewRequest(http.MethodPost, "/save", strings.NewReader(form.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(cookie)
	postRec := httptest.NewRecorder()
	postHandler.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusOK {
		t.Fatalf("form-body CSRF rejected: %d / %s", postRec.Code, postRec.Body.String())
	}
	if !called {
		t.Error("downstream handler not invoked")
	}
}

func TestCSRF_POSTAcceptsHeaderToken(t *testing.T) {
	mw := CSRF()
	getReq := httptest.NewRequest(http.MethodGet, "/", nil)
	getRec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(getRec, getReq)

	var cookie *http.Cookie
	for _, c := range getRec.Result().Cookies() {
		if c.Name == CSRFCookieName {
			cookie = c
		}
	}

	postHandler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	postReq := httptest.NewRequest(http.MethodPost, "/save", strings.NewReader(`{"x":1}`))
	postReq.Header.Set("Content-Type", "application/json")
	postReq.Header.Set(CSRFHeaderName, cookie.Value)
	postReq.AddCookie(cookie)
	postRec := httptest.NewRecorder()
	postHandler.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusOK {
		t.Fatalf("header CSRF rejected: %d / %s", postRec.Code, postRec.Body.String())
	}
}

func TestCSRFInputHTML_EmitsHiddenInput(t *testing.T) {
	mw := CSRF()
	getReq := httptest.NewRequest(http.MethodGet, "/", nil)
	getRec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(getRec, getReq)

	var cookie *http.Cookie
	for _, c := range getRec.Result().Cookies() {
		if c.Name == CSRFCookieName {
			cookie = c
		}
	}

	// Build a fresh request that has the cookie set (simulating the next page).
	r := httptest.NewRequest(http.MethodGet, "/form", nil)
	r.AddCookie(cookie)

	htmlOut := string(CSRFInputHTML(r))
	if !strings.Contains(htmlOut, `name="_csrf"`) {
		t.Errorf("missing name attr: %q", htmlOut)
	}
	if !strings.Contains(htmlOut, `value="`+cookie.Value+`"`) {
		t.Errorf("missing token value: %q", htmlOut)
	}
}

func TestCSRF_SkipsBearerAuth(t *testing.T) {
	mw := CSRF()
	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer xxx")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if !called {
		t.Error("Bearer-auth request blocked by CSRF — should have been skipped")
	}
}
