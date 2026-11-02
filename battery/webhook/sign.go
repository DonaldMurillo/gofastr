package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
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
// specific instant.
func SignWithTimestamp(secret string, unixTimestamp int64, event, deliveryID string, body []byte) string {
	return fmt.Sprintf("t=%d,v1=%s", unixTimestamp, signedMAC(secret, unixTimestamp, event, deliveryID, body))
}

// signedMAC is the hex HMAC-SHA256 of the signed material.
func signedMAC(secret string, unixTimestamp int64, event, deliveryID string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(unixTimestamp, 10)))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(event))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(deliveryID))
	mac.Write([]byte{'\n'})
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
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
func VerifyTimestamped(secret, header, event, deliveryID string, body []byte, tolerance time.Duration) bool {
	if strings.TrimSpace(secret) == "" || header == "" {
		return false
	}
	if strings.ContainsRune(event, '\n') || strings.ContainsRune(deliveryID, '\n') {
		return false
	}
	if tolerance <= 0 {
		return false
	}
	ts, sig, ok := parseTimestampedHeader(header)
	if !ok {
		return false
	}
	// Reject pathological timestamp values whose drift arithmetic
	// would overflow. Anything wildly outside a sane tolerance window
	// is invalid regardless of HMAC consistency.
	now := time.Now().Unix()
	delta := now - ts
	if delta < 0 {
		delta = -delta
	}
	if delta > int64(tolerance/time.Second)+1 {
		return false
	}
	want := signedMAC(secret, ts, event, deliveryID, body)
	return hmac.Equal([]byte(want), []byte(sig))
}

func parseTimestampedHeader(header string) (int64, string, bool) {
	// Expected form: `t=<unix>,v1=<hex>`. Unknown fields (`kid=…`,
	// `v0=…`) are tolerated for forward-compat with other v1 variants,
	// but duplicate `t=` or `v1=` is a signature-smuggling primitive
	// (last-wins lets an attacker pair a valid timestamp with a bogus
	// signature or vice versa), reject the whole header.
	var (
		tsRaw, sig string
		tsSeen     bool
		sigSeen    bool
	)
	for part := range strings.SplitSeq(header, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			if tsSeen {
				return 0, "", false
			}
			tsRaw = kv[1]
			tsSeen = true
		case "v1":
			if sigSeen {
				return 0, "", false
			}
			sig = kv[1]
			sigSeen = true
		}
	}
	if tsRaw == "" || sig == "" {
		return 0, "", false
	}
	ts, err := strconv.ParseInt(tsRaw, 10, 64)
	if err != nil {
		return 0, "", false
	}
	return ts, sig, true
}
