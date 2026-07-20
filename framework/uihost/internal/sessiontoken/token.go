// Package sessiontoken mints and verifies the stateless HMAC-signed
// tokens that replace the uihost's in-memory session map. A token is
//
//	sess-<128-bit random>.<unix-seconds>.<base64url HMAC-SHA256>
//
// and carries no state beyond its own id and mint time, so any replica
// holding the same key accepts a token minted by any other — the
// multi-replica session contract (issue #112). The id portion
// ("sess-…") doubles as the SSE stream / presence key, exactly like the
// map-era session ids did.
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

// macContext domain-separates the session MAC from any other use of the
// same key material. Versioned so a future format change can coexist
// with old tokens during a rollout.
const macContext = "gofastr-uihost-session-v1"

// clockSkew is how far in the future a token's mint time may sit before
// verification rejects it — tolerance for replicas with drifting clocks,
// not a feature.
const clockSkew = 2 * time.Minute

// maxTokenLen bounds what Verify will even look at; the longest
// legitimate token is well under this.
const maxTokenLen = 256

// minKeyLen rejects keys too short to be a real secret. Derived keys
// (HKDF-SHA256) are 32 bytes; anything under 16 is a misconfiguration.
const minKeyLen = 16

// Mint returns a signed token and the bare session id embedded in it.
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

// Verify authenticates token and returns its session id. maxAge bounds
// how old a token may be; tokens minted more than clockSkew in the
// future are rejected. Constant-time on the MAC comparison.
func Verify(key []byte, token string, now time.Time, maxAge time.Duration) (id string, ok bool) {
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

func sign(key []byte, id, created string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(macContext))
	mac.Write([]byte{0})
	mac.Write([]byte(id))
	mac.Write([]byte{0})
	mac.Write([]byte(created))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
