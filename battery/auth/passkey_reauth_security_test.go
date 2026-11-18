package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// flakyPasskeyStore fails its first armed GetPasskey with a transient
// error and answers every later one from the wrapped store.
type flakyPasskeyStore struct {
	PasskeyStore
	armed atomic.Bool
}

func (s *flakyPasskeyStore) GetPasskey(ctx context.Context, id []byte) (PasskeyCredential, error) {
	if s.armed.CompareAndSwap(true, false) {
		return PasskeyCredential{}, errors.New("transient: connection reset")
	}
	return s.PasskeyStore.GetPasskey(ctx, id)
}

// A step-up's eligibility check must fail closed: a lookup error on the
// first read must not let checkAssertion's second read pass a passkey
// registered in this very session.
func TestReauthLookupErrorFailsClosed(t *testing.T) {
	f := newPasskeyFixture(t)
	old := f.enrol(t, "u-alice")
	f.age(t, old) // so the options call has an eligible passkey to list
	alice := f.session(t, "u-alice")
	fresh := newFakeAuthenticator(t, coseAlgES256)
	fresh.flags &^= flagBE | flagBS
	ch := challengeOf(t, f.do(t, "POST", "/passkey/register/options", alice, map[string]string{"proof": f.proof(t, alice, old, "u-alice")}))
	if w := f.do(t, "POST", "/passkey/register", alice, registerBody(fresh, ch)); w.Code != http.StatusCreated {
		t.Fatalf("register: %d", w.Code)
	}
	flaky := &flakyPasskeyStore{PasskeyStore: f.pk.store}
	f.pk.store = flaky
	rch := challengeOf(t, f.do(t, "POST", "/passkey/reauth/options", alice, nil))
	flaky.armed.Store(true)
	w := f.do(t, "POST", "/passkey/reauth", alice, loginBody(fresh, rch, "u-alice"))
	if w.Code == http.StatusOK {
		t.Fatalf("a passkey registered in this session confirmed a step-up after one lookup error: %s", w.Body)
	}
}

// The predate-the-session rule must survive the SQL store: a passkey
// registered after the session signed in, in the same second, must not
// read back as older than the session.
func TestSQLPasskeyKeepsPredateOrder(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	store := sqlPasskeyStore(t, db)
	second := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Hour)
	sess := &Session{UserID: "u-alice", CreatedAt: second.Add(400 * time.Millisecond)}
	a := newFakeAuthenticator(t, coseAlgES256)
	c := PasskeyCredential{ID: a.credID, UserID: "u-alice", PublicKey: []byte{1}, Algorithm: coseAlgES256,
		CreatedAt: second.Add(900 * time.Millisecond)} // after the session
	if err := store.AddPasskey(context.Background(), c, 20); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetPasskey(context.Background(), a.credID)
	if err != nil {
		t.Fatal(err)
	}
	p := &PasskeyPlugin{config: PasskeyConfig{ReauthMinAge: time.Hour}}
	if p.reauthEligible(got, sess, time.Now()) {
		t.Fatalf("passkey made at %v, after a session of %v, reads back as %v and predates it",
			c.CreatedAt, sess.CreatedAt, got.CreatedAt)
	}
}

// An auth base must be a same-origin path: "/" makes base+"/passkey/..."
// a protocol-relative URL to the host "passkey".
func TestPasskeyBaseStaysSameOrigin(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	m := regexp.MustCompile(`const BASE = (/.*/);`).FindStringSubmatch(passkeyJS)
	if m == nil {
		t.Fatal("BASE regex not found in passkey.js")
	}
	script := `const BASE = ` + m[1] + `;
for (const b of ["/", "/auth/", "/a//b"]) {
  if (BASE.test(b) && new URL(b + "/passkey/login", "https://app.example/").host !== "app.example") {
    console.log("CROSS " + JSON.stringify(b)); }
}`
	out, err := exec.Command(node, "-e", script).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v %s", err, out)
	}
	if s := strings.TrimSpace(string(out)); s != "" {
		t.Fatalf("an accepted base aims a ceremony at another origin: %s", s)
	}
}

// Until the signature verifies, a sign-in names no account: anyone
// holding a credential id must not write refused events under its owner.
func TestAnonymousRefusalNamesNoAccount(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	forger := newFakeAuthenticator(t, coseAlgES256)
	forger.credID = a.credID // the id is known; the key is not
	forger.flags = a.flags
	if w := f.login(t, forger, "u-alice"); w.Code != http.StatusUnauthorized {
		t.Fatalf("forged signature: %d", w.Code)
	}
	if w := f.login(t, a, "u-bob"); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong user handle: %d", w.Code)
	}
	for _, ev := range f.sink.findByKindAll("passkey.refused") {
		if ev.UserID != "" {
			t.Fatalf("an unverified sign-in wrote a refused event for %q (reason %s)", ev.UserID, ev.Meta["reason"])
		}
	}
}

