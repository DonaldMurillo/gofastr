package webhook

import (
	"net/http"
	"strings"
	"time"
)

// SignatureHeader carries a delivery's signature.
const SignatureHeader = "X-GoFastr-Signature"

// EventHeader and DeliveryIDHeader carry a delivery's event name and id, both
// signed.
const (
	EventHeader      = "X-GoFastr-Event"
	DeliveryIDHeader = "X-GoFastr-Delivery-ID"
)

// specific instant. It is the Sign of the default [SignatureFormat], so
// the format engine and this helper cannot drift.
func SignWithTimestamp(secret string, unixTimestamp int64, event, deliveryID string, body []byte) string {
	return defaultSignatureFormat.Sign(secret, unixTimestamp, event, deliveryID, body)
}

// DefaultTimestampTolerance is the suggested replay window for
// VerifyTimestamped.
const DefaultTimestampTolerance = 5 * time.Minute

// The signature can carry additional comma-separated fields such as
// `kid=…`. Verification ignores those fields and refuses repeated
// `t=` or `v1=` fields.
func VerifyTimestamped(secret, header, event, deliveryID string, body []byte, tolerance time.Duration) bool {
	// The signed material joins on newlines, which a header value cannot
	// carry; an event or id holding one can never match a real delivery.
	if strings.ContainsRune(event, '\n') || strings.ContainsRune(deliveryID, '\n') {
		return false
	}
	h := http.Header{}
	h.Set(SignatureHeader, header)
	h.Set(EventHeader, event)
	h.Set(DeliveryIDHeader, deliveryID)
	return defaultSignatureFormat.verify(secret, h, body, tolerance, true)
}
