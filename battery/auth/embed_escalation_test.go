package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/embed"
)

// An embed grant resolves to the same context user a session does. That is the
// point — an embedded surface renders as its viewer. It is also why the grant
// must be refused wherever the app hands out authority that outlives it.
//
// The grant lives in a third party's page, readable by any script there and by
// anyone with devtools. It is deliberately bounded: one surface, one origin,
// one deadline. Every gate below would have converted it into something with
// none of those bounds.

func grantCtx(t *testing.T) context.Context {
	t.Helper()
	return embed.WithGrant(context.Background(), embed.Grant{
		Surface: "reports",
		Subject: "alice",
		Scopes:  []string{"reports:read"},
		Origin:  "https://acme.com",
	})
}

func TestEmbedGrantCannotMintAPITokens(t *testing.T) {
	p := &TokensPlugin{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/tokens", strings.NewReader(`{"scopes":["*:*"]}`))
	req = req.WithContext(handler.SetUser(grantCtx(t), &testEscalationUser{id: "alice"}))

	if _, ok := p.requireSessionUserID(rec, req); ok {
		t.Fatal("an embed grant was accepted for token management.\n" +
			"That turns a 15-minute, one-surface, one-origin credential into a\n" +
			"permanent *:* API token that outlives the grant's deadline, ignores its\n" +
			"scopes, and is invisible to the embed system.")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rec.Code)
	}
}

func TestEmbedGrantCannotPassMCPGates(t *testing.T) {
	ctx := handler.SetUser(grantCtx(t), &testEscalationUser{id: "alice", roles: []string{"admin"}})

	if err := MCPUser()(ctx); err == nil {
		t.Error("MCPUser admitted an embed grant — MCP tools run commands on the " +
			"caller's behalf, which no surface declares")
	}
	if err := MCPRole("admin")(ctx); err == nil {
		t.Error("MCPRole admitted an embed grant")
	}
	// An ordinary session with the same user still passes, or the gate is just broken.
	plain := handler.SetUser(context.Background(), &testEscalationUser{id: "alice", roles: []string{"admin"}})
	if err := MCPUser()(plain); err != nil {
		t.Errorf("MCPUser refused an ordinary session: %v", err)
	}
	if err := MCPRole("admin")(plain); err != nil {
		t.Errorf("MCPRole refused an ordinary session: %v", err)
	}
}

type testEscalationUser struct {
	id    string
	roles []string
}

func (u *testEscalationUser) GetID() string      { return u.id }
func (u *testEscalationUser) GetEmail() string   { return u.id + "@example.com" }
func (u *testEscalationUser) GetRoles() []string { return u.roles }

// The documented wiring is embeds.Middleware() outermost, then SessionMiddleware.
// An embed request carries no session cookie by construction, so it takes
// SessionMiddleware's anonymous branch — which used to clear the user and
// therefore erased the identity the grant had just established one layer out.
func TestSessionMiddlewareKeepsTheGrantSubject(t *testing.T) {
	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     newLinkingStore(),
		DevMode:       true,
	})

	var seen any
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen, _ = handler.GetUser(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/reports", nil)
	req = req.WithContext(handler.SetUser(grantCtx(t), &testEscalationUser{id: "alice"}))

	SessionMiddleware(mgr)(inner).ServeHTTP(httptest.NewRecorder(), req)

	if seen == nil {
		t.Fatal("SessionMiddleware cleared the embed grant's subject.\n" +
			"The documented order installs embeds.Middleware() outermost; an embed\n" +
			"request then has no cookie, takes the anonymous branch, and the handler\n" +
			"sees nobody — owner-scoped CRUD, policies, tenancy and audit all lose\n" +
			"the identity the grant established.")
	}
	if u, ok := seen.(*testEscalationUser); !ok || u.id != "alice" {
		t.Errorf("handler saw %#v, want the grant's subject", seen)
	}
}

// Same shape as the SessionMiddleware case: embeds.Middleware() deletes
// Authorization so no second credential competes with the grant, and RequireAuth
// then 401'd every embed request on any JWT-protected route — under exactly the
// middleware order the embed docs prescribe.
func TestRequireAuthAcceptsAVerifiedGrant(t *testing.T) {
	jwtAuth := NewJWTAuth("embed-order-test", time.Hour)

	var reached bool
	inner := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { reached = true })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/reports", nil)
	req = req.WithContext(handler.SetUser(grantCtx(t), &testEscalationUser{id: "alice"}))
	RequireAuth(jwtAuth)(inner).ServeHTTP(rec, req)

	if !reached {
		t.Fatalf("RequireAuth refused a request already authenticated by an embed grant: status %d", rec.Code)
	}

	// An ordinary request with no credential at all is still refused, or the
	// exemption has simply disabled the middleware.
	reached = false
	plain := httptest.NewRecorder()
	RequireAuth(jwtAuth)(inner).ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/reports", nil))
	if reached || plain.Code != http.StatusUnauthorized {
		t.Errorf("an uncredentialed request was admitted: reached=%v status=%d", reached, plain.Code)
	}
}

// A role belongs to the subject; a grant is delegated, scoped authority sitting
// in a third party's page. A grant minted for a read-only surface must not
// satisfy a role gate just because its subject happens to hold that role.
func TestRequireRoleRefusesAnEmbedGrant(t *testing.T) {
	var reached bool
	inner := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { reached = true })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req = req.WithContext(handler.SetUser(grantCtx(t), &testEscalationUser{id: "alice", roles: []string{"admin"}}))
	RequireRole("admin")(inner).ServeHTTP(rec, req)

	if reached {
		t.Fatal("a grant scoped to reports:read reached a RequireRole(\"admin\") route")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403", rec.Code)
	}

	// An ordinary session with the role still passes.
	reached = false
	plain := httptest.NewRecorder()
	ok := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	ok = ok.WithContext(handler.SetUser(context.Background(), &testEscalationUser{id: "alice", roles: []string{"admin"}}))
	RequireRole("admin")(inner).ServeHTTP(plain, ok)
	if !reached {
		t.Errorf("an ordinary admin session was refused: status %d", plain.Code)
	}
}

// A grant for a surface with no Resolve has no subject. Passing that through
// RequireAuth would hand the handler a request with no user while implying one
// was verified.
func TestRequireAuthRefusesASubjectlessGrant(t *testing.T) {
	var reached bool
	inner := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { reached = true })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/reports", nil)
	req = req.WithContext(handler.SetUser(grantCtx(t), nil)) // grant present, nobody resolved
	RequireAuth(NewJWTAuth("subjectless-test", time.Hour))(inner).ServeHTTP(rec, req)

	if reached {
		t.Fatal("RequireAuth passed an anonymous embed request to the handler")
	}
}
