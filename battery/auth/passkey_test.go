package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/noderender"
	"github.com/DonaldMurillo/gofastr/core/router"
)

type passkeyFixture struct {
	mgr   *AuthManager
	users *memoryUserStore
	pk    *PasskeyPlugin
	twofa *TwoFAPlugin
	sink  *recordingSink
	mail  *stubEmailSender
	r     *router.Router
}

func newPasskeyFixture(t *testing.T) *passkeyFixture {
	t.Helper()
	return newPasskeyFixtureFor(t, "example.com", "https://example.com")
}

// newPasskeyFixtureFor builds the fixture for one relying party.
func newPasskeyFixtureFor(t *testing.T, rpID, origin string) *passkeyFixture {
	t.Helper()
	// Verified accounts: a reset of an unverified one is a claim, which
	// removes passkeys for its own reason.
	store := newUserStoreWithPassword()
	users := store.memoryUserStore
	sink := &recordingSink{}
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		AllowInMemoryStores: true,
		SessionTTL:          time.Hour,
		SessionCookie:       "session_id",
		UserStore:           store,
		AuditSink:           sink,
	})
	pk := NewPasskeyPlugin(PasskeyConfig{RPID: rpID, Origins: []string{origin}, OnSuccessURL: "/app", TwoFactorURL: "/2fa"})
	twofa := NewTwoFAPlugin(TwoFAConfig{})
	mail := &stubEmailSender{}
	mgr.Use(NewCorePlugin().WithTwoFactorURL("/login/2fa"))
	mgr.Use(twofa)
	mgr.Use(pk)
	mgr.Use(NewPasswordResetPlugin(PasswordResetConfig{BaseURL: "https://example.com", EmailSender: mail}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for _, u := range []*BasicUser{
		{ID: "u-alice", Email: "alice@example.com"},
		{ID: "u-bob", Email: "bob@example.com"},
	} {
		e := &storeEntry{user: u, hash: passwordPlaceholderHash, emailVerified: true}
		users.users[u.Email], users.byID[u.ID] = e, e
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	return &passkeyFixture{mgr: mgr, users: users, pk: pk, twofa: twofa, sink: sink, mail: mail, r: r}
}

// session mints a full session for userID and returns its cookie value.
func (f *passkeyFixture) session(t *testing.T, userID string) string {
	t.Helper()
	sess, _, err := f.mgr.MintSession(context.Background(), userID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return sess.Token
}

func (f *passkeyFixture) do(t *testing.T, method, path, cookie string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rd *strings.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = strings.NewReader(string(b))
	} else {
		rd = strings.NewReader("")
	}
	req := httptest.NewRequest(method, "https://example.com/auth"+path, rd)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	}
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	return w
}

// challenge reads publicKey.challenge from an options response.
func challengeOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("options: %d %s", w.Code, w.Body)
	}
	var out struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	ch, err := b64url.DecodeString(out.PublicKey.Challenge)
	if err != nil {
		t.Fatal(err)
	}
	return string(ch)
}

func registerBody(a *fakeAuthenticator, challenge string) map[string]string {
	cdj, att := a.register(challenge)
	return map[string]string{
		"name": "Laptop", "id": b64url.EncodeToString(a.credID),
		"clientDataJSON": b64url.EncodeToString(cdj), "attestationObject": b64url.EncodeToString(att),
	}
}

func loginBody(a *fakeAuthenticator, challenge, userID string) map[string]string {
	cdj, ad, sig := a.assert(challenge)
	return map[string]string{
		"id": b64url.EncodeToString(a.credID), "clientDataJSON": b64url.EncodeToString(cdj),
		"authenticatorData": b64url.EncodeToString(ad), "signature": b64url.EncodeToString(sig),
		"userHandle": b64url.EncodeToString([]byte(userID)),
	}
}

