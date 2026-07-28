package embed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

func middlewareHost(t *testing.T, mutate ...func(*Config)) *Host {
	t.Helper()
	return testHost(t, append([]func(*Config){func(c *Config) {
		c.Resolve = func(_ context.Context, subject string) (any, error) { return subject, nil }
	}}, mutate...)...)
}

func grantFor(t *testing.T, h *Host) string {
	t.Helper()
	tok, err := h.MintNonce("dashboard", "u-7", "https://acme.com", nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	res, err := h.Exchange(context.Background(), tok, "https://acme.com")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	return res.Grant
}

// probe records what the wrapped handler actually saw.
type probe struct {
	reached bool
	user    any
	grant   Grant
	hasGrant,
	sawCookie bool
}

func (p *probe) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.reached = true
		p.user, _ = ctxUser(r.Context())
		p.grant, p.hasGrant = GrantFromContext(r.Context())
		p.sawCookie = r.Header.Get("Cookie") != ""
		w.WriteHeader(http.StatusOK)
	})
}

func TestMiddlewarePassesThroughOrdinaryRequests(t *testing.T) {
	h := middlewareHost(t)
	p := &probe{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/posts", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "abc"})
	h.Middleware()(p.handler()).ServeHTTP(rec, req)

	if !p.reached || rec.Code != http.StatusOK {
		t.Fatalf("an ordinary request was not passed through: status %d", rec.Code)
	}
	if p.hasGrant {
		t.Error("an ordinary request carries a grant on its context")
	}
	if !p.sawCookie {
		t.Error("the middleware stripped a cookie from a request that is not an embed request")
	}
}

// An island RPC from inside a frame reaches an ORDINARY app route. That is
// where the surface stops being authenticated unless something reads the grant.
func TestMiddlewareAuthenticatesAGrantCarryingRequest(t *testing.T) {
	h := middlewareHost(t)
	grant := grantFor(t, h)

	p := &probe{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/posts", nil)
	req.Header.Set(GrantHeader, grant)
	h.Middleware()(p.handler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if got, _ := p.user.(string); got != "u-7" {
		t.Fatalf("handler saw user %v, want the grant's subject", p.user)
	}
	if !p.hasGrant || !p.grant.HasScope("read") {
		t.Fatalf("the grant did not reach the handler: %+v hasGrant=%v", p.grant, p.hasGrant)
	}
}

// Same-site framing (app.acme.com inside www.acme.com) really does send the
// cookie to an app route. The grant is the only identity an embed request may
// have, so the cookie must not survive to the handler.
func TestMiddlewareDiscardsCookiesOnAnEmbedRequest(t *testing.T) {
	h := middlewareHost(t)
	grant := grantFor(t, h)

	p := &probe{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/posts", nil)
	req.Header.Set(GrantHeader, grant)
	req.AddCookie(&http.Cookie{Name: "__Host-gofastr-session", Value: "someone-elses-session"})
	h.Middleware()(p.handler()).ServeHTTP(rec, req)

	if p.sawCookie {
		t.Fatal("an embed request reached the handler with its cookie intact — the surface would render as the grant's subject and mutate as the cookie's user")
	}
	if got, _ := p.user.(string); got != "u-7" {
		t.Fatalf("handler saw user %v, want the grant's subject", p.user)
	}
}

// A presented-and-rejected credential must not become an anonymous visitor:
// that turns an expired grant into a silently wrong render.
func TestMiddlewareRefusesABadGrant(t *testing.T) {
	h := middlewareHost(t)
	nonce, err := h.MintNonce("dashboard", "u-7", "https://acme.com", nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}

	for name, tok := range map[string]string{
		"garbage":        "not-a-grant",
		"a nonce":        nonce,
		"wrong key":      "emg_eyJzIjoiZGFzaGJvYXJkIn0.AAAA",
		"empty-ish junk": "emg_.",
	} {
		p := &probe{}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/posts", nil)
		req.Header.Set(GrantHeader, tok)
		h.Middleware()(p.handler()).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, rec.Code)
		}
		if p.reached {
			t.Errorf("%s: reached the handler", name)
		}
	}
}

func TestMiddlewareFailsClosedWhenTheSubjectVanishes(t *testing.T) {
	h := middlewareHost(t, func(c *Config) {
		c.Resolve = func(context.Context, string) (any, error) { return nil, context.DeadlineExceeded }
	})
	grant := grantFor(t, h)

	p := &probe{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/posts", nil)
	req.Header.Set(GrantHeader, grant)
	h.Middleware()(p.handler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
	if p.reached {
		t.Fatal("the handler ran with an unresolvable subject")
	}
}

// ctxUser reads the user the middleware installed. The middleware's contract is
// that the app's usual current-user lookup works, and that lookup is
// core/handler.GetUser.
func ctxUser(ctx context.Context) (any, bool) {
	return handler.GetUser(ctx)
}

// CSRFExempt must not exempt a percent-encoded traversal that has the embed
// prefix when spelt literally but escapes it once cleaned.
//
// r.URL.Path is percent-decoded, so "/__gofastr/embed/%2e%2e/%2e%2e/admin/delete"
// arrives as "/__gofastr/embed/../../admin/delete" — it has the embed prefix as
// a raw string but path.Clean collapses it to "/admin/delete", which is not an
// embed route. Matching the raw path instead of the cleaned one exempted the
// traversal, and an exemption whose safety depends on another package's
// redirect behaviour is one refactor away from being wrong.
func TestCSRFExemptRejectsPathTraversal(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost,
		"/__gofastr/embed/%2e%2e/%2e%2e/admin/delete", nil)
	if CSRFExempt(req) {
		t.Fatal("CSRFExempt exempted a traversal path that escapes the embed prefix")
	}
}
