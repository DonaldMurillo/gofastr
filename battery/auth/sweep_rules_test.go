package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/router"
)

func TestAccountsRefusePendingSession(t *testing.T) {
	ctx := context.Background()
	store := newLinkingStore()
	user, err := store.CreateUser(ctx, "p@example.com", "", []string{"user"})
	if err != nil {
		t.Fatal(err)
	}
	mgr := New(AuthConfig{SessionTTL: time.Hour, SessionCookie: "session_id", UserStore: store, DevMode: true})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewAccountsPlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	sess, _ := mgr.SessionStore().Create(ctx, user.GetID(), time.Hour)
	_ = mgr.SessionStore().(SessionPendingMarker).MarkPendingTwoFactor(ctx, sess.Token)
	r := router.New()
	mgr.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/auth/accounts", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sess.Token})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("accounts with a pending session = %d, want 403", w.Code)
	}
}

func TestIssueTokenRefusesBadNameAndNegativeTTL(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	for name, spec := range map[string]TokenSpec{
		"nul name":     {Name: "a\x00b", OwnerKind: OwnerKindUser, OwnerID: "u1"},
		"long name":    {Name: strings.Repeat("n", maxTokenNameBytes+1), OwnerKind: OwnerKindUser, OwnerID: "u1"},
		"negative ttl": {Name: "ok", OwnerKind: OwnerKindUser, OwnerID: "u1", TTL: -time.Second},
	} {
		if _, _, err := IssueToken(context.Background(), ts, spec); !errors.Is(err, ErrInvalidTokenSpec) {
			t.Errorf("%s: IssueToken = %v, want ErrInvalidTokenSpec", name, err)
		}
	}
}

func TestWithTokenPrefixPanicsOnBadBrand(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("WithTokenPrefix accepted an invalid brand")
		}
	}()
	WithTokenPrefix("Bad-Brand")
}

func TestHostResolverKeepsItsCanonicalizer(t *testing.T) {
	keepEmailResolver(t)
	datexport.RegisterIdentityResolver(datexport.IdentityEmail, datexport.DataIdentityResolver{
		Table: "members", IDColumn: "member_id", ValueColumn: "mail",
		Canonicalize: func(s string) string { return "host:" + s },
	})
	mgr := New(AuthConfig{JWTSecret: "x", DevMode: true, UserStore: wrappedUsers{newMemoryUserStore()}, AllowInMemoryStores: true})
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.OnStop(context.Background()) })
	r, _ := datexport.ResolveIdentity(datexport.IdentityEmail)
	if r.Canonicalize == nil || r.Canonicalize("x") != "host:x" {
		t.Error("Init replaced the host resolver's own canonicalizer")
	}
}

func TestEntitySessionStoreExpiredIsNotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewEntitySessionStore(db, "sessions")
	sess, err := store.Create(context.Background(), "u1", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, err := store.Get(context.Background(), sess.Token); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expired session Get = %v, want ErrSessionNotFound", err)
	}
}

func TestOverlongShapedEmailRefused(t *testing.T) {
	mgr := New(AuthConfig{JWTSecret: "s", DevMode: true, UserStore: newMemoryUserStore()})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	email := strings.Repeat("a", maxEmailLen) + "@x.io"
	rec := (&cookieJar{}).do(mountRoutes(mgr), http.MethodPost, "/auth/register",
		map[string]string{"email": email, "password": "supersecret1"}, "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("register with a %d-byte address = %d, want 400", len(email), rec.Code)
	}
}

func TestCanonicalizerOverrideOutputMustBeStorable(t *testing.T) {
	if _, err := canonicalizeEmailWith(func(string) (string, error) { return "a\x00@x.io", nil }, "a@x.io"); err == nil {
		t.Error("an override returning NUL was accepted")
	}
}

func TestMagicLinkClaimRevokesSquatterAPITokens(t *testing.T) {
	c := newClaimRig(t)
	const email, pw = "victim@example.com", "attacker-chosen-pw-1"
	if w := c.json("/auth/register", map[string]string{"email": email, "password": pw}, ""); w.Code != http.StatusAccepted {
		t.Fatalf("register: %d", w.Code)
	}
	cookie := c.login(email, pw)
	uid := c.sessionUser(cookie)
	if _, _, err := IssueToken(context.Background(), c.tokens, TokenSpec{Name: "squat", OwnerKind: OwnerKindUser, OwnerID: uid}); err != nil {
		t.Fatal(err)
	}
	c.magicLink(email)
	list, err := c.tokens.List(context.Background(), OwnerKindUser, uid)
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range list {
		if tk.RevokedAt == nil {
			t.Errorf("token %s survived the mailbox owner's claim", tk.ID)
		}
	}
}

func TestEntityStoreGuardedUnlinkRefusesLastMethod(t *testing.T) {
	ctx := context.Background()
	store := NewEntityUserStore(setupTestDB(t), "users")
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	u, err := store.CreateUserNoPassword(ctx, "o@example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.LinkOAuth(ctx, u.GetID(), "google", "g1"); err != nil {
		t.Fatal(err)
	}
	if out, err := store.UnlinkOAuthGuarded(ctx, u.GetID(), "google"); err != nil || out != UnlinkRefusedLast {
		t.Errorf("unlinking the only method = %v, %v; want UnlinkRefusedLast", out, err)
	}
	if err := store.LinkOAuth(ctx, u.GetID(), "github", "h1"); err != nil {
		t.Fatal(err)
	}
	if out, err := store.UnlinkOAuthGuarded(ctx, u.GetID(), "google"); err != nil || out != UnlinkRemoved {
		t.Errorf("unlinking one of two = %v, %v; want UnlinkRemoved", out, err)
	}
}
