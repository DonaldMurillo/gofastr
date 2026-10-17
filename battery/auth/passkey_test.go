package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

type passkeyFixture struct {
	mgr   *AuthManager
	users *memoryUserStore
	pk    *PasskeyPlugin
	twofa *TwoFAPlugin
	sink  *recordingSink
	r     *router.Router
}

func newPasskeyFixture(t *testing.T) *passkeyFixture {
	t.Helper()
	return newPasskeyFixtureFor(t, "example.com", "https://example.com")
}

// newPasskeyFixtureFor builds the fixture for one relying party.
func newPasskeyFixtureFor(t *testing.T, rpID, origin string) *passkeyFixture {
	t.Helper()
	users := newMemoryUserStore()
	sink := &recordingSink{}
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		AllowInMemoryStores: true,
		SessionTTL:          time.Hour,
		SessionCookie:       "session_id",
		UserStore:           users,
		AuditSink:           sink,
	})
	pk := NewPasskeyPlugin(PasskeyConfig{RPID: rpID, Origins: []string{origin}, OnSuccessURL: "/app"})
	twofa := NewTwoFAPlugin(TwoFAConfig{})
	mgr.Use(NewCorePlugin())
	mgr.Use(twofa)
	mgr.Use(pk)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for _, u := range []*BasicUser{
		{ID: "u-alice", Email: "alice@example.com"},
		{ID: "u-bob", Email: "bob@example.com"},
	} {
		e := &storeEntry{user: u, hash: passwordPlaceholderHash}
		users.users[u.Email], users.byID[u.ID] = e, e
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	return &passkeyFixture{mgr: mgr, users: users, pk: pk, twofa: twofa, sink: sink, r: r}
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
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"two_factor_required":true`) {
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
