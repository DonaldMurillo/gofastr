package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// listOf reads GET /passkey/list for cookie.
func (f *passkeyFixture) listOf(t *testing.T, cookie string) passkeyList {
	t.Helper()
	w := f.do(t, "GET", "/passkey/list", cookie, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	var out passkeyList
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

type passkeyList struct {
	RPID     string `json:"rpId"`
	UserID   string `json:"userId"`
	UserName string `json:"userName"`
	Passkeys []struct {
		ID         string   `json:"id"`
		Name       string   `json:"name"`
		AAGUID     string   `json:"aaguid"`
		DeviceType string   `json:"deviceType"`
		Transports []string `json:"transports"`
	} `json:"passkeys"`
}

// A sign-in with a passkey the server never stored, or has removed,
// answers 404 so the page can tell the password manager to drop it; the
// message stays the one every failed sign-in shows.
func TestPasskeyUnknownCredentialIs404(t *testing.T) {
	f := newPasskeyFixture(t)
	stranger := newFakeAuthenticator(t, coseAlgES256)
	w := f.login(t, stranger, "u-alice")
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown credential: %d %s", w.Code, w.Body)
	}
	a := f.enrol(t, "u-alice")
	a.credID = slices.Clone(a.credID)
	a.signer = stranger.signer // a known id with the wrong key is a 401, not a 404
	if w2 := f.login(t, a, "u-alice"); w2.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature on a known credential: %d", w2.Code)
	}
	var e1, e2 map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &e1)
	w3 := f.login(t, stranger, "u-alice")
	_ = json.Unmarshal(w3.Body.Bytes(), &e2)
	if e1["error"] == nil || e1["error"] != e2["error"] {
		t.Fatalf("404 body changed between attempts: %v vs %v", e1, e2)
	}
}

