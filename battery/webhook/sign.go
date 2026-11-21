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

// SignWithTimestamp returns the default format's `t=<unix>,v1=<hex>` header, whose HMAC covers
// the timestamp, event, delivery id and body joined by newlines.
func SignWithTimestamp(secret string, unixTimestamp int64, event, deliveryID string, body []byte) string {
	return defaultSignatureFormat.Sign(secret, unixTimestamp, event, deliveryID, body)
}

// DefaultTimestampTolerance is the suggested replay window for
// VerifyTimestamped.
const DefaultTimestampTolerance = 5 * time.Minute

// VerifyTimestamped reports whether header is a valid SignWithTimestamp signature of body for
// event and deliveryID, signed within tolerance of now.
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
