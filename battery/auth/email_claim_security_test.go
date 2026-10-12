package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Registration and an unverified IdP email both create an account for an
// address nobody proved they own. Before email_verified existed, the
// mailbox owner's first magic link signed into that account with the
// squatter's password and sessions still live, and a verified IdP login
// auto-linked into it. These tests pin the claim semantics that replaced
// that: proving the mailbox claims an unverified account and evicts every
// credential the squatter planted.

type claimLinkSender struct {
	mu   sync.Mutex
	urls []string
}

func (s *claimLinkSender) SendMagicLink(_ context.Context, _, link string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.urls = append(s.urls, link)
	return nil
}

func (s *claimLinkSender) last(t *testing.T) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.urls) == 0 {
		t.Fatal("no magic link sent")
	}
	u, err := url.Parse(s.urls[len(s.urls)-1])
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	return u.Query().Get("token")
}

type claimRig struct {
	t        *testing.T
	mgr      *AuthManager
	store    *EntityUserStore
	r        *router.Router
	links    *claimLinkSender
	resets   *stubEmailSender
	verifies *stubEmailSender
	corp     *mockProvider
	google   *mockProvider
	tokens   APITokenStore
}

func newClaimRig(t *testing.T) *claimRig {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	store := NewEntityUserStore(db, "claim_users")
	rig := &claimRig{
		t:        t,
		store:    store,
		links:    &claimLinkSender{},
		resets:   &stubEmailSender{},
		verifies: &stubEmailSender{},
		corp: &mockProvider{
			name:      "corp",
			tokenResp: &OAuth2Token{AccessToken: "corp-access"}, // not-a-secret: mock provider fixture
			userResp:  &OAuth2UserInfo{ID: "attacker-sub", Email: "victim@example.com"},
		},
		google: &mockProvider{
			name:      "google",
			tokenResp: &OAuth2Token{AccessToken: "google-access"}, // not-a-secret: mock provider fixture
			userResp:  &OAuth2UserInfo{ID: "victim-sub", Email: "victim@example.com", EmailVerified: true},
		},
	}
	rig.mgr = New(AuthConfig{
		JWTSecret:           "claim-secret-0123456789abcdef-0123456789abcdef",
		SessionCookie:       "session_id",
		SessionTTL:          24 * time.Hour,
		UserStore:           store,
		AllowInMemoryStores: true,
	})
	rig.mgr.Use(NewCorePlugin())
	tokens, err := NewSQLAPITokenStore(db)
	if err != nil {
		t.Fatalf("token store: %v", err)
	}
	rig.tokens = tokens
	rig.mgr.Use(NewTokensPlugin(tokens))
	rig.mgr.Use(NewMagicLinkPlugin(MagicLinkConfig{EmailSender: rig.links, BaseURL: "http://localhost"}))
	rig.mgr.Use(NewPasswordResetPlugin(PasswordResetConfig{BaseURL: "http://localhost", EmailSender: rig.resets}))
	rig.mgr.Use(NewEmailVerificationPlugin(EmailVerificationConfig{BaseURL: "http://localhost", EmailSender: rig.verifies}))
	rig.mgr.Use(NewOAuth2Plugin(OAuth2Config{
		Providers:   map[string]OAuth2Provider{"corp": rig.corp, "google": rig.google},
		StateSecret: "claim-state-secret-0123456789",
	}))
	if err := rig.mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	rig.r = router.New()
	rig.mgr.RegisterRoutes(rig.r)
	return rig
}

func (c *claimRig) do(method, path, body, ctype, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if cookie != "" {
		req.Header.Set("Cookie", "session_id="+cookie)
	}
	w := httptest.NewRecorder()
	c.r.ServeHTTP(w, req)
	return w
}

func (c *claimRig) json(path string, body any, cookie string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	return c.do(http.MethodPost, path, string(b), "application/json", cookie)
}

func sessionCookie(w *httptest.ResponseRecorder) string {
	for _, ck := range w.Result().Cookies() {
		if ck.Name == "session_id" {
			return ck.Value
		}
	}
	return ""
}

// login posts the password form and returns the session cookie, or "" when
// the login was refused.
func (c *claimRig) login(email, pw string) string {
	w := c.json("/auth/login", map[string]string{"email": email, "password": pw}, "")
	if w.Code != http.StatusOK {
		return ""
	}
	return sessionCookie(w)
}

