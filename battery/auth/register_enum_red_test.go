//go:build red

package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// CONTRACT-QUESTION red: the maintainer must decide whether POST /auth/register
// may reveal that an address holds an account. Today it does, deliberately:
// auth.go:76-79 documents ErrEmailTaken so "the register handler can return
// 409 instead of 500", oauth2_test.go:944 calls the 409 "the conventional
// response", and the form path answers `email_taken`. But every sibling
// unauthenticated surface in this battery pins the OPPOSITE policy —
// forgot-password's uniform 200 (TestForgotPasswordNoEnumeration and the
// same-work pin), login's identical invalid-credentials body, magic-link
// send's uniform response — making register the one unauthenticated oracle
// for the exact same account table. If 409 is the chosen product behaviour,
// delete this test and note the decision beside one of the anti-enumeration
// pins; if enumeration must close, the handler needs the same shape parity
// (uniform response + burned branch work) as forgot-password.

// RED TEST — open finding, 2026-09-04 adversarial pass round 3 (tests-only; no fix applied).
// Family: F6 Information leakage in responses (account-existence oracle by
// status code + message shape on register)
// Property: an unauthenticated caller cannot learn whether an email address
// holds an account from the register response — the known-address and
// unknown-address answers are indistinguishable, the same contract
// forgot-password and login already pin.
// Surfaces: core.go::CorePlugin.registerHandler (the ErrEmailTaken branch,
// core.go:514-522: 409 "email already registered" / form `email_taken`
// versus 201 with the created user). No other surface in the battery
// answers account existence to anonymous callers.
// Finding: POST /auth/register returns 409 {"error":"email already
// registered"} for a registered address and 201 for an unknown one, so one
// unauthenticated request per address enumerates the user table — the exact
// oracle password_reset.go spends its whole uniform-200 design (and
// burnUnknownBranchWork) closing on the adjacent endpoint.
// Severity: low — deliberate, documented behaviour and register-oracles are
// a widely accepted trade-off; kept because the battery's own sibling policy
// contradicts it and round 3 explicitly targets message-shape oracles.
// Fix direction: if the contract flips, answer both branches with one status
// and one body (send-verification-style), spending equivalent work on the
// unknown branch like forgot-password does.

// TestRegisterNoEmailTakenOracle registers a known and an unknown address
// and asserts the responses cannot be told apart.
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

	if known.Code == unknown.Code && known.Body.String() == unknown.Body.String() {
		return // indistinguishable: contract holds
	}
	t.Errorf("SECURITY: [register-enum] register answers account existence to anonymous callers: "+
		"known-address response %d %q versus unknown-address response %d %q. forgot-password spends a "+
		"uniform 200 + burned branch work (password_reset.go) closing this same oracle; register is the "+
		"one unauthenticated surface left answering it. CONTRACT-QUESTION: if the 409 is the chosen "+
		"product behaviour (auth.go ErrEmailTaken doc, oauth2_test.go \"conventional\"), record that "+
		"decision beside the anti-enumeration pins instead.",
		known.Code, known.Body.String(), unknown.Code, unknown.Body.String())
}
