package middleware

import (
	"crypto/rand"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCSRF_KeyRotationAcceptsAdditionalKeys pins multi-key rotation:
// a deploy can roll a new SecretKey forward while still honoring tokens
// signed by the previous key, by listing additional verify-only keys.
// Without this, every rolling deploy invalidates every in-flight form.
func TestCSRF_KeyRotationAcceptsAdditionalKeys(t *testing.T) {
	oldKey := mustRandomKey()
	newKey := mustRandomKey()

	// "Old" middleware — mints a token with oldKey.
	old := CSRF(CSRFConfig{SecretKey: oldKey, FormField: "_csrf"})
	primeRec := httptest.NewRecorder()
	old(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).
		ServeHTTP(primeRec, httptest.NewRequest(http.MethodGet, "/", nil))
	var ck *http.Cookie
	for _, c := range primeRec.Result().Cookies() {
		if c.Name == "csrf_token" {
			ck = c
		}
	}
	if ck == nil {
		t.Fatal("no cookie from old middleware")
	}

	// "New" middleware — primary key is newKey, but oldKey is listed in
	// AdditionalKeys so verification still succeeds for in-flight tokens.
	mw := CSRF(CSRFConfig{
		SecretKey:      newKey,
		AdditionalKeys: [][]byte{oldKey},
		FormField:      "_csrf",
	})
	post := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/save",
		strings.NewReader("_csrf="+ck.Value))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(ck)
	rec := httptest.NewRecorder()
	post.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("rolled-deploy token rejected: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestCSRF_KeyRotationRemintsWithPrimary pins the next-step of rotation:
// after rolling SecretKey forward (with the old key listed as
// AdditionalKeys for transitional verification), the next safe-method
// GET that lands a fresh cookie must sign with the NEW primary key,
// NOT the old one. Otherwise the rotation never completes — operators
// could never drop the old key from AdditionalKeys.
func TestCSRF_KeyRotationRemintsWithPrimary(t *testing.T) {
	oldKey := mustRandomKey()
	newKey := mustRandomKey()

	mw := CSRF(CSRFConfig{
		SecretKey:      newKey,
		AdditionalKeys: [][]byte{oldKey},
		FormField:      "_csrf",
	})

	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	var ck *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "csrf_token" {
			ck = c
		}
	}
	if ck == nil {
		t.Fatal("no cookie minted")
	}

	// The freshly-minted cookie must verify against newKey alone — NOT
	// require AdditionalKeys to pass. Verify with primary-only.
	if !verifySignedCSRFToken(ck.Value, newKey) {
		t.Errorf("freshly minted token did not verify against new primary key (still signed by old?)")
	}
	// Sanity: it should NOT verify under the old key alone.
	if verifySignedCSRFToken(ck.Value, oldKey) {
		t.Errorf("freshly minted token verifies under OLD key — rotation didn't actually re-mint")
	}
}

// TestCSRF_KeyRotationMultipleAdditionalKeys covers the off-by-one
// risk in the additional-keys loop: rotating with two prior keys (a
// long rollout) should still accept tokens signed by either.
func TestCSRF_KeyRotationMultipleAdditionalKeys(t *testing.T) {
	keyA := mustRandomKey()
	keyB := mustRandomKey()
	newKey := mustRandomKey()

	// Mint a token under keyA.
	mwA := CSRF(CSRFConfig{SecretKey: keyA, FormField: "_csrf"})
	primeA := httptest.NewRecorder()
	mwA(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).
		ServeHTTP(primeA, httptest.NewRequest(http.MethodGet, "/", nil))
	var ckA *http.Cookie
	for _, c := range primeA.Result().Cookies() {
		if c.Name == "csrf_token" {
			ckA = c
		}
	}

	// New middleware: primary=newKey, additional=[keyA, keyB]. POST
	// using keyA's token must verify.
	mw := CSRF(CSRFConfig{
		SecretKey:      newKey,
		AdditionalKeys: [][]byte{keyA, keyB},
		FormField:      "_csrf",
	})
	postReq := httptest.NewRequest(http.MethodPost, "/save",
		strings.NewReader("_csrf="+ckA.Value))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(ckA)
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, postReq)
	if rec.Code != http.StatusOK {
		t.Errorf("token signed by keyA rejected when keyA is in AdditionalKeys: %d / %s", rec.Code, rec.Body.String())
	}
}

// TestCSRF_EmptySecretLogsWarning pins the operator-visible signal that
// the auto-generated key is dangerous in multi-instance / post-deploy
// scenarios.
func TestCSRF_EmptySecretLogsWarning(t *testing.T) {
	var sink strings.Builder
	logger := slog.New(slog.NewTextHandler(&sink, &slog.HandlerOptions{Level: slog.LevelInfo}))

	WarnIfCSRFUnconfigured(CSRFConfig{}, logger)

	out := sink.String()
	if !strings.Contains(out, "SecretKey") || !strings.Contains(strings.ToLower(out), "warn") {
		t.Errorf("missing operator warning: %q", out)
	}
}

func mustRandomKey() []byte {
	k := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, k); err != nil {
		panic(err)
	}
	return k
}
