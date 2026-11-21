package webhook

import (
	"strings"
	"testing"
	"time"
)

// TestVerify_RejectsDuplicateFields pins that a header with two `t=` or
// two `v1=` fields is rejected wholesale. Last-wins parsing would let a
// proxy/attacker pair a valid timestamp with a bogus signature (or vice
// versa) by appending a second copy of the field. Unknown fields are
// still tolerated (forward-compat).
func TestVerify_RejectsDuplicateFields(t *testing.T) {
	secret := "top-secret"
	body := []byte(`{"event":"payment.succeeded"}`)
	now := time.Now().Unix()
	valid := SignWithTimestamp(secret, now, "", "", body)

	cases := map[string]string{
		"duplicate-t":              "t=1," + valid,
		"duplicate-v1":             valid + ",v1=deadbeef",
		"repeat-after-extra-field": valid + ",kid=key-2,v1=deadbeef",
		"whitespace-t":             valid + ", t=1",
		"whitespace-v1":            valid + ",\tv1=deadbeef",
		"interleaved-repeat":       strings.Replace(valid, ",v1=", ", t=1,kid=key-2,v1=", 1),
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			if VerifyTimestamped(secret, header, "", "", body, time.Hour) {
				t.Fatalf("VerifyTimestamped accepted ambiguous header %q", header)
			}
		})
	}

	timestamp, mac, _ := strings.Cut(valid, ",")
	for name, header := range map[string]string{
		"trailing-field":    valid + ",kid=key-2",
		"interleaved-field": timestamp + ",kid=key-2," + mac,
		"leading-field":     "kid=key-2, " + valid,
		"reordered-fields":  mac + "," + timestamp,
		"decorated-time":    strings.Replace(valid, "t=", "t=+0", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if !VerifyTimestamped(secret, header, "", "", body, time.Hour) {
				t.Fatalf("VerifyTimestamped rejected compatible header %q", header)
			}
		})
	}
}

// TestSignature_ValidSignatureAccepted verifies that a correctly signed
// payload passes verification.
func TestSignature_ValidSignatureAccepted(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"event":"test"}`)

	sig := SignWithTimestamp(secret, time.Now().Unix(), "", "", body)
	if !VerifyTimestamped(secret, sig, "", "", body, 5*time.Minute) {
		t.Errorf("VerifyTimestamped rejected valid signature")
	}
}

// TestSignature_WrongSecretRejected verifies that a signature with the
// wrong secret is rejected. Attack: forging webhook signatures.
func TestSignature_WrongSecretRejected(t *testing.T) {
	body := []byte(`{"event":"test"}`)
	sig := SignWithTimestamp("correct-secret", time.Now().Unix(), "", "", body)

	if VerifyTimestamped("wrong-secret", sig, "", "", body, 5*time.Minute) {
		t.Errorf("SECURITY: [signature] VerifyTimestamped accepted signature with wrong secret. Attack: webhook signature forgery.")
	}
}

// TestSignature_ExpiredTimestampRejected verifies that old timestamps
// are rejected. Attack: replaying old webhook signatures.
func TestSignature_ExpiredTimestampRejected(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"event":"test"}`)

	// Sign with a timestamp 10 minutes ago
	oldSig := SignWithTimestamp(secret, time.Now().Add(-10*time.Minute).Unix(), "", "", body)

	if VerifyTimestamped(secret, oldSig, "", "", body, 5*time.Minute) {
		t.Errorf("SECURITY: [signature] VerifyTimestamped accepted expired timestamp (10min old, tolerance=5min). Attack: webhook replay.")
	}
	t.Run("one-minute-and-one-hour-windows", func(t *testing.T) {
		secret := "topsecret"
		body := []byte(`{"k":1}`)
		header := SignWithTimestamp(secret, time.Now().Add(-10*time.Minute).Unix(), "", "", body)
		if !VerifyTimestamped(secret, header, "", "", body, time.Hour) {
			t.Error("VerifyTimestamped rejected a 10-minute-old signature inside a 1-hour tolerance")
		}
		if VerifyTimestamped(secret, header, "", "", body, time.Minute) {
			t.Error("VerifyTimestamped accepted a 10-minute-old signature outside a 1-minute tolerance")
		}
	})
}

// TestSignature_TamperedBodyRejected verifies that modifying the body
// invalidates the signature. Attack: modifying webhook payload.
func TestSignature_TamperedBodyRejected(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"event":"test"}`)

	sig := SignWithTimestamp(secret, time.Now().Unix(), "", "", body)
	tamperedBody := []byte(`{"event":"admin","role":"superuser"}`)

	if VerifyTimestamped(secret, sig, "", "", tamperedBody, 5*time.Minute) {
		t.Errorf("SECURITY: [signature] VerifyTimestamped accepted signature for tampered body. Attack: payload modification bypasses signature.")
	}
}

// TestSignature_InvalidHeaderFormatRejected verifies that malformed
// signature headers are rejected.
func TestSignature_InvalidHeaderFormatRejected(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"event":"test"}`)

	for _, header := range []string{"", "invalid", "t1.not-a-number.sig", "12345"} {
		if VerifyTimestamped(secret, header, "", "", body, 5*time.Minute) {
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
	futureSig := SignWithTimestamp(secret, time.Now().Add(10*time.Minute).Unix(), "", "", body)

	if VerifyTimestamped(secret, futureSig, "", "", body, 5*time.Minute) {
		t.Errorf("SECURITY: [signature] VerifyTimestamped accepted future timestamp (10min ahead, tolerance=5min). Attack: pre-generated future signature.")
	}
}

func TestSignatureRejectsWrongMAC(t *testing.T) {
	body := []byte(`{"event":"test"}`)
	for _, key := range []string{"first-key", "second-key"} {
		signature := SignWithTimestamp(key, time.Now().Unix(), "created", "delivery-1", body)
		if !VerifyTimestamped(key, signature, "created", "delivery-1", body, time.Minute) {
			t.Fatal("valid signature was rejected")
		}
		if VerifyTimestamped("wrong-key", signature, "created", "delivery-1", body, time.Minute) {
			t.Fatal("signature under a different key was accepted")
		}
	}
}
