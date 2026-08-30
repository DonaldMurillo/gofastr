package webbotauth

// jwks.go: JWK Set parsing and the RFC 7638 / RFC 8037 A.3 thumbprint.
//
// Web Bot Auth's keyid is the base64url, unpadded SHA-256 thumbprint of
// the JWK. Selection is always by computed thumbprint, never by the
// directory's kid label: a conformant well-known directory sets kid to
// the same thumbprint anyway, and a jwks_uri directory may carry an
// operator-chosen kid that means nothing to a verifier. Trusting key
// material over labelling is the conservative direction.

import (
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

// jwk is one parsed OKP Ed25519 verification key with its thumbprint
// and optional time bounds.
type jwk struct {
	thumbprint string
	kid        string
	nbf, exp   *int64
	pub        ed25519.PublicKey
}

// jwkSet is a parsed directory response along with the freshness the
// response asked for (used only on the fetch path).
type jwkSet struct {
	keys []jwk
	ttl  time.Duration
}

// validAt reports whether the key's optional nbf/exp bounds admit t.
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

// parseJWKS parses a JSON Web Key Set body and keeps the Ed25519 OKP
// keys. Non-Ed25519 keys are skipped, not rejected: the set is foreign
// input and other consumers may use the rest of it. A body that is not
// a JSON object with a keys array is malformed and fails outright.
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

// parseJWK parses one JWK. It returns (nil, nil) for key types this
// verifier does not handle and an error for a key that claims to be
// OKP/Ed25519 but is malformed.
func parseJWK(m map[string]json.RawMessage) (*jwk, error) {
	var kty, crv, x, kid string
	_ = json.Unmarshal(m["kty"], &kty)
	_ = json.Unmarshal(m["crv"], &crv)
	_ = json.Unmarshal(m["kid"], &kid)
	if kty != "OKP" {
		return nil, nil
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
	var use, alg string
	_ = json.Unmarshal(m["use"], &use)
	_ = json.Unmarshal(m["alg"], &alg)
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

// selectKey returns the key whose thumbprint equals keyid and whose
// time bounds admit now, per the (URL, key) lookup rule of draft
// section 5.4: the URL half is the cache entry this set came from.
func (s *jwkSet) selectKey(keyid string, now time.Time) *jwk {
	for i := range s.keys {
		if s.keys[i].thumbprint == keyid && s.keys[i].validAt(now) {
			return &s.keys[i]
		}
	}
	return nil
}

// okpThumbprint computes the RFC 8037 Appendix A.3 thumbprint: the
// SHA-256 of the canonical JSON containing exactly crv, kty, x in
// lexicographic order, base64url-encoded without padding.
func okpThumbprint(crv, kty, x string) string {
	canonical := fmt.Sprintf(`{"crv":%s,"kty":%s,"x":%s}`, quoteJSON(crv), quoteJSON(kty), quoteJSON(x))
	sum := sha256.Sum256([]byte(canonical))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// quoteJSON renders s as a JSON string. The thumbprint inputs are
// ASCII (curve name, "OKP", base64url) so no escaping can occur; the
// json package is used anyway so a non-ASCII input fails loudly
// instead of corrupting the digest.
func quoteJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic("webbotauth: thumbprint input not encodable: " + err.Error())
	}
	return string(b)
}
