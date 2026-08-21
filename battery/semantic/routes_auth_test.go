package semantic

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// TestHandler_RejectsBearerWhenNoTokenConfigured pins the core auth bug: the
// old "presence-only" check accepted ANY non-empty Authorization header. With
// no real credential configured, a bearer token MUST be rejected (fail closed),
// not accepted on mere presence, otherwise an accidentally-unprotected mount
// is silently open to anyone who sends any header at all.
func TestHandler_RejectsBearerWhenNoTokenConfigured(t *testing.T) {
	h := Handler(errIndex{})
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	req.Header.Set("Authorization", "Bearer anything-at-all")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("presence-only auth: arbitrary bearer accepted (status %d, want 401) — "+
			"must fail closed when no token is configured", rec.Code)
	}
}

// TestHandler_NoConfigFailsClosedOnEveryRoute asserts that with no token and no
// insecure opt-in, every route rejects an authenticated-looking request, the
// mount is never silently open. /stats and DELETE /doc have no body-shape gate,
// so they reach the auth check directly.
func TestHandler_NoConfigFailsClosedOnEveryRoute(t *testing.T) {
	h := Handler(errIndex{})
	cases := []struct{ method, target string }{
		{http.MethodGet, "/stats"},
		{http.MethodDelete, "/doc/x"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.target, nil)
		req.Header.Set("Authorization", "Bearer whatever")
		req.SetPathValue("id", "x")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with no token configured: status %d, want 401 (fail closed)",
				c.method, c.target, rec.Code)
		}
	}
}

// TestHandler_WrongBearerTokenRejected verifies a configured token actually
// verifies the credential, a non-matching bearer is rejected with 401.
func TestHandler_WrongBearerTokenRejected(t *testing.T) {
	h := Handler(errIndex{}, WithAuthToken("secret"))
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong bearer accepted: status %d, want 401", rec.Code)
	}
}

// TestHandler_CorrectBearerTokenAccepted verifies the happy path: the exact
// configured token is accepted.
func TestHandler_CorrectBearerTokenAccepted(t *testing.T) {
	h := Handler(errIndex{}, WithAuthToken("secret"))
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct bearer rejected: status %d, want 200", rec.Code)
	}
}

// TestHandler_InsecureOptInServesWithoutToken verifies the explicit dev opt-in
// ([WithInsecureDisabledAuth]) is the only way to serve without a token.
func TestHandler_InsecureOptInServesWithoutToken(t *testing.T) {
	h := Handler(errIndex{}, WithInsecureDisabledAuth())
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insecure opt-in rejected an unauthenticated request: status %d, want 200", rec.Code)
	}
}

// TestPlugin_UnconfiguredMountsFailClosed mounts the framework plugin with no
// token configured and asserts the mounted routes reject an arbitrary bearer,
// no silent open mount. (The plugin's mounted handler inherits Handler's
// fail-closed policy.)
func TestPlugin_UnconfiguredMountsFailClosed(t *testing.T) {
	idx, err := Open(Options{Embedder: NewStubEmbedder(32)})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "sem-auth-test"}))
	app.RegisterPlugin(NewPlugin(idx))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/semantic/stats", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer anything")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unconfigured plugin accepted arbitrary bearer (%d, want 401) — must fail closed",
			resp.StatusCode)
	}
}

// TestPlugin_WithAuthTokenVerifiesCredential mounts the plugin with a token and
// asserts a correct bearer is accepted while a wrong one is rejected.
func TestPlugin_WithAuthTokenVerifiesCredential(t *testing.T) {
	idx, err := Open(Options{Embedder: NewStubEmbedder(32)})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "sem-auth-test2"}))
	app.RegisterPlugin(NewPlugin(idx).WithAuthToken("s3cr3t"))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	defer srv.Close()

	do := func(bearer string) int {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/semantic/stats", nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if code := do("s3cr3t"); code != http.StatusOK {
		t.Fatalf("correct plugin bearer: status %d, want 200", code)
	}
	if code := do("wrong"); code != http.StatusUnauthorized {
		t.Fatalf("wrong plugin bearer: status %d, want 401", code)
	}
	if code := do(""); code != http.StatusUnauthorized {
		t.Fatalf("missing plugin bearer: status %d, want 401", code)
	}
}
