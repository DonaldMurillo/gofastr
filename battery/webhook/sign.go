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

// SignatureHeader carries a delivery's signature.
const SignatureHeader = "X-GoFastr-Signature"

// EventHeader and DeliveryIDHeader carry a delivery's event name and id, both
// signed.
const (
	EventHeader      = "X-GoFastr-Event"
	DeliveryIDHeader = "X-GoFastr-Delivery-ID"
)

// SignWithTimestamp returns a `t=<unix>,v1=<hex>` header whose HMAC covers the
// timestamp, event, delivery id and body, joined by newlines.
func SignWithTimestamp(secret string, unixTimestamp int64, event, deliveryID string, body []byte) string {
	return fmt.Sprintf("t=%d,v1=%s", unixTimestamp, signedMAC(secret, unixTimestamp, event, deliveryID, body))
}

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
// VerifyTimestamped.
const DefaultTimestampTolerance = 5 * time.Minute

// VerifyTimestamped reports whether header is a valid SignWithTimestamp
// signature of body for event and deliveryID within tolerance.
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
