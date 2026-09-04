//go:build red

package auth

import (
	"net/http"
	"testing"
)

// CONTRACT-QUESTION red: the maintainer must decide whether email identity
// includes Unicode normalization. CanonicalEmail is documented as "trimmed
// and lowercased" (form_decode.go:119-129) and every flow canonicalizes
// through it consistently, so nothing today disagrees with the doc — but NFC
// and NFD spellings of the same visually identical address both pass it and
// become TWO accounts, and OIDC email auto-linking (oauth2.go resolveOAuthUser
// step 2, which canonicalizes the IdP email through the same function) treats
// the two forms as distinct identities for the same mailbox. If the answer is
// "ASCII-only or NFC-normalize at CanonicalEmail", this becomes a fix; if the
// answer is "non-ASCII local parts are the host's problem", delete this test
// and record that beside CanonicalEmail.

// RED TEST — open finding, 2026-09-04 adversarial pass round 3 (tests-only; no fix applied).
// Family: F13 Identity confusion (Unicode normalization of email identity)
// Property: one visually identical email address is one account — the
// canonical form used for uniqueness must fold Unicode normalization
// variants (NFC vs NFD) of the same address onto one identity.
// Surfaces: form_decode.go::CanonicalEmail — the single definition every
// ingestion point funnels through (register/login via decodeAuthCredentials,
// magic-link send, forgot-password, oauth2.go resolveOAuthUser step 2, the
// per-account login limiter key). The stores then key on the canonical
// string as-is (memoryUserStore map key; EntityUserStore email column under
// the DB's byte/collation semantics).
// Finding: CanonicalEmail = strings.ToLower(strings.TrimSpace(email) keeps
// NFC "é" (U+00E9) and NFD "e"+U+0301 distinct. Registering the NFC form
// succeeds; registering the NFD form of the SAME mailbox also succeeds
// (201, second account). The two accounts are indistinguishable to humans
// (mail clients render both identically), so a magic link / reset sent to
// one silently misses the other, and an IdP asserting the address in the
// other normalization links a fresh account instead of the user's existing
// one — the exact "two casings, two accounts" bug #270 fixed for case.
// Severity: low — requires a non-ASCII address and copy-paste normalization
// differences; the confusion is user-visible (wrong account receives mail)
// rather than an attacker takeover, since neither form verifies ownership of
// the other.
// Fix direction: normalize with golang.org/x/text/unicode/nfc (or
// refuse non-NFC input) inside CanonicalEmail so every flow folds the
// variants by construction, mirroring the #270 case fix.

// TestRegisterRejectsUnicodeTwinEmails registers NFC and NFD spellings of
// one address and asserts the second cannot create a second account.
func TestRegisterRejectsUnicodeTwinEmails(t *testing.T) {
	f := auditHarness(t)

	nfc := "jos\u00e9@example.com"  // precomposed é (U+00E9)
	nfd := "jose\u0301@example.com" // e + combining acute (U+0301)
	if nfc == nfd {
		t.Fatal("fixture: NFC and NFD forms unexpectedly equal")
	}
	if CanonicalEmail(nfc) == CanonicalEmail(nfd) {
		return // canonical form already folds normalization variants
	}

	first := (&cookieJar{}).do(f.router, http.MethodPost, "/auth/register",
		map[string]string{"email": nfc, "password": "supersecret1"}, "203.0.113.9:5555")
	if first.Code != http.StatusCreated {
		t.Fatalf("control: NFC register must succeed; got %d %s", first.Code, first.Body.String())
	}

	second := (&cookieJar{}).do(f.router, http.MethodPost, "/auth/register",
		map[string]string{"email": nfd, "password": "supersecret1"}, "203.0.113.9:5555")
	if second.Code != http.StatusConflict {
		t.Errorf("SECURITY: [email-unicode] the NFD spelling of an already-registered NFC address "+
			"created a SECOND account (%d %s; the same mailbox now exists as %q and %q, both canonical): "+
			"CanonicalEmail (ToLower+TrimSpace) folds case (#270) but not Unicode normalization, so "+
			"uniqueness, magic-link/reset delivery, and OIDC email auto-link all treat one mailbox as "+
			"two identities. CONTRACT-QUESTION: maintainer decides NFC-normalize (or refuse non-NFC) "+
			"inside CanonicalEmail versus declaring non-ASCII local parts host territory.",
			second.Code, second.Body.String(), CanonicalEmail(nfc), CanonicalEmail(nfd))
	}
}
