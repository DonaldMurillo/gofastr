package embed

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A grant is minted for one surface, handed to a third party's page, and
// readable by anyone with devtools on that page. Middleware installs the
// subject's full authority, so the scopes the surface declared have to be
// enforceable somewhere or they decorate nothing.

func TestRequireScopeRefusesAGrantWithoutIt(t *testing.T) {
	h := middlewareHost(t)
	grant := grantFor(t, h) // minted with nil scopes → the surface's own set

	p := &probe{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/embed/dashboard/delete", nil)
	req.Header.Set(GrantHeader, grant)

	// Middleware authenticates; RequireScope decides what that identity reaches.
	h.Middleware()(h.RequireScope("admin")(p.handler())).ServeHTTP(rec, req)

	if p.reached {
		t.Fatal("a grant scoped to a reporting surface reached an admin route.\n" +
			"The subject behind the grant may well be an admin — that is the point:\n" +
			"the grant must not carry the subject's full authority off its surface.")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403", rec.Code)
	}
}

func TestRequireScopeAdmitsADeclaredScope(t *testing.T) {
	h := middlewareHost(t)
	grant := grantFor(t, h)

	p := &probe{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/embed/dashboard/data", nil)
	req.Header.Set(GrantHeader, grant)
	h.Middleware()(h.RequireScope("read")(p.handler())).ServeHTTP(rec, req)

	if !p.reached {
		t.Fatalf("a grant carrying 'read' was refused a read route: status %d", rec.Code)
	}
	if p.grant.Subject != "u-7" {
		t.Errorf("subject = %q, want u-7 — the gate must not cost the identity", p.grant.Subject)
	}
}

func TestRequireScopeLetsOrdinaryTrafficPast(t *testing.T) {
	// A first-party visitor has no grant. Refusing here would 403 every real
	// user of the route, which is a worse bug than the one being fixed.
	h := middlewareHost(t)
	p := &probe{}
	rec := httptest.NewRecorder()
	h.Middleware()(h.RequireScope("admin")(p.handler())).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/embed/dashboard/data", nil))

	if !p.reached {
		t.Fatalf("an ordinary request with no grant was refused: status %d", rec.Code)
	}
}

func TestRequireScopeRefusesAnUncheckedGrantHeader(t *testing.T) {
	// RequireScope installed WITHOUT Middleware in front of it. The context
	// carries no grant, so the naive reading is "not an embed request, pass",
	// which would make the gate skippable by the very caller it gates.
	h := middlewareHost(t)
	p := &probe{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/embed/dashboard/delete", nil)
	req.Header.Set(GrantHeader, grantFor(t, h))
	h.RequireScope("admin")(p.handler()).ServeHTTP(rec, req)

	if p.reached {
		t.Fatal("a grant-bearing request passed a scope gate that never saw the grant")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403", rec.Code)
	}
}

func TestMiddlewareDropsBearerCredentials(t *testing.T) {
	// An authenticator running INSIDE this middleware must find nothing to
	// authenticate. Otherwise a bearer token overwrites the grant's identity,
	// or an API token's scopes end up on the context under the grant subject's
	// name.
	h := middlewareHost(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/embed/dashboard/data", nil)
	req.Header.Set(GrantHeader, grantFor(t, h))
	req.Header.Set("Authorization", "Bearer someone-elses-jwt")
	req.Header.Set("X-API-Key", "gfsk_someone_elses_token")
	req.AddCookie(&http.Cookie{Name: "session", Value: "someone-elses-session"})

	var saw http.Header
	h.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		saw = r.Header.Clone()
	})).ServeHTTP(rec, req)

	for _, header := range []string{"Authorization", "X-API-Key", "Cookie"} {
		if saw.Get(header) != "" {
			t.Errorf("%s survived into an embed request: %q", header, saw.Get(header))
		}
	}
}

func TestVerifyRejectsAnEmptyKey(t *testing.T) {
	// HMAC accepts an empty key and computes a MAC anyone can reproduce. The
	// minting side has always refused one; a verifier that does not is a
	// signature check that verifies forgeries.
	now := time.Now()
	// Forged with the empty key, which is the whole point: an attacker who
	// knows the verifier's key is empty needs no secret to produce a token that
	// verifies. Signing under some OTHER key would make this test pass on the
	// signature mismatch alone and prove nothing.
	forged, err := sign(GrantPrefix, []byte{}, grantClaims{
		Surface: "dashboard", Origin: "https://acme.com",
		Expires: now.Add(time.Hour).Unix(), Deadline: now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := VerifyGrant(nil, forged, now); err == nil {
		t.Error("VerifyGrant accepted a token under a nil key")
	}
	// A separately forged NONCE. Passing the grant token above would have made
	// this assertion pass on the prefix mismatch alone, green even with the
	// empty-key guard removed, which is the whole defect it exists to catch.
	forgedNonce, err := sign(NoncePrefix, []byte{}, nonceClaims{
		Surface: "dashboard", ID: "n1", Origin: "https://acme.com",
		Expires: now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("sign nonce: %v", err)
	}
	if _, err := VerifyNonce([]byte{}, forgedNonce, now); err == nil {
		t.Error("VerifyNonce accepted a token under an empty key")
	}
}

func TestGrantMaxAgeMustExceedGrantTTL(t *testing.T) {
	// Equality produces exactly the failure the guard names: the grant is born
	// at its deadline and every refresh clamps back to it, so the loop never
	// moves forward. It read like a legal configuration.
	_, err := New(Config{
		Surfaces:    []Surface{{Name: "dashboard", Screen: testScreen{"/d"}, Origins: []string{"https://acme.com"}}},
		BurnStore:   NewMemoryBurnStore(),
		GrantTTL:    15 * time.Minute,
		GrantMaxAge: 15 * time.Minute,
	})
	if err == nil {
		t.Fatal("GrantMaxAge == GrantTTL was accepted; every grant is born at its " +
			"deadline and refresh can never make progress")
	}
}