// enrol registers a passkey for userID through the endpoints.
func (f *passkeyFixture) enrol(t *testing.T, userID string) *fakeAuthenticator {
	t.Helper()
	cookie := f.session(t, userID)
	a := newFakeAuthenticator(t, coseAlgES256)
	a.flags &^= flagBE | flagBS // a device-bound key with a counter
	ch := challengeOf(t, f.do(t, "POST", "/passkey/register/options", cookie, nil))
	if w := f.do(t, "POST", "/passkey/register", cookie, registerBody(a, ch)); w.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", w.Code, w.Body)
	}
	return a
}

func (f *passkeyFixture) login(t *testing.T, a *fakeAuthenticator, userID string) *httptest.ResponseRecorder {
	t.Helper()
	ch := challengeOf(t, f.do(t, "POST", "/passkey/login/options", "", nil))
	return f.do(t, "POST", "/passkey/login", "", loginBody(a, ch, userID))
}

func passkeySessionCookie(w *httptest.ResponseRecorder) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == "session_id" && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

func TestPasskeyRegisterThenSignIn(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")

	w := f.login(t, a, "u-alice")
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"redirect":"/app"`) || w.Header().Get("Cache-Control") == "" {
		t.Fatalf("login response: %s %v", w.Body, w.Header())
	}
	sess, err := f.mgr.SessionStore().Get(context.Background(), passkeySessionCookie(w))
	if err != nil || sess.UserID != "u-alice" || sess.PendingTwoFactor {
		t.Fatalf("session: %+v %v", sess, err)
	}
	creds, _ := f.pk.store.ListPasskeys(context.Background(), "u-alice")
	if len(creds) != 1 || creds[0].SignCount != a.count || creds[0].LastUsedAt.IsZero() || creds[0].Name != "Laptop" {
		t.Fatalf("stored: %+v", creds)
	}
}

func TestPasskeyChallengeIsSingleUse(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	ch := challengeOf(t, f.do(t, "POST", "/passkey/login/options", "", nil))
	body := loginBody(a, ch, "u-alice")
	if w := f.do(t, "POST", "/passkey/login", "", body); w.Code != http.StatusOK {
		t.Fatalf("first: %d %s", w.Code, w.Body)
	}
	a.count++ // a fresh signature over the same challenge still fails
	body2 := loginBody(a, ch, "u-alice")
	if w := f.do(t, "POST", "/passkey/login", "", body2); w.Code != http.StatusUnauthorized {
		t.Fatalf("replayed challenge: %d %s", w.Code, w.Body)
	}
}

func TestPasskeyRegisterNeedsSession(t *testing.T) {
	f := newPasskeyFixture(t)
	for _, path := range []string{"/passkey/register/options", "/passkey/register", "/passkey/delete"} {
		if w := f.do(t, "POST", path, "", map[string]string{}); w.Code != http.StatusUnauthorized {
			t.Errorf("%s without a session: %d", path, w.Code)
		}
	}
	if w := f.do(t, "GET", "/passkey/list", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("list without a session: %d", w.Code)
	}
}

// A registration challenge is bound to the account that asked for it.
func TestPasskeyRegisterOthersChallenge(t *testing.T) {
	f := newPasskeyFixture(t)
	bob := f.session(t, "u-bob")
	ch := challengeOf(t, f.do(t, "POST", "/passkey/register/options", bob, nil))
	alice := f.session(t, "u-alice")
	a := newFakeAuthenticator(t, coseAlgES256)
	if w := f.do(t, "POST", "/passkey/register", alice, registerBody(a, ch)); w.Code != http.StatusBadRequest {
		t.Fatalf("alice spent bob's challenge: %d %s", w.Code, w.Body)
	}
	if creds, _ := f.pk.store.ListPasskeys(context.Background(), "u-alice"); len(creds) != 0 {
		t.Fatalf("stored %+v", creds)
	}
}

// A sign-in challenge cannot complete a registration, and the reverse.
func TestPasskeyChallengePurposes(t *testing.T) {
	f := newPasskeyFixture(t)
	alice := f.session(t, "u-alice")
	loginCh := challengeOf(t, f.do(t, "POST", "/passkey/login/options", "", nil))
	a := newFakeAuthenticator(t, coseAlgES256)
	if w := f.do(t, "POST", "/passkey/register", alice, registerBody(a, loginCh)); w.Code != http.StatusBadRequest {
		t.Fatalf("register with a sign-in challenge: %d", w.Code)
	}
	b := f.enrol(t, "u-alice")
	regCh := challengeOf(t, f.do(t, "POST", "/passkey/register/options", alice, nil))
	if w := f.do(t, "POST", "/passkey/login", "", loginBody(b, regCh, "u-alice")); w.Code != http.StatusUnauthorized {
		t.Fatalf("sign in with a registration challenge: %d", w.Code)
	}
}

func TestPasskeyUserHandleMustMatch(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	if w := f.login(t, a, "u-bob"); w.Code != http.StatusUnauthorized || passkeySessionCookie(w) != "" {
		t.Fatalf("alice's passkey claimed bob: %d", w.Code)
	}
}

func TestPasskeySignCountMustAdvance(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	if w := f.login(t, a, "u-alice"); w.Code != http.StatusOK {
		t.Fatalf("login: %d", w.Code)
	}
	a.count-- // assert() adds one back: the same count again
	if w := f.login(t, a, "u-alice"); w.Code != http.StatusUnauthorized {
		t.Fatalf("repeated counter accepted: %d", w.Code)
	}
	var cloned bool
	for _, ev := range f.sink.snapshot() {
		cloned = cloned || ev.Kind == "passkey.clone_suspected"
	}
	if !cloned {
		t.Fatal("no passkey.clone_suspected event")
	}
}

// Synced passkeys report a zero counter on every use; that is no signal.
func TestPasskeyZeroCounterSignsIn(t *testing.T) {
	f := newPasskeyFixture(t)
	alice := f.session(t, "u-alice")
	a := newFakeAuthenticator(t, coseAlgEdDSA)
	ch := challengeOf(t, f.do(t, "POST", "/passkey/register/options", alice, nil))
	if w := f.do(t, "POST", "/passkey/register", alice, registerBody(a, ch)); w.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", w.Code, w.Body)
	}
	for range 2 {
		a.count = 0
		ch := challengeOf(t, f.do(t, "POST", "/passkey/login/options", "", nil))
		cdj, ad, sig := a.clientData("webauthn.get", ch), a.authData(a.flags, nil), []byte(nil)
		sig = a.sign(ad, cdj)
		body := map[string]string{
			"id": b64url.EncodeToString(a.credID), "clientDataJSON": b64url.EncodeToString(cdj),
			"authenticatorData": b64url.EncodeToString(ad), "signature": b64url.EncodeToString(sig),
			"userHandle": b64url.EncodeToString([]byte("u-alice")),
		}
		if w := f.do(t, "POST", "/passkey/login", "", body); w.Code != http.StatusOK {
			t.Fatalf("zero-counter login: %d %s", w.Code, w.Body)
		}
	}
}

func enableTwoFA(t *testing.T, f *passkeyFixture, userID string) {
	t.Helper()
	if err := f.twofa.store.SetTwoFA(context.Background(), userID, &TwoFAState{Enabled: true, Verified: true, Secret: GenerateSecret()}); err != nil {
		t.Fatal(err)
	}
}

// A password-only session of a 2FA account cannot add a way in.
func TestPasskeyRegisterNeedsSecondFactor(t *testing.T) {
	f := newPasskeyFixture(t)
	early := f.session(t, "u-alice") // minted before enrolment: never pending
	enableTwoFA(t, f, "u-alice")
	pending := f.session(t, "u-alice")
	for name, cookie := range map[string]string{"pending": pending, "pre-enrolment": early} {
		if w := f.do(t, "POST", "/passkey/register/options", cookie, nil); w.Code != http.StatusForbidden {
			t.Errorf("%s session: %d %s", name, w.Code, w.Body)
		}
	}
}

func TestPasskeyLoginKeepsSecondFactor(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	enableTwoFA(t, f, "u-alice")
	w := f.login(t, a, "u-alice")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"two_factor_required":true`) ||
		!strings.Contains(w.Body.String(), `"redirect":"/2fa"`) {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	sess, err := f.mgr.SessionStore().Get(context.Background(), passkeySessionCookie(w))
	if err != nil || !sess.PendingTwoFactor {
		t.Fatalf("session not pending: %+v %v", sess, err)
	}
}

