package auth

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestArgon2PHCRefusesOversizedParams(t *testing.T) {
	salt := base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef"))
	key := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	for _, params := range []string{"m=2097152,t=3,p=2", "m=65536,t=101,p=2", "m=65536,t=3,p=17"} {
		if _, ok := parseArgon2PHC("$argon2id$v=19$" + params + "$" + salt + "$" + key); ok {
			t.Errorf("a stored hash with %s parsed", params)
		}
	}
}

func TestMailQueueDetachesFromRequestCancel(t *testing.T) {
	q := newMailQueue("test")
	defer q.stop(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := make(chan error, 1)
	q.enqueue(ctx, "a@example.com", func(sctx context.Context) { got <- sctx.Err() })
	if err := <-got; err != nil {
		t.Errorf("a send queued from a cancelled request ran cancelled: %v", err)
	}
}

func TestMemorySessionStoreRefusesNegativeTTL(t *testing.T) {
	if _, err := NewMemorySessionStore().Create(context.Background(), "u1", -time.Second); err == nil {
		t.Error("a negative ttl minted a session")
	}
}

func TestSessionMiddlewareLoadsNoPendingUser(t *testing.T) {
	store := newMemoryUserStore()
	u := seedUser(t, store, "p@example.com", "supersecret1")
	mgr := New(AuthConfig{JWTSecret: "s", DevMode: true, UserStore: store})
	sess, _ := mgr.SessionStore().Create(context.Background(), u.GetID(), time.Hour)
	_ = mgr.SessionStore().(SessionPendingMarker).MarkPendingTwoFactor(context.Background(), sess.Token)
	var got User
	h := SessionMiddleware(mgr)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = GetCurrentUser(r.Context()) }))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: mgr.Config().SessionCookie, Value: sess.Token})
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got != nil {
		t.Errorf("a pending session loaded user %s", got.GetID())
	}
}

func TestResetOfVerifiedAccountEvictsEverything(t *testing.T) {
	c := newClaimRig(t)
	const email, pw = "victim@example.com", "first-password-1"
	if w := c.json("/auth/register", map[string]string{"email": email, "password": pw}, ""); w.Code != http.StatusAccepted {
		t.Fatalf("register: %d", w.Code)
	}
	cookie := c.login(email, pw)
	uid := c.sessionUser(cookie)
	ctx := context.Background()
	if err := c.store.MarkEmailVerified(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if err := c.store.LinkOAuth(ctx, uid, "corp", "planted-1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := IssueToken(ctx, c.tokens, TokenSpec{Name: "planted", OwnerKind: OwnerKindUser, OwnerID: uid}); err != nil {
		t.Fatal(err)
	}
	if w := c.json("/auth/forgot-password", map[string]string{"email": email}, ""); w.Code != http.StatusOK {
		t.Fatalf("forgot-password: %d", w.Code)
	}
	_, body := c.resets.snapshot()
	if w := c.json("/auth/reset-password", map[string]string{"token": extractTokenFromBody(body), "password": "owner-new-pw-1"}, ""); w.Code != http.StatusOK {
		t.Fatalf("reset-password: %d %s", w.Code, w.Body.String())
	}
	if _, err := c.mgr.SessionStore().Get(ctx, cookie); err == nil {
		t.Error("a session from before the reset still resolves")
	}
	if c.linked(uid, "corp") {
		t.Error("an OAuth link from before the reset survived it")
	}
	list, _ := c.tokens.List(ctx, OwnerKindUser, uid)
	for _, tk := range list {
		if tk.RevokedAt == nil {
			t.Errorf("API token %s survived the reset", tk.ID)
		}
	}
}

func TestOAuthRedirectSendsPKCEChallenge(t *testing.T) {
	mgr, _ := newOAuth2Manager(t, NewGoogleProvider("cid", "csec", "http://localhost/cb"))
	w := httptest.NewRecorder()
	mountOAuth2Routes(mgr).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/oauth/google", nil))
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil || loc.Query().Get("code_challenge") == "" || loc.Query().Get("code_challenge_method") != "S256" {
		t.Errorf("provider redirect %q carries no S256 challenge", w.Header().Get("Location"))
	}
}

func TestOAuthCallbackRefusesAnotherFlowsCookie(t *testing.T) {
	mgr, _ := newOAuth2Manager(t, &mockProvider{
		name:      "mock",
		tokenResp: &OAuth2Token{AccessToken: "tok"},
		userResp:  &OAuth2UserInfo{ID: "attacker-1", Email: "attacker@evil.example"},
	})
	r := mountOAuth2Routes(mgr)
	start := func() (string, []*http.Cookie) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/oauth/mock", nil))
		loc, _ := url.Parse(w.Header().Get("Location"))
		return loc.Query().Get("state"), w.Result().Cookies()
	}
	attackerState, _ := start()
	_, victimCookies := start()
	cb := httptest.NewRequest(http.MethodGet, "/auth/oauth/mock/callback?state="+url.QueryEscape(attackerState)+"&code=abc", nil)
	for _, c := range victimCookies {
		cb.AddCookie(c)
	}
	cw := httptest.NewRecorder()
	r.ServeHTTP(cw, cb)
	if cw.Code == http.StatusFound || cw.Code == http.StatusOK {
		t.Errorf("a callback whose state cookie belongs to another flow = %d, want a refusal", cw.Code)
	}
}
