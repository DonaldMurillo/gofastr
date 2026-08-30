package webbotauth

// middleware_test.go: the Verifier middleware's request-path behaviour:
// context annotation in observe mode, the never-blocks guarantee, and
// require mode's 403. The verified-signature path reuses the draft
// E.2.1 vector against a live TLS directory.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

func TestMiddleware_ObserveAnnotatesContextForVerifiedAgent(t *testing.T) {
	v := loadVector[wbaVector](t, "testdata/wba-e.2.1-ed25519-dictionary.json")
	res, _ := directoryServer(t, v.DirectoryHost, v.DirectoryBody)
	ver := testVerifier(res, time.Now(), false)

	var seen *Agent
	var ctxOK bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, ctxOK = AgentFromContext(r.Context()), true
		w.WriteHeader(http.StatusOK)
	})
	r := v.Request.toHTTPRequest(t)
	r.Header.Set("Signature-Input", v.SignatureInput)
	r.Header.Set("Signature", v.SignatureLabel+"=:"+v.Signature+":")
	rec := httptest.NewRecorder()
	ver.Middleware(next).ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("observe mode must not block, got %d", rec.Code)
	}
	if !ctxOK {
		t.Fatal("next handler never ran")
	}
	if seen == nil {
		t.Fatal("verified request carried no agent identity in context")
	}
	if seen.URL != v.ExpectedAgentURL {
		t.Errorf("agent URL = %q", seen.URL)
	}
}

func TestMiddleware_ObservePassesUnsignedThrough(t *testing.T) {
	ver := New(false, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if AgentFromContext(r.Context()) != nil {
			t.Error("unsigned request must not carry an agent identity")
		}
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	ver.Middleware(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("observe mode blocked an unsigned request: %d", rec.Code)
	}
}

func TestMiddleware_RequireBlocksUnverified(t *testing.T) {
	ver := New(true, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("require mode served an unverified request")
	})
	rec := httptest.NewRecorder()
	ver.Middleware(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("require mode returned %d, want 403", rec.Code)
	}
	if accept := rec.Header().Get("Accept-Signature"); !strings.Contains(accept, "web-bot-auth") {
		t.Errorf("Accept-Signature = %q", accept)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
		t.Errorf("Content-Type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "Web Bot Auth signature required") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestMiddleware_RequireBlocksInvalidSignature(t *testing.T) {
	v := loadVector[wbaVector](t, "testdata/wba-e.2.1-ed25519-dictionary.json")
	// Expired verification clock makes the otherwise-valid vector
	// invalid; require mode must refuse it like any unverified request.
	res, _ := directoryServer(t, v.DirectoryHost, v.DirectoryBody)
	ver := testVerifier(res, time.Unix(4889289601, 0), true)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("require mode served an invalid signature")
	})
	r := v.Request.toHTTPRequest(t)
	r.Header.Set("Signature-Input", v.SignatureInput)
	r.Header.Set("Signature", v.SignatureLabel+"=:"+v.Signature+":")
	rec := httptest.NewRecorder()
	ver.Middleware(next).ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("require mode returned %d for invalid signature, want 403", rec.Code)
	}
}

// The middleware must satisfy the framework's middleware type directly
// (it is installed as router middleware via app.Use).
var _ middleware.Middleware = (&Verifier{}).Middleware

// context propagation: WithAgent/AgentFromContext round-trip with an
// unrelated context.
func TestAgentContext_RoundTrip(t *testing.T) {
	if a := AgentFromContext(context.Background()); a != nil {
		t.Fatalf("background context yielded %v", a)
	}
	a := &Agent{URL: "https://x.test/.well-known/http-message-signatures-directory", KeyID: "k"}
	got := AgentFromContext(WithAgent(context.Background(), a))
	if got == nil || got.URL != a.URL || got.KeyID != a.KeyID {
		t.Fatalf("round-trip lost %+v -> %+v", a, got)
	}
}