func TestPasskeyDeleteOnlyOwn(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	id := map[string]string{"id": b64url.EncodeToString(a.credID)}
	if w := f.do(t, "POST", "/passkey/delete", f.session(t, "u-bob"), id); w.Code != http.StatusNotFound {
		t.Fatalf("bob deleted alice's passkey: %d", w.Code)
	}
	alice := f.session(t, "u-alice")
	if w := f.do(t, "GET", "/passkey/list", alice, nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), id["id"]) {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	if w := f.do(t, "POST", "/passkey/delete", alice, id); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if w := f.login(t, a, "u-alice"); w.Code != http.StatusUnauthorized {
		t.Fatalf("deleted passkey signed in: %d", w.Code)
	}
}

// Claiming an unverified account evicts the registrant's passkeys.
func TestPasskeyClaimRemovesPasskeys(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	if err := f.mgr.claimAccount(context.Background(), "u-alice", "magic_link", "", true); err != nil {
		t.Fatal(err)
	}
	if w := f.login(t, a, "u-alice"); w.Code != http.StatusUnauthorized {
		t.Fatalf("squatter's passkey survived the claim: %d", w.Code)
	}
}

func TestPasskeyBodyIsStrict(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	ch := challengeOf(t, f.do(t, "POST", "/passkey/login/options", "", nil))
	body := loginBody(a, ch, "u-alice")
	body["extra"] = "x"
	if w := f.do(t, "POST", "/passkey/login", "", body); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown member: %d", w.Code)
	}
	body = loginBody(a, ch, "u-alice")
	body["signature"] += "=="
	if w := f.do(t, "POST", "/passkey/login", "", body); w.Code != http.StatusBadRequest {
		t.Fatalf("padded base64: %d", w.Code)
	}
}

