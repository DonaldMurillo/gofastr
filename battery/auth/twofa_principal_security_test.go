package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// RequireTwoFA used to judge the first session cookie on the request while
// the handler ran as whatever principal SessionMiddleware or RequireAuth
// had put in the context. Two cookies (an attacker's pending session first,
// then the victim's pre-enrollment session) or a victim JWT next to the
// attacker's own unenrolled session therefore ran a step-up route as the
// victim without the victim's second factor. The gate now judges the
// context principal and needs a session cookie of that same user.

type twofaSplitRig struct {
	t       *testing.T
	mgr     *AuthManager
	twofa   *TwoFAPlugin
	r       *router.Router
	ran     string // user the protected handler last ran as
	victim  User
	attackr User
}

func newTwoFASplitRig(t *testing.T) *twofaSplitRig {
	t.Helper()
	ctx := context.Background()
	store := newMemoryUserStore()
	rig := &twofaSplitRig{t: t}
	for _, who := range []string{"victim", "attacker"} {
		hash, err := HashPassword(who + "-password-1")
		if err != nil {
			t.Fatalf("hash: %v", err)
		}
		u, err := store.CreateUser(ctx, who+"@example.com", hash, []string{"user"})
		if err != nil {
			t.Fatalf("seed %s: %v", who, err)
		}
		if who == "victim" {
			rig.victim = u
		} else {
			rig.attackr = u
		}
	}
	rig.mgr = New(AuthConfig{
		JWTSecret:           "split-secret-0123456789abcdef-0123456789abcdef",
		SessionCookie:       "session_id",
		SessionTTL:          24 * time.Hour,
		UserStore:           store,
		AllowInMemoryStores: true,
	})
	rig.twofa = NewTwoFAPlugin(TwoFAConfig{RateLimit: &RateLimiterConfig{MaxAttempts: 1000, Window: time.Minute, BlockDuration: time.Minute}})
	rig.mgr.Use(NewCorePlugin())
	rig.mgr.Use(rig.twofa)
	rig.mgr.Use(NewAccountsPlugin())
	if err := rig.mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	rig.r = router.New()
	rig.mgr.RegisterRoutes(rig.r)
	h := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if u := GetCurrentUser(req.Context()); u != nil {
			rig.ran = u.GetID()
		}
		_, _ = w.Write([]byte("ok"))
	})
	rig.r.Get("/protected", SessionMiddleware(rig.mgr)(rig.twofa.RequireTwoFA()(h)))
	rig.r.Get("/protected-jwt", RequireAuth(rig.mgr.JWT())(rig.twofa.RequireTwoFA()(h)))
	return rig
}

func (g *twofaSplitRig) do(method, path, body, bearer string, cookies ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if len(cookies) > 0 {
		parts := make([]string, len(cookies))
		for i, v := range cookies {
			parts[i] = "session_id=" + v
		}
		req.Header.Set("Cookie", strings.Join(parts, "; "))
	}
	w := httptest.NewRecorder()
	g.r.ServeHTTP(w, req)
	return w
}

