package variants

import (
	"bytes"
	"net/http"
	"strings"
)

type store struct {
	token string
	sig   []byte
}

func bytesEqual(sig, expectedSig []byte) bool {
	return bytes.Equal(sig, expectedSig) // want `sig compared`
}

func conv(sig []byte, expectedSig string) bool {
	return string(sig) == expectedSig // want `compared`
}

func paren(token string, s *store) bool {
	return (token) == s.token // want `compared`
}

func sw(token string, s *store) bool {
	switch token {
	case s.token: // want `compared`
		return true
	}
	return false
}

func cmp(token string, s *store) bool {
	return strings.Compare(token, s.token) == 0 // want `compared`
}

func hdr(r *http.Request, s *store) bool {
	return r.Header.Get("X-Token") == s.token // want `compared`
}

func generic[T ~string](token, storedToken T) bool {
	return token == storedToken // want `compared`
}

// Silent: the counterparts the widening must not catch.

func bytesEqualDigest(body, other []byte) bool {
	return bytes.Equal(body, other)
}

func switchConst(token string) bool {
	switch token {
	case "", "none":
		return false
	}
	return true
}

func lenConv(token, storedToken string) bool {
	return len(token) == len(storedToken)
}

func genericInt[T ~int](pin, storedPin T) bool {
	return pin == storedPin
}
