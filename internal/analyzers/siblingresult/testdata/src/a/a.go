// Package a holds the MintSession call sites, reduced from battery/auth
// before b508cc0d (magic link) and b9b236eb (OAuth callback).
package a

import (
	"context"
	"net/http"

	"example.com/lib"
)

type Session struct{ Token string }

type manager struct{}

// MintSession creates a session and applies the second-factor mark.
//
//gofastr:mustuse pending
func (m *manager) MintSession(ctx context.Context, userID string) (sess *Session, pending bool, err error) { // want MintSession:`mustUse\[1\]`
	return &Session{Token: userID}, false, nil
}

// magicLinkVerify is the verify handler before b508cc0d.
func magicLinkVerify(m *manager, w http.ResponseWriter, r *http.Request) {
	sess, _, err := m.MintSession(r.Context(), "u") // want `MintSession's result pending is marked //gofastr:mustuse and this call blanks it`
	if err != nil {
		return
	}
	http.Redirect(w, r, "/"+sess.Token, http.StatusFound)
}

// oauthCallback is the callback before b9b236eb, spelled with =.
func oauthCallback(m *manager, w http.ResponseWriter, r *http.Request) {
	var sess *Session
	var err error
	sess, _, err = m.MintSession(r.Context(), "u") // want `MintSession's result pending is marked`
	_, _ = sess, err
}

// varSpec spells the blank in a var declaration.
func varSpec(m *manager) {
	var sess, _, err = m.MintSession(context.Background(), "u") // want `MintSession's result pending is marked`
	_, _ = sess, err
}

// password is the fix: pending is read.
func password(m *manager, w http.ResponseWriter, r *http.Request) {
	sess, pending, err := m.MintSession(r.Context(), "u")
	if err != nil {
		return
	}
	target := "/"
	if pending {
		target = "/2fa"
	}
	http.Redirect(w, r, target+sess.Token, http.StatusFound)
}

// crossPackage blanks a result marked in another package.
func crossPackage() {
	sess, _, err := lib.Mint("u") // want `Mint's result pending is marked`
	_, _ = sess, err
	a, _, _ := lib.Both() // want `Both's result b is marked`
	_ = a
}

// ---- quiet ----

// effectOnly discards every result: a call for its effect.
func effectOnly(m *manager) {
	_, _, _ = m.MintSession(context.Background(), "u")
	m.MintSession(context.Background(), "u")
}

// forwarded hands every result on.
func forwarded(m *manager) (*Session, bool, error) {
	return m.MintSession(context.Background(), "u")
}

// unmarked results stay the caller's call.
func unmarked() string {
	v, _ := lib.Lookup("k")
	n, _ := lib.Prose()
	sess, pending, _ := lib.Mint("u") // err is unmarked; discardederr's territory
	_, _, _ = sess, pending, n
	return v
}

// stepUp blanks pending on purpose, and says why.
func stepUp(m *manager) {
	//gofastr:allow(siblingresult) the fresh session is marked cleared on the next line
	fresh, _, err := m.MintSession(context.Background(), "u") // want `MintSession's result pending is marked`
	_, _ = fresh, err
}
