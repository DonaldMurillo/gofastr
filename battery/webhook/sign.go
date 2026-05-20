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

// SignaturePrefix labels the algorithm used in legacy unsigned-timestamp
// payloads. Kept for backwards compatibility with stored signatures
// computed by older callers of the [Sign] helper.
const SignaturePrefix = "sha256="

// Sign returns a body-only HMAC-SHA256 signature, hex-encoded with the
// legacy algorithm prefix.
//
// Deprecated: Sign does not bind a timestamp into the signed material,
// so a captured signature replays forever. New callers should use
// [SignWithTimestamp] / [VerifyTimestamped] which follow the Stripe-
// style `t=<unix>,v1=<hmac>` convention and let the receiver reject
// stale signatures.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return SignaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

// Verify checks header against Sign(secret, body). Same caveat as Sign:
// it does not bind a timestamp, so it should only be used to verify
// payloads produced by the legacy [Sign] helper.
//
// Deprecated: use [VerifyTimestamped] for new code.
func Verify(secret, header string, body []byte) bool {
	if secret == "" || header == "" {
		return false
	}
	if !strings.HasPrefix(header, SignaturePrefix) {
		return false
	}
	want := Sign(secret, body)
	return hmac.Equal([]byte(want), []byte(header))
}

// SignWithTimestamp returns a `t=<unix>,v1=<hex-hmac>` signature header
// where the HMAC covers `<unix>.<body>`. Binding the timestamp into the
// signed material lets receivers reject replays via VerifyTimestamped.
//
// Pass time.Now().Unix() for fresh signatures; tests may pin to a
// specific instant.
func SignWithTimestamp(secret string, unixTimestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(unixTimestamp, 10)))
	mac.Write([]byte{'.'})
	mac.Write(body)
	return fmt.Sprintf("t=%d,v1=%s", unixTimestamp, hex.EncodeToString(mac.Sum(nil)))
}

// VerifyTimestamped accepts a header in the `t=<unix>,v1=<hex>` form
// and a tolerance window. Returns true iff:
//
//   - the header parses cleanly
//   - |now - ts| <= tolerance
//   - the HMAC over <ts>.<body> equals v1
//
// `now` is captured at call time so receivers don't need a clock arg.
// `tolerance` of 5 minutes is a reasonable default.
//
// An empty secret always rejects.
func VerifyTimestamped(secret, header string, body []byte, tolerance time.Duration) bool {
	if secret == "" || header == "" {
		return false
	}
	ts, sig, ok := parseTimestampedHeader(header)
	if !ok {
		return false
	}
	if tolerance > 0 {
		drift := time.Since(time.Unix(ts, 0))
		if drift < 0 {
			drift = -drift
		}
		if drift > tolerance {
			return false
		}
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(ts, 10)))
	mac.Write([]byte{'.'})
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(sig))
}

func parseTimestampedHeader(header string) (int64, string, bool) {
	// Expected form: `t=<unix>,v1=<hex>`. Extra fields are tolerated
	// — only `t=` and `v1=` are required, in any order, comma-separated.
	var tsRaw, sig string
	for _, part := range strings.Split(header, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			tsRaw = kv[1]
		case "v1":
			sig = kv[1]
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
