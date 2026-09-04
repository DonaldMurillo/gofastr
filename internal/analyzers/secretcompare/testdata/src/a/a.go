// Package a holds the secretcompare fixture reduced from the real bug
// site: framework/experimental/harness/control/auth issuance.go
// Issuer.Confirm as it is today (probe
// TestMintCodeRedConstantTimeCompare, 2026-09-03 round 5, no fix
// applied), with the constant-time controls next to it, reduced from
// battery/setup/token.go tokenEqual and battery/auth/oauth2.go state.
package a

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"os"
)

// pendingMint mirrors issuance.go: a 6-digit code shown out of band.
type pendingMint struct {
	code    string
	claims  string
	expires bool
}

// Issuer is the reduced minter.
type Issuer struct {
	pending map[string]pendingMint
}

// Confirm, as shipped: the user-supplied 6-digit code is compared with
// a raw !=, a short-circuit byte-at-a-time compare on an auth path.
func (i *Issuer) Confirm(mintID, code string) (string, error) {
	p, ok := i.pending[mintID]
	if !ok {
		return "", errUnknownMint
	}
	if code != p.code { // want `code compared with ==/!=: a short-circuit compare on a credential leaks match status through timing; use subtle.ConstantTimeCompare or hmac.Equal`
		return "", errCodeMismatch
	}
	return p.claims, nil
}

// ConfirmFixed is the fix posture from battery/setup/token.go and
// battery/auth/twofa.go: both sides are fixed-width %06d strings, so
// subtle.ConstantTimeCompare applies directly. The == 1 compare is
// against a literal and stays quiet.
func (i *Issuer) ConfirmFixed(mintID, code string) (string, error) {
	p, ok := i.pending[mintID]
	if !ok {
		return "", errUnknownMint
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(p.code)) != 1 {
		return "", errCodeMismatch
	}
	return p.claims, nil
}

// tokenEqual is the battery/setup control verbatim in shape.
func tokenEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// stateEqual is the battery/auth/oauth2.go control: the oauth state
// compare uses hmac.Equal.
func stateEqual(c *http.Cookie, state string) bool {
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) == 1
}

// webhookSigEqual is the battery/webhook control shape: hmac.Equal on
// the signature pair.
func webhookSigEqual(sig string, body []byte, secret []byte) bool {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hmac.Equal([]byte(sig), mac.Sum(nil))
}

// serveTokenGate, reduced from framework/experimental/harness/control
// mcpserver/server.go Serve: the env-supplied token against the
// required one, raw !=. apiKey names a credential across words
// (api+key), and the other side is an input-fetching call.
func serveTokenGate(apiKey string) error {
	if os.Getenv("GOFASTR_HARNESS_TOKEN") != apiKey { // want `apiKey compared with ==/!=: a short-circuit compare on a credential leaks match status through timing; use subtle.ConstantTimeCompare or hmac.Equal`
		return errCodeMismatch
	}
	return nil
}

// serverCopies is silent, reduced from battery/auth/oidc.go:478: a
// credential-named value against another server-held copy read into a
// plain local has no guessable side.
func serverCopies(claims, ui map[string]any) bool {
	tokenSub := claimString(claims, "sub")
	uiSub := claimString(ui, "sub")
	return tokenSub != "" && tokenSub != uiSub
}

func claimString(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return v
}

// contentDigest is silent, reduced from core-ui/compute compute.go:78
// (current.hash == hash): hash-named values are content digests, not
// credentials.
func contentDigest(currentHash, hash string) bool {
	return currentHash == hash
}

// emptyCheck is silent: `code == ""` is not a compare of two secrets.
func emptyCheck(code string) bool {
	return code == ""
}

// lengthCheck is silent: lengths, not bytes.
func lengthCheck(code string, want string) bool {
	return len(code) != len(want)
}

// defaultPIN is a package-level sentinel credential.
const defaultPIN = "000000"

// sentinelCheck is silent: a credential against a named sentinel
// constant is not a compare of two secrets, even with both sides
// credential-named.
func sentinelCheck(pin string) bool {
	return pin != defaultPIN
}

// intCreds is silent: ints have no byte-at-a-time short-circuit, even
// with credential names on both sides.
func intCreds(pin, otherPin int) bool {
	return pin != otherPin
}

// intEnum is silent: an int named code (HTTP status shapes, rr.Code)
// has no byte-at-a-time string short-circuit to leak.
func intEnum(code int) bool {
	return code != 200
}

// intCompare is silent: ints named code (against another variable,
// not a constant) compare no bytes.
func intCompare(code, other int) bool {
	return code != other
}

// nilCheck is silent: != nil is a presence check.
func nilCheck(tok *string) bool {
	return tok != nil
}

// Status is a typed string enum.
type Status string

const (
	// StatusPending is the typed enum constant.
	StatusPending Status = "pending"
)

// enumCheck is silent: comparing against a typed enum constant is not a
// compare of two secrets.
func enumCheck(s Status) bool {
	return s != StatusPending
}

// unrelatedWords is silent: zipcode and encoding are single words that
// contain the letters "code", not the credential token — silent even
// against an input-fetching call.
func unrelatedWords(zipcode string, r *http.Request) bool {
	return r.URL.Query().Get("zip") == zipcode
}

func encoding(a, b string) bool {
	return a == b
}

var (
	errUnknownMint  = &errStr{"unknown mint"}
	errCodeMismatch = &errStr{"code mismatch"}
)

type errStr struct{ msg string }

func (e *errStr) Error() string { return e.msg }