// magicLink runs send + confirm for email and returns the minted session's
// user id.
func (c *claimRig) magicLink(email string) string {
	c.t.Helper()
	if w := c.json("/auth/magic-link/send", map[string]string{"email": email}, ""); w.Code != http.StatusOK {
		c.t.Fatalf("magic-link send: %d %s", w.Code, w.Body.String())
	}
	form := url.Values{"token": {c.links.last(c.t)}}
	w := c.do(http.MethodPost, "/auth/magic-link/verify", form.Encode(), "application/x-www-form-urlencoded", "")
	if w.Code != http.StatusFound {
		c.t.Fatalf("magic-link verify: %d %s", w.Code, w.Body.String())
	}
	return c.sessionUser(sessionCookie(w))
}

func (c *claimRig) sessionUser(token string) string {
	c.t.Helper()
	if token == "" {
		c.t.Fatal("no session cookie")
	}
	sess, err := c.mgr.SessionStore().Get(context.Background(), token)
	if err != nil {
		c.t.Fatalf("session get: %v", err)
	}
	return sess.UserID
}

// oauth drives redirect + callback for provider and returns the session's
// user id, or "" plus the status when the callback refused.
func (c *claimRig) oauth(provider string) (string, int) {
	c.t.Helper()
	w1 := httptest.NewRecorder()
	c.r.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/auth/oauth/"+provider, nil))
	if w1.Code != http.StatusFound {
		c.t.Fatalf("%s redirect: %d", provider, w1.Code)
	}
	loc := w1.Header().Get("Location")
	state := loc[strings.LastIndex(loc, "state=")+len("state="):]
	req := httptest.NewRequest(http.MethodGet, "/auth/oauth/"+provider+"/callback?state="+url.QueryEscape(state)+"&code=c", nil)
	for _, ck := range w1.Result().Cookies() {
		if ck.Name == oauthStateCookie {
			req.AddCookie(ck)
		}
	}
	w2 := httptest.NewRecorder()
	c.r.ServeHTTP(w2, req)
	if w2.Code != http.StatusFound {
		return "", w2.Code
	}
	return c.sessionUser(sessionCookie(w2)), w2.Code
}

func (c *claimRig) verified(uid string) bool {
	c.t.Helper()
	v, err := c.store.IsEmailVerified(context.Background(), uid)
	if err != nil {
		c.t.Fatalf("IsEmailVerified: %v", err)
	}
	return v
}

func (c *claimRig) linked(uid, provider string) bool {
	c.t.Helper()
	accts, err := c.store.ListAccounts(context.Background(), uid)
	if err != nil {
		c.t.Fatalf("ListAccounts: %v", err)
	}
	for _, a := range accts {
		if a.Provider == provider {
			return true
		}
	}
	return false
}

