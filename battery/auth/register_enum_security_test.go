package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Pins the register anti-enumeration contract, found by the 2026-09-04
// red-probe round (register_enum_red_test.go); fixed by making
// registerHandler answer BOTH the taken and the free address with one
// uniform 202 (form: a cookie-less 303), notifying the existing holder
// instead of creating anything on the taken branch.
//
// Property: an unauthenticated caller cannot learn whether an email
// address holds an account from the register response — the
// known-address and unknown-address answers are indistinguishable, the
// same contract forgot-password (TestForgotPasswordNoEnumeration) and
// login (identical invalid-credentials body) already pin. Both branches
// also burn the same bcrypt work (the hash is computed before the
// branch, the register analogue of forgot-password's
// burnUnknownBranchWork), so the oracle stays closed to a clock too.
// Surfaces: core.go::CorePlugin.registerHandler — the ErrEmailTaken
// branch. No other surface in the battery answers account existence to
// anonymous callers.
func TestRegisterNoEmailTakenOracle(t *testing.T) {
	f := auditHarness(t)
	f.seedUser(t, "u-enum", "owner@example.com", "supersecret1")

	do := func(email string) *httptest.ResponseRecorder {
		jar := &cookieJar{}
		return jar.do(f.router, http.MethodPost, "/auth/register",
			map[string]string{"email": email, "password": "supersecret1"}, "203.0.113.9:5555")
	}

	known := do("owner@example.com")
	unknown := do("nobody-knew@example.com")

	if known.Code != unknown.Code || known.Body.String() != unknown.Body.String() {
		t.Errorf("SECURITY: [register-enum] register answers account existence to anonymous callers: "+
			"known-address response %d %q versus unknown-address response %d %q.",
			known.Code, known.Body.String(), unknown.Code, unknown.Body.String())
	}
	if known.Code != http.StatusAccepted {
		t.Errorf("known-address register: status %d, want the uniform 202", known.Code)
	}
	// A session cookie on only one branch is the same oracle: the taken
	// branch has no user to mint for, so neither branch may set one.
	for _, rec := range []*httptest.ResponseRecorder{known, unknown} {
		for _, c := range rec.Result().Cookies() {
			if c.Name == f.mgr.Config().SessionCookie {
				t.Errorf("SECURITY: [register-enum] register set a session cookie (%s); "+
					"registration must not auto-login", c.Name)
			}
		}
	}
	// The taken branch created nothing: the store still holds exactly
	// the seeded holder for that address.
	if u, _, err := f.store.FindByEmail(context.Background(), "owner@example.com"); err != nil || u == nil || u.GetID() != "u-enum" {
		t.Errorf("known-address register mutated the existing account: %+v %v", u, err)
	}
	// Operator trail: the taken attempt is auditable (register.duplicate
	// names the HOLDER, never echoed to the caller).
	if ev := f.rec.findByKind("register.duplicate"); ev == nil {
		t.Errorf("no register.duplicate event; kinds: %v", f.rec.kinds())
	} else if ev.UserID != "u-enum" {
		t.Errorf("register.duplicate UserID = %q, want the holder u-enum", ev.UserID)
	}
}

// syncEmailSender records every Send call so the async duplicate-notice
// delivery can be observed deterministically.
type syncEmailSender struct {
	mu   sync.Mutex
	sent []string // "to\x00body", in order
	ch   chan struct{}
}

func (s *syncEmailSender) Send(_ context.Context, to, body string) error {
	s.mu.Lock()
	s.sent = append(s.sent, to+"\x00"+body)
	s.mu.Unlock()
	if s.ch != nil {
		close(s.ch)
	}
	return nil
}

// TestRegisterDuplicateNotifiesHolder pins the other half of the round 3
// decision: on a taken address the battery emails the EXISTING holder
// (AuthConfig.RegisterEmailSender) instead of creating anything — the
// caller learns nothing, the holder learns someone tried.
func TestRegisterDuplicateNotifiesHolder(t *testing.T) {
	sender := &syncEmailSender{ch: make(chan struct{})}
	store := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		SessionTTL:          time.Hour,
		SessionCookie:       "session_id",
		UserStore:           store,
		DevMode:             true,
		RegisterEmailSender: sender,
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	seedUser(t, store, "holder@example.com", "supersecret1")
	r := mountRoutes(mgr)

	jar := &cookieJar{}
	rec := jar.do(r, http.MethodPost, "/auth/register",
		map[string]string{"email": "holder@example.com", "password": "supersecret1"}, "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("duplicate register: %d %s", rec.Code, rec.Body.String())
	}

	select {
	case <-sender.ch:
	case <-time.After(2 * time.Second):
		// The delivery is off the timed path by design, but two seconds
		// means it never fired at all.
		t.Fatal("holder was not notified of the duplicate-registration attempt")
	}
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.sent) != 1 {
		t.Fatalf("sender calls = %d, want 1", len(sender.sent))
	}
	to, body, _ := strings.Cut(sender.sent[0], "\x00")
	if to != "holder@example.com" {
		t.Errorf("notice went to %q, want the existing holder", to)
	}
	// The notice carries no URL and no token: it must not be a phish
	// vector, and it must not leak anything account-bearing.
	if strings.Contains(body, "http") {
		t.Errorf("duplicate-notice body carries a URL (%q); it must be credential-free", body)
	}
}
