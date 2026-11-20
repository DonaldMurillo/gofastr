package webbotauth

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

const (
	maxDirectoryKeys = 32 // draft section 6.7: bound the O(n) key search
)

type jwk struct {
	thumbprint string
	kid        string
	nbf, exp   *int64
	pub        ed25519.PublicKey
}

type jwkSet struct {
	keys []jwk
	ttl  time.Duration
}

func (k *jwk) validAt(t time.Time) bool {
	unix := t.Unix()
	if k.nbf != nil && unix < *k.nbf {
		return false
	}
	if k.exp != nil && unix >= *k.exp {
		return false
	}
	return true
}

func parseJWKS(body []byte) (*jwkSet, error) {
	var raw struct {
		Keys []map[string]json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("jwks: %w", err)
	}
	if raw.Keys == nil {
		return nil, fmt.Errorf("jwks: no keys array")
	}
	if len(raw.Keys) > maxDirectoryKeys {
		return nil, fmt.Errorf("jwks: %d keys exceeds the cap of %d", len(raw.Keys), maxDirectoryKeys)
	}
	set := &jwkSet{}
	for i, m := range raw.Keys {
		k, err := parseJWK(m)
		if err != nil {
			return nil, fmt.Errorf("jwks key %d: %w", i, err)
		}
		if k == nil {
			continue // unsupported type: skip
		}
		set.keys = append(set.keys, *k)
	}
	return set, nil
}

func jwkString(m map[string]json.RawMessage, name string) (string, error) {
	raw, ok := m[name]
	if !ok {
		return "", nil
	}
	var s string
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &s) != nil {
		return "", fmt.Errorf("%s is not a string", name)
	}
	return s, nil
}

func parseJWK(m map[string]json.RawMessage) (*jwk, error) {
	var kty, crv, x string
	_ = json.Unmarshal(m["kty"], &kty)
	_ = json.Unmarshal(m["crv"], &crv)
	if kty != "OKP" {
		return nil, nil
	}
	kid, err := jwkString(m, "kid")
	if err != nil {
		return nil, err
	}
	if crv != "Ed25519" {
		return nil, fmt.Errorf("unsupported OKP curve %q", crv)
	}
	if err := json.Unmarshal(m["x"], &x); err != nil || x == "" {
		return nil, fmt.Errorf("missing x")
	}
	raw, err := base64.RawURLEncoding.DecodeString(x)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("x is not a 32-byte base64url value")
	}
	use, err := jwkString(m, "use")
	if err != nil {
		return nil, err
	}
	alg, err := jwkString(m, "alg")
	if err != nil {
		return nil, err
	}
	if use != "" && use != "sig" {
		return nil, nil
	}
	if alg != "" && alg != "EdDSA" {
		return nil, nil
	}
	k := &jwk{kid: kid, pub: ed25519.PublicKey(raw)}
	k.thumbprint = okpThumbprint(crv, kty, x)
	var nbf, exp int64
	if m["nbf"] != nil {
		if err := json.Unmarshal(m["nbf"], &nbf); err != nil {
			return nil, fmt.Errorf("nbf: %w", err)
		}
		k.nbf = &nbf
	}
	if m["exp"] != nil {
		if err := json.Unmarshal(m["exp"], &exp); err != nil {
			return nil, fmt.Errorf("exp: %w", err)
		}
		k.exp = &exp
	}
	return k, nil
}

func (s *jwkSet) selectKey(keyid string, now time.Time) *jwk {
	for i := range s.keys {
		if s.keys[i].thumbprint == keyid && s.keys[i].validAt(now) {
			return &s.keys[i]
		}
	}
	return nil
}

func okpThumbprint(crv, kty, x string) string {
	canonical := fmt.Sprintf(`{"crv":%s,"kty":%s,"x":%s}`, quoteJSON(crv), quoteJSON(kty), quoteJSON(x))
	sum := sha256.Sum256([]byte(canonical))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func quoteJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic("webbotauth: thumbprint input not encodable: " + err.Error())
	}
	return string(b)
}
