package auth

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression tests from the 2026-11-01 battery/auth bug bash:
// multipart/form-data bodies. isFormRequest accepted them, but only
// ParseForm ran, which never reads a multipart body: every multipart
// login or register answered 400, though auth.md's "HTML form support"
// promises multipart works like urlencoded.

type multipartField struct {
	name, value string
	file        bool
}

func multipartBody(t *testing.T, fields ...multipartField) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, f := range fields {
		if f.file {
			fw, err := mw.CreateFormFile(f.name, "x.txt")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(fw, f.value)
			continue
		}
		if err := mw.WriteField(f.name, f.value); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

func postMultipart(t *testing.T, srvURL, path string, fields ...multipartField) *http.Response {
	t.Helper()
	body, ct := multipartBody(t, fields...)
	req, _ := http.NewRequest(http.MethodPost, srvURL+path, body)
	req.Header.Set("Content-Type", ct)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestMultipartRegisterAndLogin(t *testing.T) {
	srv, mgr := setupAuthTestServer(t)

	resp := postMultipart(t, srv.URL, "/auth/register",
		multipartField{name: "email", value: "mp@example.com"},
		multipartField{name: "password", value: "hunter22"})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("multipart register = %d, want 303; body=%s", resp.StatusCode, mustReadBody(t, resp))
	}

	resp = postMultipart(t, srv.URL, "/auth/login",
		multipartField{name: "email", value: "mp@example.com"},
		multipartField{name: "password", value: "hunter22"},
		multipartField{name: "next", value: "/dashboard"})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("multipart login = %d, want 303; body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	if loc := resp.Header.Get("Location"); loc != "/dashboard" {
		t.Errorf("multipart login Location = %q, want /dashboard (the body's next field)", loc)
	}
	var minted bool
	for _, c := range resp.Cookies() {
		if c.Name == "test_session" && c.Value != "" {
			if _, err := mgr.SessionStore().Get(t.Context(), c.Value); err == nil {
				minted = true
			}
		}
	}
	if !minted {
		t.Error("multipart login minted no session")
	}

	resp = postMultipart(t, srv.URL, "/auth/logout", multipartField{name: "next", value: "/bye"})
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || loc != "/bye" {
		t.Errorf("multipart logout = %d Location %q, want 303 to /bye", resp.StatusCode, loc)
	}
}

// Property: the duplicate-field refusal covers multipart too. net/url
// and the multipart reader keep the FIRST of a repeated field while the
// JSON surface keeps the LAST, so one body naming email twice must be
// refused rather than sign in whichever account the parser picked.
func TestMultipartLoginRefusesDuplicateAndFileFields(t *testing.T) {
	srv, _ := setupAuthTestServer(t)
	postMultipart(t, srv.URL, "/auth/register",
		multipartField{name: "email", value: "dup@example.com"},
		multipartField{name: "password", value: "hunter22"})

	for name, fields := range map[string][]multipartField{
		"duplicate email": {
			{name: "email", value: "dup@example.com"},
			{name: "email", value: "other@example.com"},
			{name: "password", value: "hunter22"},
		},
		"duplicate password": {
			{name: "email", value: "dup@example.com"},
			{name: "password", value: "hunter22"},
			{name: "password", value: "x"},
		},
		"file part": {
			{name: "email", value: "dup@example.com"},
			{name: "password", value: "hunter22"},
			{name: "avatar", value: "data", file: true},
		},
	} {
		resp := postMultipart(t, srv.URL, "/auth/login", fields...)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: multipart login = %d, want 400", name, resp.StatusCode)
		}
		for _, c := range resp.Cookies() {
			if c.Name == "test_session" && c.Value != "" {
				t.Errorf("%s: a session cookie was minted", name)
			}
		}
	}
}

// Property: forgot-password answers exactly one JSON document. The
// uniform {"sent":true} write was deferred before the length check ran,
// so an over-long address answered 400 {...} followed by {"sent":true}
// in the same body.
func TestForgotPasswordOverlongEmailWritesOneDocument(t *testing.T) {
	store := newUserStoreWithPassword()
	mgr := New(AuthConfig{UserStore: store, DevMode: true, SessionTTL: 3600e9})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewPasswordResetPlugin(PasswordResetConfig{BaseURL: "http://localhost", DevMode: true}))
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	r := mountRoutes(mgr)

	long := strings.Repeat("a", maxEmailLen) + "@example.com"
	body, _ := json.Marshal(map[string]string{"email": long})
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	dec := json.NewDecoder(bytes.NewReader(w.Body.Bytes()))
	var docs int
	for {
		var v any
		if err := dec.Decode(&v); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("body is not JSON: %v; body=%q", err, w.Body.String())
		}
		docs++
	}
	if docs != 1 {
		t.Errorf("forgot-password with an over-long email wrote %d JSON documents (status %d), want 1; body=%q", docs, w.Code, w.Body.String())
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (length does not depend on the account, so it opens no oracle)", w.Code)
	}
}
