package auth

import (
	"net/http"
	"testing"
)

// SECURITY TEST — fixed 2026-09-05, red-probe round 4.
// Family: F25 Bidi, invisible, and confusable characters
// Property: one visually identical email address is one account — the canonical
// form used for uniqueness must not admit invisible-character twins (the pinned
// property of TestRegisterFoldsUnicodeTwinEmails, extended from NFC/NFD to the
// invisible codepoints NFC cannot fold).
// Surfaces: form_decode.go::CanonicalEmail / stripInvisibleEmailRunes (the
// single funnel: register/login via decodeAuthCredentials, magiclink.go
// sendHandler/verifyHandler, password_reset.go forgotHandler, oauth2.go
// resolveOAuthUser step 2, the per-account login limiter key), twofa.go::
// buildOTPAuthURL (the canonical email becomes the QR label), notify/email
// bodies that echo the address.
// Fix: the default canonicalizer strips the zero-width/bidi set and the C1
// block (core/textsafe) BEFORE NFC/lower/trim folding, so every twin lands
// on the base account. The AuthConfig.CanonicalizeEmail override stays
// total and owns its own invisible-character policy.

// TestInvisibleTwinEmailsOneAccount registers a base address and then four
// visually identical invisible-character twins; every twin must land on the
// SAME account (folded or refused), never a second one.
func TestInvisibleTwinEmailsOneAccount(t *testing.T) {
	f := auditHarness(t)
	jar := &cookieJar{}

	base := "owner@example.com"
	reg := (&cookieJar{}).do(f.router, http.MethodPost, "/auth/register",
		map[string]string{"email": base, "password": "supersecret1"}, "203.0.113.9:5555")
	if reg.Code != http.StatusAccepted {
		t.Fatalf("setup: base register got %d %s", reg.Code, reg.Body.String())
	}

	twins := []string{
		"ow\u200Bner@example.com",       // zero-width space
		"owner@example.com\uFEFF",       // BOM / zero-width no-break space
		"ow\u202Ener@example.com",       // right-to-left override
		"ow\u2066ner\u2069@example.com", // LRI ... PDI isolates
	}
	for _, twin := range twins {
		res := jar.do(f.router, http.MethodPost, "/auth/register",
			map[string]string{"email": twin, "password": "supersecret1"}, "203.0.113.9:5555")
		_ = res // the response is uniform (202) on every branch by design; the store is the oracle
	}

	f.store.mu.Lock()
	count := len(f.store.users)
	f.store.mu.Unlock()
	if count != 1 {
		t.Fatalf("SECURITY: [email-invisible] base + 4 invisible twins produced %d accounts; want 1 — an invisible-codepoint twin of a registered address became a second, visually identical identity", count)
	}
}
