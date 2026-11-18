//go:build red

package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
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
func TestRedReauthLookupErrorFailsClosed(t *testing.T) {
	f := newPasskeyFixture(t)
	old := f.enrol(t, "u-alice")
	f.age(t, old) // so the options call has an eligible passkey to list
	alice := f.session(t, "u-alice")
	fresh := newFakeAuthenticator(t, coseAlgES256)
	fresh.flags &^= flagBE | flagBS
	ch := challengeOf(t, f.do(t, "POST", "/passkey/register/options", alice, nil))
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
func TestRedSQLPasskeyKeepsPredateOrder(t *testing.T) {
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
func TestRedPasskeyBaseRejectsRootSlash(t *testing.T) {
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