// durableSessions stands in for a durable session store, so the
// production-mode Init reaches the passkey plugin's own check.
type durableSessions struct{ *MemorySessionStore }

func TestPasskeyProdRefusesMemoryStores(t *testing.T) {
	mgr := New(AuthConfig{JWTSecret: "s", SessionTTL: time.Hour, SessionCookie: "sid", UserStore: newMemoryUserStore(), SessionStore: durableSessions{NewMemorySessionStore()}})
	mgr.Use(NewPasskeyPlugin(PasskeyConfig{RPID: "example.com", Origins: []string{"https://example.com"}}))
	err := mgr.Init(nil)
	if err == nil || !strings.Contains(err.Error(), "in-memory stores") {
		t.Fatalf("Init: %v", err)
	}
}

func TestPasskeyBadOriginFailsInit(t *testing.T) {
	mgr := New(AuthConfig{JWTSecret: "s", AllowInMemoryStores: true, SessionTTL: time.Hour, SessionCookie: "sid", UserStore: newMemoryUserStore()})
	mgr.Use(NewPasskeyPlugin(PasskeyConfig{RPID: "example.com", Origins: []string{"https://evil.example"}}))
	if err := mgr.Init(nil); err == nil {
		t.Fatal("Init accepted an origin outside the RP ID")
	}
}

func TestPasskeyRegisterIDMustMatch(t *testing.T) {
	f := newPasskeyFixture(t)
	alice := f.session(t, "u-alice")
	a := newFakeAuthenticator(t, coseAlgES256)
	ch := challengeOf(t, f.do(t, "POST", "/passkey/register/options", alice, nil))
	body := registerBody(a, ch)
	body["id"] = b64url.EncodeToString([]byte("some-other-credential"))
	if w := f.do(t, "POST", "/passkey/register", alice, body); w.Code != http.StatusBadRequest {
		t.Fatalf("mismatched id: %d %s", w.Code, w.Body)
	}
}