func (g *twofaSplitRig) login(who string) (cookie, jwt string, pending bool) {
	g.t.Helper()
	w := g.do(http.MethodPost, "/auth/login", fmt.Sprintf(`{"email":"%s@example.com","password":"%s-password-1"}`, who, who), "")
	if w.Code != http.StatusOK {
		g.t.Fatalf("login %s: %d %s", who, w.Code, w.Body.String())
	}
	var resp struct {
		Token             string `json:"token"`
		TwoFactorRequired bool   `json:"two_factor_required"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	for _, c := range w.Result().Cookies() {
		if c.Name == "session_id" {
			cookie = c.Value
		}
	}
	if cookie == "" {
		g.t.Fatalf("login %s: no session cookie", who)
	}
	return cookie, resp.Token, resp.TwoFactorRequired
}

// enroll enrolls who through session s and returns the TOTP secret.
func (g *twofaSplitRig) enroll(s string) string {
	g.t.Helper()
	w := g.do(http.MethodPost, "/auth/2fa/enroll", "", "", s)
	if w.Code != http.StatusOK {
		g.t.Fatalf("enroll: %d %s", w.Code, w.Body.String())
	}
	var er struct {
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &er)
	w = g.do(http.MethodPost, "/auth/2fa/verify", fmt.Sprintf(`{"code":%q}`, totpNow(er.Secret)), "", s)
	if w.Code != http.StatusOK {
		g.t.Fatalf("verify: %d %s", w.Code, w.Body.String())
	}
	return er.Secret
}

func totpNow(secret string) string {
	return GenerateTOTP(secret, uint64(time.Now().Unix()/30))
}

func TestRequireTwoFASplitCookiesRefused(t *testing.T) {
	g := newTwoFASplitRig(t)
	// Victim: T minted before enrollment, then enrolls through T2.
	T, _, _ := g.login("victim")
	T2, _, _ := g.login("victim")
	g.enroll(T2)

	// Attacker: enroll, hold pending P, step up through V, disable.
	A0, _, _ := g.login("attacker")
	secret := g.enroll(A0)
	P, _, p1 := g.login("attacker")
	V, _, p2 := g.login("attacker")
	if !p1 || !p2 {
		t.Fatalf("expected pending sessions: %v %v", p1, p2)
	}
	if w := g.do(http.MethodPost, "/auth/2fa/challenge", fmt.Sprintf(`{"code":%q}`, totpNow(secret)), "", V); w.Code != http.StatusOK {
		t.Fatalf("challenge: %d %s", w.Code, w.Body.String())
	}
	if w := g.do(http.MethodPost, "/auth/2fa/disable", "", "", V); w.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", w.Code, w.Body.String())
	}

	g.ran = ""
	w := g.do(http.MethodGet, "/protected", "", "", P, T)
	if w.Code == http.StatusOK || g.ran == g.victim.GetID() {
		t.Fatalf("[P, T] ran the step-up route as the victim: %d ran=%s", w.Code, g.ran)
	}
	// Control: the victim's stepped-up session passes.
	if w := g.do(http.MethodGet, "/protected", "", "", T2); w.Code != http.StatusOK {
		t.Fatalf("verified victim session refused: %d %s", w.Code, w.Body.String())
	}
}

func TestRequireTwoFAJWTPlusOtherCookie(t *testing.T) {
	g := newTwoFASplitRig(t)
	_, victimJWT, _ := g.login("victim")
	T2, _, _ := g.login("victim")
	g.enroll(T2)
	A, _, _ := g.login("attacker") // unenrolled

	g.ran = ""
	w := g.do(http.MethodGet, "/protected-jwt", "", victimJWT, A)
	if w.Code == http.StatusOK || g.ran == g.victim.GetID() {
		t.Fatalf("victim JWT + attacker cookie passed step-up: %d ran=%s", w.Code, g.ran)
	}
	// The attacker's own stepped-up session proves nothing about the
	// victim either (enrolling marks A verified).
	g.enroll(A)
	g.ran = ""
	w = g.do(http.MethodGet, "/protected-jwt", "", victimJWT, A)
	if w.Code == http.StatusOK || g.ran == g.victim.GetID() {
		t.Fatalf("victim JWT + attacker's verified cookie passed step-up: %d ran=%s", w.Code, g.ran)
	}
	// A JWT alone carries no session-bound assurance.
	if w := g.do(http.MethodGet, "/protected-jwt", "", victimJWT); w.Code == http.StatusOK {
		t.Fatalf("bare JWT passed step-up: %d", w.Code)
	}
	// Control: the JWT next to the victim's own stepped-up session passes.
	if w := g.do(http.MethodGet, "/protected-jwt", "", victimJWT, T2); w.Code != http.StatusOK {
		t.Fatalf("JWT + verified session refused: %d %s", w.Code, w.Body.String())
	}
}

// The 2FA self-service handlers act on the session a request carries. When
// an outer middleware already put a principal in the context, a cookie of a
// different user must not be the one acted on.
func TestTwoFADisableBoundToPrincipal(t *testing.T) {
	g := newTwoFASplitRig(t)
	_, attackerJWT, _ := g.login("attacker")
	T2, _, _ := g.login("victim")
	g.enroll(T2)

	wrapped := RequireAuth(g.mgr.JWT())(g.r)
	req := httptest.NewRequest(http.MethodPost, "/auth/2fa/disable", nil)
	req.Header.Set("Authorization", "Bearer "+attackerJWT)
	req.Header.Set("Cookie", "session_id="+T2)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Fatalf("disable acted on the victim's cookie under the attacker's principal: %d %s", w.Code, w.Body.String())
	}
	on, err := g.twofa.HasTwoFactorEnabled(context.Background(), g.victim.GetID())
	if err != nil || !on {
		t.Fatalf("victim 2FA turned off: on=%v err=%v", on, err)
	}
}

// The accounts, OAuth-link and send-verification handlers resolved their
// user from the first session cookie the same way. They share the binding.
func TestAccountsBoundToPrincipal(t *testing.T) {
	g := newTwoFASplitRig(t)
	_, attackerJWT, _ := g.login("attacker")
	V, _, _ := g.login("victim")

	wrapped := RequireAuth(g.mgr.JWT())(g.r)
	req := httptest.NewRequest(http.MethodGet, "/auth/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+attackerJWT)
	req.Header.Set("Cookie", "session_id="+V)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("accounts resolved the victim's cookie under the attacker's principal: %d %s", w.Code, w.Body.String())
	}
}

// Disabling 2FA drops the user's pending sessions. Left in place, one would
// sit inert until a later re-enrollment and then step up with the new
// factor, a login that predates the factor it is completing.
func TestTwoFADisableDropsPendingSessions(t *testing.T) {
	g := newTwoFASplitRig(t)
	A0, _, _ := g.login("attacker")
	g.enroll(A0)
	P, _, pending := g.login("attacker")
	if !pending {
		t.Fatal("expected a pending session")
	}
	if w := g.do(http.MethodPost, "/auth/2fa/disable", "", "", A0); w.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", w.Code, w.Body.String())
	}
	if _, err := g.mgr.SessionStore().Get(context.Background(), P); err == nil {
		t.Fatal("pending session survived the disable")
	}
	if _, err := g.mgr.SessionStore().Get(context.Background(), A0); err != nil {
		t.Fatalf("the disabling session itself was dropped: %v", err)
	}
}

func TestEntityStoreDeletesOnlyPending(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()
	store := NewEntitySessionStore(db, "sessions")
	full, err := store.Create(ctx, "u1", time.Hour)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	pending, err := store.Create(ctx, "u1", time.Hour)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	other, err := store.Create(ctx, "u2", time.Hour)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	for _, tok := range []string{pending.Token, other.Token} {
		if err := store.MarkPendingTwoFactor(ctx, tok); err != nil {
			t.Fatalf("mark pending: %v", err)
		}
	}
	n, err := store.DeletePendingByUser(ctx, "u1")
	if err != nil || n != 1 {
		t.Fatalf("DeletePendingByUser = %d, %v; want 1", n, err)
	}
	if _, err := store.Get(ctx, pending.Token); err == nil {
		t.Fatal("u1's pending session survived")
	}
	if _, err := store.Get(ctx, full.Token); err != nil {
		t.Fatalf("u1's full session was dropped: %v", err)
	}
	if _, err := store.Get(ctx, other.Token); err != nil {
		t.Fatalf("u2's pending session was dropped: %v", err)
	}
}
