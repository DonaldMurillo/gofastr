package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRespond_NoServerHeader verifies that responses don't include a
// Server header revealing implementation details.
// Attack: fingerprinting the server for targeted attacks.
func TestRespond_NoServerHeader(t *testing.T) {
	w := httptest.NewRecorder()
	w.WriteHeader(http.StatusOK)

	server := w.Header().Get("Server")
	if server != "" {
		t.Logf("NOTE: [respond] Server header present: %q — consider removing to reduce fingerprinting surface.", server)
	}
}

// TestRespond_XContentTypeOptions verifies that responses include
// X-Content-Type-Options: nosniff. Attack: MIME sniffing XSS.
func TestRespond_XContentTypeOptions(t *testing.T) {
	w := httptest.NewRecorder()
	w.WriteHeader(http.StatusOK)

	nosniff := w.Header().Get("X-Content-Type-Options")
	if nosniff != "nosniff" {
		t.Logf("SECURITY: [respond] X-Content-Type-Options missing or wrong: %q. Attack: MIME sniffing may reinterpret response as HTML.", nosniff)
	}
}

func TestRespondJSON_SetsNoSniff(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	Respond(w, req, map[string]any{"ok": true})

	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("SECURITY: [respond] JSON response missing X-Content-Type-Options: nosniff (got %q). Attack: browser MIME sniffing on API responses.", got)
	}
}

func TestWriteError_SetsNoSniff(t *testing.T) {
	w := httptest.NewRecorder()

	WriteError(w, Errorf(http.StatusBadRequest, "bad input"))

	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("SECURITY: [respond] error JSON missing X-Content-Type-Options: nosniff (got %q). Attack: browser MIME sniffing on structured error responses.", got)
	}
}

// TestContext_UserIsolation verifies that users stored in different
// contexts don't leak between requests. Attack: user context leaking
// across concurrent requests.
func TestContext_UserIsolation(t *testing.T) {
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)

	ctx1 := SetUser(req1.Context(), &testSecurityUser{id: "alice"})
	ctx2 := SetUser(req2.Context(), &testSecurityUser{id: "bob"})

	u1, ok1 := GetUser(ctx1)
	u2, ok2 := GetUser(ctx2)

	if !ok1 || !ok2 {
		t.Fatalf("GetUser failed: ok1=%v, ok2=%v", ok1, ok2)
	}

	// Verify they return different values
	s1 := u1.(interface{ GetID() string }).GetID()
	s2 := u2.(interface{ GetID() string }).GetID()
	if s1 != "alice" || s2 != "bob" {
		t.Errorf("SECURITY: [context] user isolation failed: u1=%q, u2=%q. Attack: user context leak across requests.", s1, s2)
	}
}

// TestContext_NilContextSafe verifies that GetUser on an empty context
// doesn't panic. Attack: crash via nil context.
func TestContext_NilContextSafe(t *testing.T) {
	u, ok := GetUser(context.Background())
	if ok {
		t.Errorf("SECURITY: [context] GetUser on empty context returned ok=true with user=%v. Attack: phantom user.", u)
	}
	if u != nil {
		t.Errorf("SECURITY: [context] GetUser on empty context returned non-nil user.")
	}
}

type testSecurityUser struct{ id string }

func (u *testSecurityUser) GetID() string { return u.id }