func TestPasskeyPerUserCap(t *testing.T) {
	f := newPasskeyFixture(t)
	f.pk.config.MaxPerUser = 2
	f.enrol(t, "u-alice")
	f.enrol(t, "u-alice")
	if w := f.do(t, "POST", "/passkey/register/options", f.session(t, "u-alice"), nil); w.Code != http.StatusConflict {
		t.Fatalf("third passkey options: %d", w.Code)
	}
}

// A second factor needs somewhere to finish; without TwoFactorURL a
// pending passkey sign-in would land on a page that refuses it.
func TestPasskeyTwoFactorNeedsURL(t *testing.T) {
	mgr := New(AuthConfig{JWTSecret: "s", AllowInMemoryStores: true, SessionTTL: time.Hour, SessionCookie: "sid", UserStore: newMemoryUserStore()})
	mgr.Use(NewTwoFAPlugin(TwoFAConfig{}))
	mgr.Use(NewPasskeyPlugin(PasskeyConfig{RPID: "example.com", Origins: []string{"https://example.com"}}))
	if err := mgr.Init(nil); err == nil || !strings.Contains(err.Error(), "TwoFactorURL") {
		t.Fatalf("Init: %v", err)
	}
}

// A reset locks out whoever held the account, passkeys included.
func TestPasskeyResetRemovesPasskeys(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	b := f.enrol(t, "u-bob")
	if w := f.do(t, "POST", "/forgot-password", "", map[string]string{"email": "alice@example.com"}); w.Code != http.StatusOK {
		t.Fatalf("forgot-password: %d %s", w.Code, w.Body)
	}
	_, mail := f.mail.snapshot()
	tok := extractTokenFromBody(mail)
	if w := f.do(t, "POST", "/reset-password", "", map[string]string{"token": tok, "password": "brandnewpw1"}); w.Code != http.StatusOK {
		t.Fatalf("reset-password: %d %s", w.Code, w.Body)
	}
	if w := f.login(t, a, "u-alice"); w.Code != http.StatusUnauthorized {
		t.Fatalf("a passkey survived the reset: %d", w.Code)
	}

	if w := f.login(t, b, "u-bob"); w.Code != http.StatusOK {
		t.Fatalf("another account's passkey went too: %d", w.Code)
	}
}

// Challenges minted while under the cap must not register past it.
func TestPasskeyCapHoldsAtRegister(t *testing.T) {
	f := newPasskeyFixture(t)
	f.pk.config.MaxPerUser = 2
	f.enrol(t, "u-alice")
	alice := f.session(t, "u-alice")
	ch1 := challengeOf(t, f.do(t, "POST", "/passkey/register/options", alice, nil))
	ch2 := challengeOf(t, f.do(t, "POST", "/passkey/register/options", alice, nil))
	if w := f.do(t, "POST", "/passkey/register", alice, registerBody(newFakeAuthenticator(t, coseAlgES256), ch1)); w.Code != http.StatusCreated {
		t.Fatalf("second passkey: %d %s", w.Code, w.Body)
	}
	if w := f.do(t, "POST", "/passkey/register", alice, registerBody(newFakeAuthenticator(t, coseAlgES256), ch2)); w.Code != http.StatusConflict {
		t.Fatalf("third passkey: %d %s", w.Code, w.Body)
	}
	if creds, _ := f.pk.store.ListPasskeys(context.Background(), "u-alice"); len(creds) != 2 {
		t.Fatalf("stored %d passkeys, want 2", len(creds))
	}
}

// Backup eligibility is fixed for a credential's life; a flip is refused.
func TestPasskeyBackupEligibleIsFixed(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice") // device-bound: BE clear
	a.flags |= flagBE
	if w := f.login(t, a, "u-alice"); w.Code != http.StatusUnauthorized {
		t.Fatalf("BE flipped on: %d %s", w.Code, w.Body)
	}
}

