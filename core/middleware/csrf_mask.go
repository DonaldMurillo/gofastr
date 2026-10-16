package middleware

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
)

// maskedPrefix marks a masked CSRF token. A raw token starts with the
// 43-character base64url encoding of its random half, so it can never
// begin with this.
const maskedPrefix = "m1."

// MaskCSRFToken returns token under a fresh one-time pad:
// "m1." + base64url(pad || pad XOR token). Render this, never the raw
// token, into any response body. The bytes differ on every call, so a
// compressed page carrying the token gives a BREACH-style attacker
// nothing to converge on; CSRF accepts the masked form wherever it
// accepts the raw one. Returns "" for an empty token.
func MaskCSRFToken(token string) string {
	if token == "" {
		return ""
	}
	n := len(token)
	buf := make([]byte, 2*n)
	if _, err := rand.Read(buf[:n]); err != nil {
		// crypto/rand does not fail on supported platforms; refuse to
		// render a predictable pad if it ever does.
		panic("csrf: masking token: " + err.Error())
	}
	for i := range n {
		buf[n+i] = buf[i] ^ token[i]
	}
	return maskedPrefix + base64.RawURLEncoding.EncodeToString(buf)
}

// unmaskCSRFToken reverses MaskCSRFToken. A value without the masked
// prefix is returned as is (a raw token); a malformed masked value
// returns "", which never matches a cookie.
func unmaskCSRFToken(v string) string {
	rest, ok := strings.CutPrefix(v, maskedPrefix)
	if !ok {
		return v
	}
	buf, err := base64.RawURLEncoding.DecodeString(rest)
	if err != nil || len(buf) == 0 || len(buf)%2 != 0 {
		return ""
	}
	n := len(buf) / 2
	out := make([]byte, n)
	for i := range n {
		out[i] = buf[i] ^ buf[n+i]
	}
	return string(out)
}
