package middleware

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
)

const maskedPrefix = "m1."

// MaskCSRFToken returns token under a fresh one-time pad, so its bytes differ on every call; render this, never the raw token. Empty stays empty.
func MaskCSRFToken(token string) string {
	if token == "" {
		return ""
	}
	n := len(token)
	buf := make([]byte, 2*n)
	if _, err := rand.Read(buf[:n]); err != nil {
		panic("csrf: masking token: " + err.Error())
	}
	for i := range n {
		buf[n+i] = buf[i] ^ token[i]
	}
	return maskedPrefix + base64.RawURLEncoding.EncodeToString(buf)
}

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
