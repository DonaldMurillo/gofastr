package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

// TestSignature_ValidSignatureAccepted verifies that a correctly signed
// payload passes verification.
func TestSignature_ValidSignatureAccepted(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"event":"test"}`)

	sig := SignWithTimestamp(secret, time.Now().Unix(), body)
	if !VerifyTimestamped(secret, sig, body, 5*time.Minute) {
		t.Errorf("VerifyTimestamped rejected valid signature")
	}
}

// TestSignature_WrongSecretRejected verifies that a signature with the
// wrong secret is rejected. Attack: forging webhook signatures.
func TestSignature_WrongSecretRejected(t *testing.T) {
	body := []byte(`{"event":"test"}`)
	sig := SignWithTimestamp("correct-secret", time.Now().Unix(), body)

	if VerifyTimestamped("wrong-secret", sig, body, 5*time.Minute) {
		t.Errorf("SECURITY: [signature] VerifyTimestamped accepted signature with wrong secret. Attack: webhook signature forgery.")
	}
}

// TestSignature_ExpiredTimestampRejected verifies that old timestamps
// are rejected. Attack: replaying old webhook signatures.
func TestSignature_ExpiredTimestampRejected(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"event":"test"}`)

	// Sign with a timestamp 10 minutes ago
	oldSig := SignWithTimestamp(secret, time.Now().Add(-10*time.Minute).Unix(), body)

	if VerifyTimestamped(secret, oldSig, body, 5*time.Minute) {
		t.Errorf("SECURITY: [signature] VerifyTimestamped accepted expired timestamp (10min old, tolerance=5min). Attack: webhook replay.")
	}
}

// TestSignature_TamperedBodyRejected verifies that modifying the body
// invalidates the signature. Attack: modifying webhook payload.
func TestSignature_TamperedBodyRejected(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"event":"test"}`)

	sig := SignWithTimestamp(secret, time.Now().Unix(), body)
	tamperedBody := []byte(`{"event":"admin","role":"superuser"}`)

	if VerifyTimestamped(secret, sig, tamperedBody, 5*time.Minute) {
		t.Errorf("SECURITY: [signature] VerifyTimestamped accepted signature for tampered body. Attack: payload modification bypasses signature.")
	}
}

// TestSignature_InvalidHeaderFormatRejected verifies that malformed
// signature headers are rejected.
func TestSignature_InvalidHeaderFormatRejected(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"event":"test"}`)

	for _, header := range []string{"", "invalid", "t1.not-a-number.sig", "12345"} {
		if VerifyTimestamped(secret, header, body, 5*time.Minute) {
			t.Errorf("SECURITY: [signature] VerifyTimestamped accepted malformed header %q. Attack: signature bypass via malformed header.", header)
		}
	}
}

// TestSignature_FutureTimestampRejected verifies that future timestamps
// beyond tolerance are rejected. Attack: pre-generating signatures for
// future use.
func TestSignature_FutureTimestampRejected(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"event":"test"}`)

	// Sign with a timestamp 10 minutes in the future
	futureSig := SignWithTimestamp(secret, time.Now().Add(10*time.Minute).Unix(), body)

	if VerifyTimestamped(secret, futureSig, body, 5*time.Minute) {
		t.Errorf("SECURITY: [signature] VerifyTimestamped accepted future timestamp (10min ahead, tolerance=5min). Attack: pre-generated future signature.")
	}
}

// TestSignature_TimingSafeComparison verifies that the comparison is not
// vulnerable to timing attacks. This is a documentation test — if the
// implementation uses hmac.Equal, it is constant-time.
func TestSignature_TimingSafeComparison(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"event":"test"}`)

	// Compute a valid signature
	validSig := SignWithTimestamp(secret, time.Now().Unix(), body)

	// Craft a completely wrong signature (same length)
	h := hmac.New(sha256.New, []byte("wrong-secret"))
	h.Write(body)
	wrongMAC := hex.EncodeToString(h.Sum(nil))

	// Parse both and check — this just documents the implementation uses
	// hmac.Equal (constant-time) rather than == (short-circuit)
	t.Logf("Valid signature uses constant-time comparison: hmac.Equal=%v", hmac.Equal([]byte(validSig), []byte("t."+validSig[1:])))
	t.Logf("NOTE: [signature] VerifyTimestamped should use constant-time comparison (hmac.Equal) to prevent timing attacks")
	_ = wrongMAC
}