// P5R1: the attacker registers the victim's address and keeps a session;
// the victim's magic link must evict both the session and the password.
func TestMagicLinkClaimKillsSquatter(t *testing.T) {
	c := newClaimRig(t)
	const email, pw = "victim@example.com", "attacker-chosen-pw-1"
	if w := c.json("/auth/register", map[string]string{"email": email, "password": pw}, ""); w.Code != http.StatusAccepted {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	attacker := c.login(email, pw)
	uid := c.sessionUser(attacker)
	if c.verified(uid) {
		t.Fatal("registration marked the address verified")
	}
	ctx := context.Background()
	if _, _, err := IssueToken(ctx, c.tokens, TokenSpec{Name: "planted", OwnerKind: "user", OwnerID: uid, Scopes: []string{"a:read"}}); err != nil {
		t.Fatalf("issue: %v", err)
	}

	if got := c.magicLink(email); got != uid {
		t.Fatalf("magic link signed into %s, want the claimed account %s", got, uid)
	}
	if w := c.do(http.MethodGet, "/auth/me", "", "", attacker); w.Code == http.StatusOK {
		t.Fatalf("squatter session survived the claim: %s", w.Body.String())
	}
	if c.login(email, pw) != "" {
		t.Fatal("squatter password survived the claim")
	}
	list, err := c.tokens.List(ctx, "user", uid)
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	for _, tk := range list {
		if tk.RevokedAt == nil {
			t.Fatal("squatter's API token survived the claim")
		}
	}
	if !c.verified(uid) {
		t.Fatal("claim did not mark the account verified")
	}
}

// A verified account is not reclaimed: its password and sessions stay.
func TestMagicLinkVerifiedSkipsClaim(t *testing.T) {
	c := newClaimRig(t)
	const email, pw = "owner@example.com", "owner-password-1"
	if w := c.json("/auth/register", map[string]string{"email": email, "password": pw}, ""); w.Code != http.StatusAccepted {
		t.Fatalf("register: %d", w.Code)
	}
	sess := c.login(email, pw)
	uid := c.sessionUser(sess)
	if err := c.store.MarkEmailVerified(context.Background(), uid); err != nil {
		t.Fatalf("MarkEmailVerified: %v", err)
	}
	c.magicLink(email)
	if w := c.do(http.MethodGet, "/auth/me", "", "", sess); w.Code != http.StatusOK {
		t.Fatalf("verified owner's session dropped: %d", w.Code)
	}
	if c.login(email, pw) == "" {
		t.Fatal("verified owner's password cleared")
	}
}

func TestNewMagicLinkUserIsVerified(t *testing.T) {
	c := newClaimRig(t)
	uid := c.magicLink("fresh@example.com")
	if !c.verified(uid) {
		t.Fatal("magic-link auto-create left the account unverified")
	}
}

// P5R2: an unverified IdP email pre-creates the account; a verified login
// for the same address must not merge into it.
func TestOAuthUnverifiedPrecreateNoMerge(t *testing.T) {
	c := newClaimRig(t)
	uid1, code := c.oauth("corp")
	if code != http.StatusFound {
		t.Fatalf("corp callback: %d", code)
	}
	if c.verified(uid1) {
		t.Fatal("unverified IdP email marked the account verified")
	}
	uid2, code := c.oauth("google")
	if uid2 == uid1 {
		t.Fatalf("verified login merged into the squatted account %s", uid1)
	}
	if code != http.StatusConflict {
		t.Fatalf("google callback: got %d, want 409", code)
	}
	if c.linked(uid1, "google") {
		t.Fatal("google identity linked into the squatted account")
	}
}

// The claim also drops the squatter's IdP link, after which the owner's
// verified IdP login may auto-link.
func TestClaimDropsSquatterOAuthLink(t *testing.T) {
	c := newClaimRig(t)
	uid1, _ := c.oauth("corp")
	if got := c.magicLink("victim@example.com"); got != uid1 {
		t.Fatalf("magic link signed into %s, want %s", got, uid1)
	}
	if c.linked(uid1, "corp") {
		t.Fatal("squatter's corp link survived the claim")
	}
	if got, _ := c.oauth("corp"); got == uid1 {
		t.Fatal("squatter's IdP identity still signs into the claimed account")
	}
	if got, code := c.oauth("google"); got != uid1 {
		t.Fatalf("owner's verified login after claim: user %q code %d, want %s", got, code, uid1)
	}
}

// A completed reset on an unverified account is the same mailbox proof.
func TestResetClaimsUnverifiedAccount(t *testing.T) {
	c := newClaimRig(t)
	uid1, _ := c.oauth("corp")
	if w := c.json("/auth/forgot-password", map[string]string{"email": "victim@example.com"}, ""); w.Code != http.StatusOK {
		t.Fatalf("forgot-password: %d", w.Code)
	}
	_, body := c.resets.snapshot()
	tok := extractTokenFromBody(body)
	if w := c.json("/auth/reset-password", map[string]string{"token": tok, "password": "owner-new-pw-1"}, ""); w.Code != http.StatusOK {
		t.Fatalf("reset-password: %d %s", w.Code, w.Body.String())
	}
	if c.linked(uid1, "corp") {
		t.Fatal("squatter's corp link survived the reset claim")
	}
	if !c.verified(uid1) {
		t.Fatal("reset did not mark the account verified")
	}
}

// The registrant of an address can mail a verification link to its real
// owner. The owner's click, or a scanner's prefetch, must not mark the
// registrant's account verified: that would end the claim and open the
// verified-login auto-link. Only the account's own session redeems it.
func TestVerifyEmailNeedsOwnSession(t *testing.T) {
	c := newClaimRig(t)
	const email, pw = "victim@example.com", "attacker-chosen-pw-1"
	c.json("/auth/register", map[string]string{"email": email, "password": pw}, "")
	attacker := c.login(email, pw)
	uid := c.sessionUser(attacker)
	c.json("/auth/register", map[string]string{"email": "other@example.com", "password": "bystander-pw-1"}, "")
	other := c.login("other@example.com", "bystander-pw-1")

	sendLink := func() string {
		if w := c.json("/auth/send-verification", map[string]string{}, attacker); w.Code != http.StatusOK {
			t.Fatalf("send-verification: %d %s", w.Code, w.Body.String())
		}
		_, body := c.verifies.snapshot()
		return extractTokenFromBody(body)
	}
	open := func(tok, cookie string) int {
		return c.do(http.MethodGet, "/auth/verify-email?token="+url.QueryEscape(tok), "", "", cookie).Code // not-a-secret: test-minted token
	}

	tok := sendLink()
	if code := open(tok, other); code != http.StatusForbidden {
		t.Fatalf("link opened by another account: got %d, want 403", code)
	}
	if code := open(tok, ""); code != http.StatusUnauthorized {
		t.Fatalf("sessionless open: got %d, want 401", code)
	}
	if c.verified(uid) {
		t.Fatal("a click without the registrant's session verified the account")
	}
	if code := open(tok, attacker); code != http.StatusOK {
		t.Fatalf("own-session open after a prefetch: got %d, want 200", code)
	}
	if !c.verified(uid) {
		t.Fatal("own-session open did not verify")
	}
}

// A linked IdP login asserting the account's own address verifies it, so
// legacy passwordless accounts regain auto-link. An unverified assertion,
// or a verified one for another address, does not.
func TestVerifiedLoginMarksAccount(t *testing.T) {
	c := newClaimRig(t)
	ctx := context.Background()
	u, err := c.store.CreateUserNoPassword(ctx, "victim@example.com", []string{"user"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.store.LinkOAuth(ctx, u.GetID(), "corp", "attacker-sub"); err != nil {
		t.Fatal(err)
	}
	if err := c.store.LinkOAuth(ctx, u.GetID(), "google", "victim-sub"); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.oauth("corp"); got != u.GetID() {
		t.Fatalf("corp step-1 login: %q", got)
	}
	if c.verified(u.GetID()) {
		t.Fatal("unverified IdP assertion marked the account verified")
	}
	c.google.userResp = &OAuth2UserInfo{ID: "victim-sub", Email: "other@example.com", EmailVerified: true}
	c.oauth("google")
	if c.verified(u.GetID()) {
		t.Fatal("verified assertion for another address marked the account")
	}
	c.google.userResp = &OAuth2UserInfo{ID: "victim-sub", Email: "victim@example.com", EmailVerified: true}
	c.oauth("google")
	if !c.verified(u.GetID()) {
		t.Fatal("verified login for the account's address did not mark it")
	}
}

// A users table from before email_verified gains the column with every
// existing row unverified.
func TestLegacyUsersStayUnverified(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, stmt := range []string{
		`CREATE TABLE legacy_users (id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL DEFAULT '', roles TEXT NOT NULL DEFAULT '[]', password_set INTEGER NOT NULL DEFAULT 0)`,
		`INSERT INTO legacy_users (id, email, password_hash, roles, password_set) VALUES ('u1', 'old@example.com', 'x', '[]', 1)`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	store := NewEntityUserStore(db, "legacy_users")
	for i := range 2 {
		if err := store.EnsureSchema(ctx); err != nil {
			t.Fatalf("EnsureSchema run %d: %v", i, err)
		}
	}
	v, err := store.IsEmailVerified(ctx, "u1")
	if err != nil {
		t.Fatalf("IsEmailVerified: %v", err)
	}
	if v {
		t.Fatal("migration marked a legacy row verified")
	}
	if err := store.MarkEmailVerified(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if v, _ := store.IsEmailVerified(ctx, "u1"); !v {
		t.Fatal("MarkEmailVerified did not stick")
	}
	if err := store.MarkEmailVerified(ctx, "nope"); err == nil {
		t.Fatal("MarkEmailVerified on a missing user returned nil")
	}
}

// The Postgres half: openPGForBattery's users table predates the column.
func TestLegacyPGUsersStayUnverified(t *testing.T) {
	db := openPGForBattery(t)
	ctx := context.Background()
	store := NewEntityUserStore(db, "users")
	u, err := store.CreateUser(ctx, "old@pg.test", "x", []string{"user"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	for i := range 2 {
		if err := store.EnsureSchema(ctx); err != nil {
			t.Fatalf("EnsureSchema run %d: %v", i, err)
		}
	}
	if v, err := store.IsEmailVerified(ctx, u.GetID()); err != nil || v {
		t.Fatalf("legacy row after migration: verified=%v err=%v", v, err)
	}
	if err := store.MarkEmailVerified(ctx, u.GetID()); err != nil {
		t.Fatalf("MarkEmailVerified: %v", err)
	}
	if v, _ := store.IsEmailVerified(ctx, u.GetID()); !v {
		t.Fatal("MarkEmailVerified did not stick")
	}
	if err := store.ClearPassword(ctx, u.GetID()); err != nil {
		t.Fatalf("ClearPassword: %v", err)
	}
	if has, _ := store.HasPassword(ctx, u.GetID()); has {
		t.Fatal("ClearPassword left password_set true")
	}
}
