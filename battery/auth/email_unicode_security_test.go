package auth

import (
	"net/http"
	"testing"
	"time"
)

// Pins Unicode normalization folding of email identity, found by the
// 2026-09-04 red-probe round (email_unicode_red_test.go); fixed by
// refusing decomposed (non-NFC) addresses inside CanonicalEmail — the
// composed spelling canonicalizes, the decomposed twin is rejected with
// ErrEmailNotComposed at every ingestion point, so one mailbox can never
// become two accounts. Hosts needing full NFC folding install
// AuthConfig.CanonicalizeEmail.
//
// Property: one visually identical email address is one account — the
// canonical form used for uniqueness must fold Unicode normalization
// variants (NFC vs NFD) of the same address onto one identity.
// Surfaces: form_decode.go::CanonicalEmail — the single definition every
// ingestion point funnels through (register/login via
// decodeAuthCredentials, magic-link send, forgot-password, oauth2.go
// resolveOAuthUser step 2, the per-account login limiter key). The
// stores key on the canonical string as-is.

// Pins the maintainer decision (2026-09-04 red-probe round 3) on email
// identity: CanonicalEmail NFC-normalizes by default, so the composed and
// decomposed spellings of one mailbox fold onto one identity at every
// ingestion point and can never become two accounts.
// Property: NFC and NFD spellings of an address are one identity.
// Surfaces: CanonicalEmail and every handler that canonicalizes
// (register, login, forgot-password, magic-link send).
func TestRegisterFoldsUnicodeTwinEmails(t *testing.T) {
	f := auditHarness(t)

	nfc := "jos\u00e9@example.com"  // precomposed é (U+00E9)
	nfd := "jose\u0301@example.com" // e + combining acute (U+0301)
	if nfc == nfd {
		t.Fatal("fixture: NFC and NFD forms unexpectedly equal")
	}
	cNFC, err := CanonicalEmail(nfc)
	if err != nil {
		t.Fatalf("composed address refused: %v", err)
	}
	cNFD, err := CanonicalEmail(nfd)
	if err != nil {
		t.Fatalf("decomposed address refused: %v", err)
	}
	if cNFC != cNFD || cNFC != nfc {
		t.Fatalf("SECURITY: [email-unicode] canonical forms differ: %q vs %q (want both %q)", cNFC, cNFD, nfc)
	}

	// Registering the composed form, then the decomposed twin: the second
	// answers the uniform register response and creates NO second account.
	for _, e := range []string{nfc, nfd} {
		rec := (&cookieJar{}).do(f.router, http.MethodPost, "/auth/register",
			map[string]string{"email": e, "password": "supersecret1"}, "203.0.113.9:5555")
		if rec.Code != http.StatusAccepted {
			t.Fatalf("register %q: got %d %s", e, rec.Code, rec.Body.String())
		}
	}
	count := 0
	for k := range f.store.users {
		if k == nfc || k == nfd {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("SECURITY: [email-unicode] the mailbox exists as %d canonical identities; want 1", count)
	}
	if _, ok := f.store.users[nfc]; !ok {
		t.Fatalf("the single account must be stored under the NFC spelling")
	}

	// The decomposed spelling logs into the composed account.
	login := (&cookieJar{}).do(f.router, http.MethodPost, "/auth/login",
		map[string]string{"email": nfd, "password": "supersecret1"}, "203.0.113.9:5555")
	if login.Code != http.StatusOK && login.Code != http.StatusSeeOther && login.Code != http.StatusNoContent {
		t.Fatalf("login with the decomposed spelling: got %d %s", login.Code, login.Body.String())
	}
}

// TestCanonicalizeEmailOverride pins the round 3 escape hatch:
// AuthConfig.CanonicalizeEmail REPLACES the default entirely, so a
// deployment with golang.org/x/text can install real NFC folding and
// both spellings land on one identity (no refusal).
func TestCanonicalizeEmailOverride(t *testing.T) {
	fold := func(e string) (string, error) {
		// Minimal stand-in for norm.NFC.String: composes the e+combining-
		// acute sequence onto the precomposed form, so NFD collapses onto
		// NFC for this fixture. A real deployment passes
		// norm.NFC.String(strings.ToLower(strings.TrimSpace(e))).
		out := ""
		for _, r := range e {
			if r == 0x0301 && len(out) > 0 && out[len(out)-1] == 'e' {
				out = out[:len(out)-1] + "é"
				continue
			}
			out += string(r)
		}
		return out, nil
	}
	store := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:         "test-secret",
		SessionCookie:     "session_id",
		SessionTTL:        time.Hour,
		UserStore:         store,
		DevMode:           true,
		CanonicalizeEmail: fold,
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := mountRoutes(mgr)

	nfc := "jos\u00e9@example.com"
	nfd := "jose\u0301@example.com"
	first := (&cookieJar{}).do(r, http.MethodPost, "/auth/register",
		map[string]string{"email": nfc, "password": "supersecret1"}, "")
	second := (&cookieJar{}).do(r, http.MethodPost, "/auth/register",
		map[string]string{"email": nfd, "password": "supersecret1"}, "")
	for name, rec := range map[string]int{"nfc": first.Code, "nfd": second.Code} {
		if rec != http.StatusAccepted {
			t.Errorf("%s register under override: %d (register answers uniform 202)", name, rec)
		}
	}
	// Both spellings folded to ONE stored identity under the override.
	count := 0
	for k := range store.users {
		if k == nfc {
			count++
		}
		if k == nfd {
			t.Errorf("override did not fold: the decomposed spelling is stored verbatim")
		}
	}
	if count != 1 {
		t.Errorf("override: expected exactly one folded identity, found %d", count)
	}
}