// The unauthenticated routes mint challenges and sessions, so each
// route's throttle is pinned, not just one.
func TestPasskeyRoutesAreRateLimited(t *testing.T) {
	for _, rt := range []struct{ method, path string }{
		{"POST", "/passkey/login/options"}, {"POST", "/passkey/login"},
		{"POST", "/passkey/register/options"}, {"POST", "/passkey/register"},
		{"GET", "/passkey/list"}, {"POST", "/passkey/delete"},
	} {
		f := newPasskeyFixture(t)
		f.pk.limit = newScopedRateLimiter(RateLimiterConfig{MaxAttempts: 2, Window: time.Minute, BlockDuration: time.Minute}, "passkey")
		alice := f.session(t, "u-alice")
		var last int
		for range 3 {
			last = f.do(t, rt.method, rt.path, alice, nil).Code
		}
		if last != http.StatusTooManyRequests {
			t.Errorf("%s %s third call: %d, want 429", rt.method, rt.path, last)
		}
	}
}

// Every passkey POST speaks JSON, so a cross-site simple request
// cannot reach one without a preflight.
func TestPasskeyOptionsNeedJSON(t *testing.T) {
	f := newPasskeyFixture(t)
	alice := f.session(t, "u-alice")
	for _, path := range []string{"/passkey/login/options", "/passkey/register/options"} {
		req := httptest.NewRequest("POST", "https://example.com/auth"+path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "text/plain")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.AddCookie(&http.Cookie{Name: "session_id", Value: alice})
		w := httptest.NewRecorder()
		f.r.ServeHTTP(w, req)
		if w.Code != http.StatusUnsupportedMediaType {
			t.Errorf("%s as text/plain: %d", path, w.Code)
		}
	}
}

// Browsers report https://example.com:443 as https://example.com, so an
// origin spelled with its default port would refuse every ceremony.
func TestPasskeyDefaultPortFailsInit(t *testing.T) {
	for id, o := range map[string]string{"example.com": "https://example.com:443", "localhost": "http://localhost:80"} {
		if err := (webauthnRP{ID: id, Origins: []string{o}}).validate(); err == nil {
			t.Errorf("validate accepted %s", o)
		}
	}
}

type brokenTwoFAStore struct{ TwoFAStore }

func (brokenTwoFAStore) GetTwoFA(context.Context, string) (*TwoFAState, error) {
	return nil, errors.New("2fa store down")
}

// When the 2FA state cannot be read, a passkey sign-in fails closed and
// the audit trail says why, as a password login does.
func TestPasskeyTwoFADownFailsClosed(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	f.twofa.store = brokenTwoFAStore{f.twofa.store}
	w := f.login(t, a, "u-alice")
	if w.Code != http.StatusInternalServerError || passkeySessionCookie(w) != "" {
		t.Fatalf("login with 2FA down: %d %s", w.Code, w.Body)
	}
	for _, ev := range f.sink.snapshot() {
		if ev.Kind == "login.failed" && ev.Meta["reason"] == "twofa_failclosed" && ev.Meta["via"] == "passkey" {
			return
		}
	}
	t.Fatalf("no login.failed twofa_failclosed event: %+v", f.sink.snapshot())
}

// Agent-authored IR must not arm the passkey module: a disguised
// delete button would remove a passkey with one click.
func TestPasskeyMarkersLeaveUntrustedIR(t *testing.T) {
	got := string(noderender.RenderKind("button", map[string]any{
		"data-passkey-delete": "/auth", "data-passkey-login": "/auth", "data-passkey-register": "/auth", "text": "View invoice",
	}, nil))
	if strings.Contains(got, "data-passkey-") {
		t.Fatalf("untrusted IR kept a passkey marker: %s", got)
	}
}

func TestPasskeyNameRefusesBadInput(t *testing.T) {
	for in, want := range map[string]bool{
		"Laptop": true, "  ": true, strings.Repeat("é", 64): true,
		strings.Repeat("a", 65): false, "bad\xff": false, "bell\a": false, "rtl\u202eexe": false,
	} {
		if _, ok := passkeyName(in); ok != want {
			t.Errorf("passkeyName(%q) ok = %v, want %v", in, ok, want)
		}
	}
	if got, _ := passkeyName("  "); got != "Passkey" {
		t.Errorf("blank name = %q, want Passkey", got)
	}
}