// Registration keeps the transports the browser reported (known ones,
// sorted, once each) and the authenticator's AAGUID; the list shows
// them with the device type, and the next registration's
// excludeCredentials carries the transports.
func TestPasskeyKeepsTransportsAndAAGUID(t *testing.T) {
	f := newPasskeyFixture(t)
	cookie := f.session(t, "u-alice")
	a := newFakeAuthenticator(t, coseAlgES256)
	a.aaguid = []byte{0xea, 0x9b, 0x8d, 0x66, 0x4d, 0x01, 0x1d, 0x21, 0x3c, 0xe4, 0xb6, 0xb4, 0x8c, 0xb5, 0x75, 0xd4}
	ch := challengeOf(t, f.do(t, "POST", "/passkey/register/options", cookie, nil))
	body := map[string]any{}
	for k, v := range registerBody(a, ch) {
		body[k] = v
	}
	body["transports"] = []string{"internal", "carrier-pigeon", "hybrid", "internal"}
	if w := f.do(t, "POST", "/passkey/register", cookie, body); w.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", w.Code, w.Body)
	}
	list := f.listOf(t, cookie)
	if len(list.Passkeys) != 1 {
		t.Fatalf("list: %+v", list)
	}
	got := list.Passkeys[0]
	if !slices.Equal(got.Transports, []string{"hybrid", "internal"}) || got.AAGUID != "ea9b8d66-4d01-1d21-3ce4-b6b48cb575d4" || got.DeviceType != "multiDevice" {
		t.Fatalf("stored passkey: %+v", got)
	}
	w := f.do(t, "POST", "/passkey/register/options", cookie, nil)
	var opts struct {
		PublicKey struct {
			Exclude []struct {
				ID         string   `json:"id"`
				Transports []string `json:"transports"`
			} `json:"excludeCredentials"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &opts); err != nil {
		t.Fatal(err)
	}
	if len(opts.PublicKey.Exclude) != 1 || !slices.Equal(opts.PublicKey.Exclude[0].Transports, []string{"hybrid", "internal"}) {
		t.Fatalf("excludeCredentials: %s", w.Body)
	}
}

// The list names the relying party, the account's user handle and its
// name, the inputs of the browser's Signal API calls.
func TestPasskeyListNamesTheAccount(t *testing.T) {
	f := newPasskeyFixture(t)
	f.enrol(t, "u-alice")
	list := f.listOf(t, f.session(t, "u-alice"))
	if list.RPID != "example.com" || list.UserID != b64url.EncodeToString([]byte("u-alice")) || list.UserName != "alice@example.com" {
		t.Fatalf("list: %+v", list)
	}
	if list.Passkeys[0].DeviceType != "singleDevice" || list.Passkeys[0].AAGUID != "" {
		t.Fatalf("device-bound passkey with no AAGUID: %+v", list.Passkeys[0])
	}
}

// Rename changes only the caller's own passkey and cleans the name the
// way registration does.
func TestPasskeyRenameOnlyOwn(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	alice, bob := f.session(t, "u-alice"), f.session(t, "u-bob")
	id := b64url.EncodeToString(a.credID)
	if w := f.do(t, "POST", "/passkey/rename", bob, map[string]string{"id": id, "name": "Mine now"}); w.Code != http.StatusNotFound {
		t.Fatalf("bob renamed alice's passkey: %d", w.Code)
	}
	if w := f.do(t, "POST", "/passkey/rename", alice, map[string]string{"id": id, "name": "bad\u0007name"}); w.Code != http.StatusBadRequest {
		t.Fatalf("control character accepted: %d", w.Code)
	}
	if w := f.do(t, "POST", "/passkey/rename", alice, map[string]string{"id": id, "name": "  Phone  "}); w.Code != http.StatusNoContent {
		t.Fatalf("rename: %d %s", w.Code, w.Body)
	}
	if got := f.listOf(t, alice).Passkeys[0].Name; got != "Phone" {
		t.Fatalf("name after rename: %q", got)
	}
	if f.sink.findByKind("passkey.renamed") == nil {
		t.Fatal("no passkey.renamed event")
	}
	if w := f.do(t, "POST", "/passkey/rename", "", map[string]string{"id": id, "name": "x"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous rename: %d", w.Code)
	}
}

// A conditional create (the browser saves a passkey without a prompt
// after a password sign-in) may carry neither UP nor UV; its options ask
// for user verification only as preferred. A regular registration
// challenge still refuses such authenticator data.
func TestPasskeyConditionalCreate(t *testing.T) {
	f := newPasskeyFixture(t)
	cookie := f.session(t, "u-alice")
	w := f.do(t, "POST", "/passkey/register/options", cookie, map[string]bool{"conditional": true})
	var opts struct {
		PublicKey struct {
			Sel map[string]any `json:"authenticatorSelection"`
		} `json:"publicKey"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &opts)
	if opts.PublicKey.Sel["userVerification"] != "preferred" {
		t.Fatalf("conditional options: %s", w.Body)
	}
	quiet := newFakeAuthenticator(t, coseAlgES256)
	quiet.flags &^= flagUP | flagUV
	if w := f.do(t, "POST", "/passkey/register", cookie, registerBody(quiet, challengeOf(t, w))); w.Code != http.StatusCreated {
		t.Fatalf("conditional create without UP/UV: %d %s", w.Code, w.Body)
	}

	loud := newFakeAuthenticator(t, coseAlgES256)
	loud.flags &^= flagUP | flagUV
	ch := challengeOf(t, f.do(t, "POST", "/passkey/register/options", cookie, nil))
	if w := f.do(t, "POST", "/passkey/register", cookie, registerBody(loud, ch)); w.Code != http.StatusBadRequest {
		t.Fatalf("regular registration without UP/UV: %d", w.Code)
	}
	// The passkey a conditional create stored still needs UV to sign in.
	if w := f.login(t, quiet, "u-alice"); w.Code != http.StatusUnauthorized {
		t.Fatalf("sign-in without UV: %d", w.Code)
	}
}

// reauthProof runs the step-up ceremony for cookie with authenticator a
// and answers the response.
func (f *passkeyFixture) reauth(t *testing.T, cookie string, a *fakeAuthenticator, userID string) *httptest.ResponseRecorder {
	t.Helper()
	ch := challengeOf(t, f.do(t, "POST", "/passkey/reauth/options", cookie, nil))
	return f.do(t, "POST", "/passkey/reauth", cookie, loginBody(a, ch, userID))
}

func (f *passkeyFixture) redeem(cookie, proof string) error {
	req := httptest.NewRequest("POST", "https://example.com/account/delete", nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	}
	return f.pk.RedeemReauthProof(req, proof)
}

