package webhook

import (
	"net/http"
	"strings"
	"time"
)

// SignatureHeader is the response/request header that carries the
// payload signature.
const SignatureHeader = "X-GoFastr-Signature"

// EventHeader and DeliveryIDHeader carry a delivery's event name and id.
// Both are signed, so a receiver passes them to VerifyTimestamped.
const (
	EventHeader      = "X-GoFastr-Event"
	DeliveryIDHeader = "X-GoFastr-Delivery-ID"
)

// SignWithTimestamp returns a `t=<unix>,v1=<hex-hmac>` signature header
// where the HMAC covers `<unix>\n<event>\n<delivery id>\n<body>`. Binding
// the timestamp lets receivers reject replays via VerifyTimestamped;
// binding the event and the delivery id stops a captured delivery from
// being re-sent under another event name or id inside the window.
//
// The parts are joined with a newline, which an HTTP header value cannot
// carry, so no event, id and body split two ways. Event names carry dots
// ("order.created"), which is why a dot does not separate them.
//
// Pass time.Now().Unix() for fresh signatures; tests may pin to a
// specific instant. It is the Sign of the default [SignatureFormat], so
// the format engine and this helper cannot drift.
func SignWithTimestamp(secret string, unixTimestamp int64, event, deliveryID string, body []byte) string {
	return defaultSignatureFormat.Sign(secret, unixTimestamp, event, deliveryID, body)
}

// DefaultTimestampTolerance is the suggested replay window for
// VerifyTimestamped, wide enough to cover modest clock skew, narrow
// enough that a captured signature decays quickly.
const DefaultTimestampTolerance = 5 * time.Minute

// VerifyTimestamped accepts a header in the `t=<unix>,v1=<hex>` form,
// the delivery's event name and id (the EventHeader and DeliveryIDHeader
// values) and a tolerance window. Returns true iff:
//
//   - the header parses cleanly
//   - |now - ts| <= tolerance
//   - neither event nor deliveryID contains a newline
//   - the HMAC over <ts>\n<event>\n<delivery id>\n<body> equals v1
//
// `now` is captured at call time so receivers don't need a clock arg.
//
// tolerance MUST be positive. A non-positive tolerance is treated as a
// usage error and always rejects, otherwise a caller that accidentally
// passed 0 (zero value of a forgotten config field) would silently
// disable the replay check. Use [DefaultTimestampTolerance] for the
// suggested default.
//
// An empty or whitespace-only secret always rejects.
//
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
