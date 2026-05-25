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

// DefaultTimestampTolerance is the suggested replay window for
// VerifyTimestamped — wide enough to cover modest clock skew, narrow
// enough that a captured signature decays quickly.
const DefaultTimestampTolerance = 5 * time.Minute

// VerifyTimestamped accepts a header in the `t=<unix>,v1=<hex>` form
// and a tolerance window. Returns true iff:
//
//   - the header parses cleanly
//   - |now - ts| <= tolerance
//   - the HMAC over <ts>.<body> equals v1
//
// `now` is captured at call time so receivers don't need a clock arg.
//
// tolerance MUST be positive. A non-positive tolerance is treated as a
// usage error and always rejects — otherwise a caller that accidentally
// passed 0 (zero value of a forgotten config field) would silently
// disable the replay check. Use [DefaultTimestampTolerance] for the
// suggested default.
//
// An empty secret always rejects.
func VerifyTimestamped(secret, header string, body []byte, tolerance time.Duration) bool {
	if secret == "" || header == "" {
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