// A user id WebAuthn cannot carry as user.id is refused before options.
func TestPasskeyLongUserIDRefused(t *testing.T) {
	f := newPasskeyFixture(t)
	long := strings.Repeat("u", maxUserHandleLen+1)
	e := &storeEntry{user: &BasicUser{ID: long, Email: "long@example.com"}, hash: passwordPlaceholderHash, emailVerified: true}
	f.users.users["long@example.com"], f.users.byID[long] = e, e
	if w := f.do(t, "POST", "/passkey/register/options", f.session(t, long), nil); w.Code != http.StatusInternalServerError {
		t.Fatalf("long user id: %d %s, want 500", w.Code, w.Body)
	}
}

type failingFindStore struct{ UserStore }

func (failingFindStore) FindByID(context.Context, string) (User, error) {
	return nil, errors.New("db down")
}

// A store outage is a server error, not a signed-out user.
func TestPasskeyUserLookupFailIs500(t *testing.T) {
	f := newPasskeyFixture(t)
	alice := f.session(t, "u-alice")
	f.mgr.userStore = failingFindStore{f.mgr.userStore}
	if w := f.do(t, "POST", "/passkey/register/options", alice, nil); w.Code != http.StatusInternalServerError {
		t.Fatalf("lookup outage: %d %s, want 500", w.Code, w.Body)
	}
}

type lostRaceStore struct{ PasskeyStore }

func (lostRaceStore) RecordPasskeyUse(context.Context, []byte, uint32, uint32, bool, time.Time) (bool, error) {
	return false, nil
}

// Two sign-ins racing on one counter: the loser gets no session.
func TestPasskeyLostRaceIs409(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	f.pk.store = lostRaceStore{f.pk.store}
	w := f.login(t, a, "u-alice")
	if w.Code != http.StatusConflict || passkeySessionCookie(w) != "" {
		t.Fatalf("lost race: %d %s, want 409 and no session", w.Code, w.Body)
	}
}

// The login route is anonymous, so a refusal's audit reason is a fixed
// code; request text never reaches SecurityEvent.Meta.
func TestPasskeyRefusalReasonIsACode(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	ch := challengeOf(t, f.do(t, "POST", "/passkey/login/options", "", nil))
	body := loginBody(a, ch, "u-alice")
	body["clientDataJSON"] = b64url.EncodeToString([]byte(`{"type":"INJECTED reason","challenge":"eA","origin":"https://example.com"}`))
	if w := f.do(t, "POST", "/passkey/login", "", body); w.Code != http.StatusUnauthorized {
		t.Fatalf("forged client data: %d %s", w.Code, w.Body)
	}
	codes := map[string]bool{"client_data": true, "challenge": true, "attestation": true, "credential_mismatch": true,
		"unknown_credential": true, "user_handle": true, "signature": true, "backup_eligibility": true, "account_gone": true}
	seen := false
	for _, ev := range f.sink.snapshot() {
		if ev.Kind != "passkey.refused" {
			continue
		}
		seen = true
		if !codes[ev.Meta["reason"]] {
			t.Errorf("reason %q is not a fixed code", ev.Meta["reason"])
		}
	}
	if !seen {
		t.Fatal("no passkey.refused event")
	}
}

type failingPurgeStore struct{ PasskeyStore }

func (failingPurgeStore) DeletePasskeysForUser(context.Context, string) (int, error) {
	return 0, errors.New("db down")
}

// A reset that could not remove the passkeys must not report success:
// the owner would believe a stolen session's passkey is gone.
func TestPasskeyResetFailsIfPurgeFails(t *testing.T) {
	f := newPasskeyFixture(t)
	f.enrol(t, "u-alice")
	f.pk.store = failingPurgeStore{f.pk.store}
	if w := f.do(t, "POST", "/forgot-password", "", map[string]string{"email": "alice@example.com"}); w.Code != http.StatusOK {
		t.Fatalf("forgot-password: %d %s", w.Code, w.Body)
	}
	_, mail := f.mail.snapshot()
	w := f.do(t, "POST", "/reset-password", "", map[string]string{"token": extractTokenFromBody(mail), "password": "brandnewpw1"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("reset with a failed purge: %d %s, want 500", w.Code, w.Body)
	}
}
