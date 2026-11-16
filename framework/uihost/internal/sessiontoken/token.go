// Package sessiontoken mints and verifies stateless session tokens of the
// form sess-<random>.<unix-seconds>.<base64url HMAC-SHA256>.
package sessiontoken

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

const macContext = "gofastr-uihost-session-v1"

const clockSkew = 2 * time.Minute

const maxTokenLen = 256

const minKeyLen = 16

// Mint returns a signed token and the bare session id embedded in it. It
// errors when key is shorter than 16 bytes.
func Mint(key []byte, now time.Time) (token, id string, err error) {
	if len(key) < minKeyLen {
		return "", "", errors.New("sessiontoken: key must be at least 16 bytes")
	}
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", "", err
	}
	id = "sess-" + base64.RawURLEncoding.EncodeToString(buf[:])
	created := strconv.FormatInt(now.Unix(), 10)
	token = id + "." + created + "." + sign(key, id, created)
	return token, id, nil
}

// Verify authenticates token under key and returns its session id; a token
// older than maxAge or minted in the future is refused.
func Verify(key []byte, token string, now time.Time, maxAge time.Duration) (id string, ok bool) {
	return VerifyAny(key, nil, token, now, maxAge)
}

// VerifyAny is Verify accepting the current key or any previous key.
func VerifyAny(current []byte, previous [][]byte, token string, now time.Time, maxAge time.Duration) (id string, ok bool) {
	if id, ok := verifyOne(current, token, now, maxAge); ok {
		return id, true
	}
	for _, k := range previous {
		if id, ok := verifyOne(k, token, now, maxAge); ok {
			return id, true
		}
	}
	return "", false
}

func verifyOne(key []byte, token string, now time.Time, maxAge time.Duration) (id string, ok bool) {
	if len(key) < minKeyLen || len(token) > maxTokenLen {
		return "", false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", false
	}
	id, created, mac := parts[0], parts[1], parts[2]
	if !strings.HasPrefix(id, "sess-") {
		return "", false
	}
	createdUnix, err := strconv.ParseInt(created, 10, 64)
	if err != nil {
		return "", false
	}
	if !hmac.Equal([]byte(mac), []byte(sign(key, id, created))) {
		return "", false
	}
	mintedAt := time.Unix(createdUnix, 0)
	if mintedAt.After(now.Add(clockSkew)) {
		return "", false
	}
	if now.Sub(mintedAt) > maxAge {
		return "", false
	}
	return id, true
}

// MintedAt returns the signing time a token carries. It authenticates
// nothing: call it only on a token Verify accepted.
func MintedAt(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	createdUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(createdUnix, 0), true
}

func sign(key []byte, id, created string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(macContext))
	mac.Write([]byte{0})
	mac.Write([]byte(id))
	mac.Write([]byte{0})
	mac.Write([]byte(created))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