// A step-up offers only the session's own passkeys, answers a proof
// that RedeemReauthProof accepts once and only for that account, and
// refuses another account's passkey.
func TestPasskeyReauthProof(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	b := f.enrol(t, "u-bob")
	alice, bob := f.session(t, "u-alice"), f.session(t, "u-bob")

	w := f.do(t, "POST", "/passkey/reauth/options", alice, nil)
	var opts struct {
		PublicKey struct {
			Allow []struct {
				ID string `json:"id"`
			} `json:"allowCredentials"`
		} `json:"publicKey"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &opts)
	if len(opts.PublicKey.Allow) != 1 || opts.PublicKey.Allow[0].ID != b64url.EncodeToString(a.credID) {
		t.Fatalf("allowCredentials: %s", w.Body)
	}

	w = f.reauth(t, alice, a, "u-alice")
	if w.Code != http.StatusOK {
		t.Fatalf("reauth: %d %s", w.Code, w.Body)
	}
	var out struct {
		Proof string `json:"proof"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.Proof == "" {
		t.Fatalf("no proof: %s", w.Body)
	}
	if err := f.redeem(bob, out.Proof); !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("bob spent alice's proof: %v", err)
	}
	w = f.reauth(t, alice, a, "u-alice")
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if err := f.redeem(alice, out.Proof); err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if err := f.redeem(alice, out.Proof); !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("proof spent twice: %v", err)
	}
	if err := f.redeem(alice, ""); !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("empty proof: %v", err)
	}
	if f.sink.findByKind("passkey.reauthenticated") == nil {
		t.Fatal("no passkey.reauthenticated event")
	}

	// Bob's passkey on Alice's step-up is not one of hers.
	if w := f.reauth(t, alice, b, "u-bob"); w.Code != http.StatusNotFound {
		t.Fatalf("another account's passkey: %d %s", w.Code, w.Body)
	}
	// A sign-in challenge is not a step-up challenge.
	ch := challengeOf(t, f.do(t, "POST", "/passkey/login/options", "", nil))
	if w := f.do(t, "POST", "/passkey/reauth", alice, loginBody(a, ch, "u-alice")); w.Code != http.StatusUnauthorized {
		t.Fatalf("login challenge spent on reauth: %d", w.Code)
	}
	if w := f.do(t, "POST", "/passkey/reauth/options", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous reauth options: %d", w.Code)
	}
}

// A proof is not a session: one redeemed under a session waiting on
// its second factor is refused.
func TestPasskeyReauthProofNeedsFullSession(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	alice := f.session(t, "u-alice")
	w := f.reauth(t, alice, a, "u-alice")
	var out struct {
		Proof string `json:"proof"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	enableTwoFA(t, f, "u-alice")
	pending := passkeySessionCookie(f.login(t, a, "u-alice"))
	if pending == "" {
		t.Fatal("no pending session")
	}
	if err := f.redeem(pending, out.Proof); !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("pending session spent a proof: %v", err)
	}
}

// A session minted before the account turned 2FA on is not pending, but
// it never passed the second factor: the step-up routes refuse it, and
// so must RedeemReauthProof.
func TestPasskeyReauthProofRefusesPreTwoFactorSession(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	old := f.session(t, "u-alice")
	w := f.reauth(t, f.session(t, "u-alice"), a, "u-alice")
	var out struct {
		Proof string `json:"proof"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	enableTwoFA(t, f, "u-alice")
	if w := f.do(t, "POST", "/passkey/reauth/options", old, nil); w.Code != http.StatusForbidden {
		t.Fatalf("pre-2FA session got step-up options: %d", w.Code)
	}
	if err := f.redeem(old, out.Proof); !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("pre-2FA session spent a proof: %v", err)
	}
}

// register/options read no body before conditional create; a body with
// other members, or none that parses, is still a regular registration.
func TestPasskeyRegisterOptionsToleratesOtherBodies(t *testing.T) {
	f := newPasskeyFixture(t)
	cookie := f.session(t, "u-alice")
	for _, body := range []any{nil, map[string]any{}, map[string]any{"name": "Laptop"}, map[string]any{"conditional": "yes"}} {
		w := f.do(t, "POST", "/passkey/register/options", cookie, body)
		var opts struct {
			PublicKey struct {
				Sel map[string]any `json:"authenticatorSelection"`
			} `json:"publicKey"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &opts)
		if w.Code != http.StatusOK || opts.PublicKey.Sel["userVerification"] != "required" {
			t.Errorf("body %v: %d %s", body, w.Code, w.Body)
		}
	}
}