// A flag body with a duplicated or case-folded key reads as no flag.
func TestOptionsFlagDecodeIsStrict(t *testing.T) {
	f := newPasskeyFixture(t)
	cookie := f.session(t, "u-alice")
	for _, raw := range []string{`{"conditional":false,"conditional":true}`, `{"Conditional":true}`} {
		req := httptest.NewRequest("POST", "https://example.com/auth/passkey/register/options", strings.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
		w := httptest.NewRecorder()
		f.r.ServeHTTP(w, req)
		if !strings.Contains(w.Body.String(), `"userVerification":"required"`) {
			t.Errorf("%s asked for a conditional create: %s", raw, w.Body)
		}
	}
}

// A step-up proof belongs to the session that earned it: another session
// of the same account cannot spend it.
func TestReauthProofIsTheSessions(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	f.age(t, a)
	first, second := f.session(t, "u-alice"), f.session(t, "u-alice")
	w := f.reauth(t, first, a, "u-alice")
	var out struct {
		Proof string `json:"proof"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.Proof == "" {
		t.Fatalf("reauth: %d %s", w.Code, w.Body)
	}
	if err := f.redeem(second, out.Proof); !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("another session of the account spent the proof: %v", err)
	}
	w = f.reauth(t, first, a, "u-alice")
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if err := f.redeem(first, out.Proof); err != nil {
		t.Fatalf("the earning session: %v", err)
	}
}

// Once the account holds a passkey that can confirm a step-up, adding or
// removing a passkey needs a proof this session earned: a stolen session
// can neither add a key of its own nor remove the owner's.
func TestPasskeyChangesNeedStepUp(t *testing.T) {
	f := newPasskeyFixture(t)
	old := f.enrol(t, "u-alice")
	f.age(t, old)
	alice, other := f.session(t, "u-alice"), f.session(t, "u-alice")
	needs := func(w *httptest.ResponseRecorder, what string) {
		t.Helper()
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `"reason":"passkey_reauth_required"`) {
			t.Fatalf("%s: %d %s, want 403 passkey_reauth_required", what, w.Code, w.Body)
		}
	}
	needs(f.do(t, "POST", "/passkey/register/options", alice, nil), "add without a proof")
	needs(f.do(t, "POST", "/passkey/register/options", alice, map[string]bool{"conditional": true}), "silent add without a proof")
	needs(f.do(t, "POST", "/passkey/register/options", alice, map[string]string{"proof": f.proof(t, other, old, "u-alice")}), "add with another session's proof")
	p := f.proof(t, alice, old, "u-alice")
	if w := f.do(t, "POST", "/passkey/register/options", alice, map[string]string{"proof": p}); w.Code != http.StatusOK {
		t.Fatalf("add with a proof: %d %s", w.Code, w.Body)
	}
	needs(f.do(t, "POST", "/passkey/register/options", alice, map[string]string{"proof": p}), "add with a spent proof")

	id := b64url.EncodeToString(old.credID)
	needs(f.do(t, "POST", "/passkey/delete", alice, map[string]string{"id": id}), "remove without a proof")
	if w := f.do(t, "POST", "/passkey/delete", alice, map[string]string{"id": id, "proof": f.proof(t, alice, old, "u-alice")}); w.Code != http.StatusNoContent {
		t.Fatalf("remove with a proof: %d %s", w.Code, w.Body)
	}
	// With no passkey left that could confirm, the session suffices.
	if w := f.do(t, "POST", "/passkey/register/options", alice, nil); w.Code != http.StatusOK {
		t.Fatalf("add on an account with no passkey: %d", w.Code)
	}
}

// Adding a passkey emails the account, so one someone else added does not
// go unnoticed; outside DevMode the plugin will not run without a way to.
func TestPasskeyAddedNotifiesTheOwner(t *testing.T) {
	f := newPasskeyFixture(t)
	f.enrol(t, "u-alice")
	to, body := f.notices.snapshot()
	if to != "alice@example.com" || !strings.Contains(body, "A new passkey") || !strings.Contains(body, "remove it") {
		t.Fatalf("added-passkey notice to %q: %q", to, body)
	}
	boot := func(cfg AuthConfig, pk PasskeyConfig) error {
		cfg.JWTSecret, cfg.UserStore, cfg.AllowInMemoryStores = "s", newMemoryUserStore(), true
		mgr := New(cfg)
		pk.RPID, pk.Origins = "example.com", []string{"https://example.com"}
		mgr.Use(NewPasskeyPlugin(pk))
		return mgr.Init(nil)
	}
	if err := boot(AuthConfig{}, PasskeyConfig{}); err == nil || !strings.Contains(err.Error(), "NoticeSender") {
		t.Fatalf("no sender outside DevMode: %v", err)
	}
	// The duplicate-register sender may send its own fixed template
	// whatever the body; it is not taken as the passkey notice sender.
	if err := boot(AuthConfig{RegisterEmailSender: &stubEmailSender{}}, PasskeyConfig{}); err == nil {
		t.Fatal("RegisterEmailSender stood in for NoticeSender")
	}
	for name, ok := range map[string]error{
		"NoticeSender":  boot(AuthConfig{}, PasskeyConfig{NoticeSender: &stubEmailSender{}}),
		"NoAddedNotice": boot(AuthConfig{}, PasskeyConfig{NoAddedNotice: true}),
		"DevMode":       boot(AuthConfig{DevMode: true}, PasskeyConfig{}),
	} {
		if ok != nil {
			t.Errorf("%s: %v", name, ok)
		}
	}
}
