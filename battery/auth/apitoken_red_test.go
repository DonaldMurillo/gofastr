//go:build red

package auth

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only;
// no fix applied).
//
// CONTRACT-QUESTION red: immortal-by-default user tokens are a DOCUMENTED
// contract — TokenSpec.TTL reads `TTL time.Duration // 0 = no expiry`
// (apitoken.go:94), and validateTokenSpec's own rejection message says
// "omit it for a token that does not expire" (apitoken.go:148). Delete or
// promote per maintainer decision: either the HTTP surface refuses an
// omitted ttl_seconds (or stamps a bounded default on it) while TokenSpec
// keeps its documented programmatic meaning, or the contract stands and this
// test is deleted.
//
// Property: every credential a user can self-mint has a bounded default
// lifetime. The package holds itself to this everywhere else: sessions 7d,
// JWTs 1h, magic links 15m, reset tokens 1h, verification tokens 24h, embed
// grants 15m capped by GrantMaxAge.
//
// Surfaces: apitoken_routes.go:createTokenHandler:126-133 — body.TTLSeconds
// is forwarded verbatim; an omitted ttl_seconds decodes to 0. apitoken.go:
// validateTokenSpec:143-149 rejects only NEGATIVE TTLs ("Absent (zero) still
// means permanent; a stated negative does not"), IssueToken:187-190 leaves
// rec.ExpiresAt nil for TTL<=0. The handler's own comment (:93-95) calls the
// embed-grant consequence "a permanent `*:*` API token".
//
// Finding: POST /auth/tokens {"name":...} with no ttl_seconds mints a bearer
// credential that never expires — the strongest lifetime the endpoint can
// produce, handed out by default. A leak has no natural remediation horizon;
// revocation depends on the operator noticing.
//
// Severity: P2 if promoted — immortal bearer tokens by default at a
// user-reachable endpoint; but the behavior is documented, hence the
// contract question rather than a straight bug claim.
//
// Fix direction (if promoted): at the HTTP surface, default an omitted
// ttl_seconds to a bounded TTL or refuse the omission (400 naming
// ttl_seconds; leave TokenSpec's programmatic 0=no-expiry contract alone.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

func TestApiTokenRedDefaultExpiryBounded(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	mgr := New(AuthConfig{JWTSecret: "red-apitoken", DevMode: true})
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	plugin := NewTokensPlugin(ts)
	plugin.Init(mgr)

	alice := &BasicUser{ID: "alice", Email: "alice@example.com", Roles: []string{"user"}}
	req := httptest.NewRequest(http.MethodPost, "/auth/tokens", strings.NewReader(`{"name":"red-immortal","scopes":["posts:read"]}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(handler.SetUser(req.Context(), alice))
	rec := httptest.NewRecorder()
	plugin.createTokenHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK && rec.Code != http.StatusBadRequest {
		t.Fatalf("create token: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ExpiresAt *time.Time `json:"expiresAt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, rec.Body.String())
	}

	// Documented-contract acceptance: a 400 refusal satisfies the property
	// (the omission is refused), and so does any non-nil expiry. What must
	// not happen is 200 + expiresAt:null from an OMITTED ttl_seconds.
	if rec.Code == http.StatusOK && resp.ExpiresAt == nil {
		t.Errorf("SECURITY: [immortal-default] POST /auth/tokens with ttl_seconds omitted minted a token with expiresAt=null (200, body=%s): the default lifetime of a user-minted bearer credential is 'never expires' — every other credential in the package gets a bounded default (session 7d, JWT 1h, magic link 15m, reset 1h, verification 24h, embed grant 15m). Documented contract (TokenSpec.TTL '0 = no expiry', apitoken.go:94): delete this test or bound the default at the HTTP surface", rec.Body.String())
	}
}
