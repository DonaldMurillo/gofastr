package webhook

// The signature covered only <ts>.<payload>, so a delivery captured inside
// the tolerance window could be re-sent with another X-GoFastr-Event or
// X-GoFastr-Delivery-ID and still verify. v1 now signs the event and the
// delivery id too, newline-joined: event names carry dots, and a dot join
// would let "a.b"/"c"/"d" verify as "a"/"b"/"c.d", a forged body
// (issue #614, S41).

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSignatureBindsEventAndID(t *testing.T) {
	const secret = "whsec-test-0123456789" // not-a-secret: test fixture
	ts := time.Now().Unix()
	body := []byte(`{"id":1}`)
	sig := SignWithTimestamp(secret, ts, "order.created", "d1", body)
	tol := DefaultTimestampTolerance

	if !VerifyTimestamped(secret, sig, "order.created", "d1", body, tol) {
		t.Fatal("genuine delivery did not verify")
	}
	if VerifyTimestamped(secret, sig, "order.deleted", "d1", body, tol) {
		t.Error("relabelled event verified")
	}
	if VerifyTimestamped(secret, sig, "order.created", "d2", body, tol) {
		t.Error("relabelled delivery id verified")
	}
	shifted := SignWithTimestamp(secret, ts, "a.b", "c", []byte("d"))
	if VerifyTimestamped(secret, shifted, "a", "b", []byte("c.d"), tol) {
		t.Error("dot-shifted event, id and body verified")
	}
	if VerifyTimestamped(secret, sig, "order.created\nd1", "", body, tol) {
		t.Error("an event carrying the separator verified")
	}
}

func TestTimestampedVerifierReadsHeaders(t *testing.T) {
	const secret = "whsec-test-0123456789" // not-a-secret: test fixture
	body := []byte(`{"id":1}`)
	req := httptest.NewRequest("POST", "/hook", strings.NewReader(string(body)))
	req.Header.Set(EventHeader, "order.created")
	req.Header.Set(DeliveryIDHeader, "d1")
	req.Header.Set(SignatureHeader, SignWithTimestamp(secret, time.Now().Unix(), "order.created", "d1", body))
	verify := TimestampedVerifier(secret, time.Minute)
	if err := verify(req, body); err != nil {
		t.Fatalf("genuine request refused: %v", err)
	}
	req.Header.Set(EventHeader, "order.deleted")
	if err := verify(req, body); err == nil {
		t.Fatal("request with a relabelled event header verified")
	}
}
