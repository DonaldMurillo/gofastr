package auth

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// shiftSession moves a stored session's sign-in time by d.
func (f *passkeyFixture) shiftSession(t *testing.T, token string, d time.Duration, zero bool) {
	t.Helper()
	ms, ok := f.mgr.SessionStore().(*MemorySessionStore)
	if !ok {
		t.Fatal("not a memory session store")
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()
	s := ms.sessions[token]
	if s == nil {
		t.Fatal("no such session")
	}
	if zero {
		s.CreatedAt = time.Time{}
	} else {
		s.CreatedAt = s.CreatedAt.Add(d)
	}
}

// The session that enrolled the account's passkey (the conditional
// create after a password sign-in) is older than that passkey, so the
// predate rule never lets it step up and steppedUp never gates it: a
// thief holding it removes the owner's passkey with no proof, for as
// long as the session lives.
func TestEnrollingSessionIsGated(t *testing.T) {
	f := newPasskeyFixture(t)
	s := f.session(t, "u-alice")
	a := newFakeAuthenticator(t, coseAlgES256)
	a.flags &^= flagBE | flagBS
	ch := challengeOf(t, f.do(t, "POST", "/passkey/register/options", s, nil))
	if w := f.do(t, "POST", "/passkey/register", s, registerBody(a, ch)); w.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", w.Code, w.Body)
	}
	// Three hours on: the passkey is old enough to confirm a step-up for
	// any later session, and the enrolling session is still alive.
	f.age(t, a)
	f.age(t, a)
	f.shiftSession(t, s, -5*time.Hour, false)
	id := b64url.EncodeToString(a.credID)
	w := f.do(t, "POST", "/passkey/delete", s, map[string]string{"id": id})
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `"reason":"passkey_signin_required"`) {
		t.Fatalf("the enrolling session removing the account's passkey without a proof: %d %s", w.Code, w.Body)
	}
}

// A SessionStore that does not record CreatedAt makes every passkey
// ineligible, which steppedUp reads as "nothing to confirm with": the
// gate fails open instead of closed.
func TestZeroSessionTimeIsGated(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	f.age(t, a)
	s := f.session(t, "u-alice")
	f.shiftSession(t, s, 0, true)
	id := b64url.EncodeToString(a.credID)
	if w := f.do(t, "POST", "/passkey/delete", s, map[string]string{"id": id}); w.Code == http.StatusNoContent {
		t.Fatal("a session with no sign-in time removed a passkey without a proof")
	}
}

// The added-passkey notice carries no caller-chosen text: a passkey name
// holding a link or markup must not reach mail sent from the app's own
// domain (the duplicate-register notice keeps no caller text for the same
// reason).
func TestAddedNoticeCarriesNoCallerText(t *testing.T) {
	f := newPasskeyFixture(t)
	s := f.session(t, "u-alice")
	a := newFakeAuthenticator(t, coseAlgES256)
	ch := challengeOf(t, f.do(t, "POST", "/passkey/register/options", s, nil))
	body := registerBody(a, ch)
	body["name"] = `<a href="https://evil.example/r">Restore access</a>`
	if w := f.do(t, "POST", "/passkey/register", s, body); w.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", w.Code, w.Body)
	}
	_, got := f.notices.snapshot()
	if strings.Contains(got, "evil.example") || strings.Contains(got, "<a") {
		t.Fatalf("the added-passkey notice carries caller-chosen text: %q", got)
	}
}

// The notice goes only to a proven address: one nobody verified may be
// someone else's.
func TestAddedNoticeSkipsUnverifiedAddress(t *testing.T) {
	f := newPasskeyFixture(t)
	f.users.byID["u-alice"].emailVerified = false
	f.enrol(t, "u-alice")
	if to, _ := f.notices.snapshot(); to != "" {
		t.Fatalf("notice mailed to the unverified %q", to)
	}
}
