package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ─── JWKS fetch/cache + id_token verification ────────────────────────────────
//
// Hand-rolled (stdlib-only, per repo policy): JWK parsing, JWKS caching, and
// compact-JWT signature verification for RS256/ES256. This is the crypto core
// of the OIDC provider — see oidc.go for the provider/discovery plumbing.

const (
	jwksMaxBody      = 1 << 20 // 1 MiB cap on a JWKS response
	forcedRefetchGap = 5 * time.Minute
)

// oidcKey is a parsed, usable JWK signing key. Exactly one of rsa/ec is set;
// alg records which signature family it belongs to (derived from kty/crv).
type oidcKey struct {
	kid string
	alg string // "RS256" or "ES256"
	rsa *rsa.PublicKey
	ec  *ecdsa.PublicKey
}

// jwksSet is a fetched-and-parsed key set.
type jwksSet struct {
	byKid map[string]oidcKey
	list  []oidcKey // every usable key (for kid-less lookup)
}

func (s *jwksSet) lookup(kid string) (oidcKey, bool) {
	if kid != "" {
		k, ok := s.byKid[kid]
		return k, ok
	}
	// No kid in the header: only unambiguous when the set has exactly one key.
	if len(s.list) == 1 {
		return s.list[0], true
	}
	return oidcKey{}, false
}

type jwksCache struct {
	httpClient *http.Client
	ttl        time.Duration

	mu        sync.Mutex
	set       *jwksSet
	fetchedAt time.Time
	// forcedByKid rate-limits rotation refetches PER kid. A single global
	// timestamp would let one unknown kid's spent slot block every other
	// kid's refetch for the whole window — so a token signed by a freshly
	// rotated key would be rejected for minutes even though a refetch would
	// resolve it. Keyed per kid, each unknown kid gets its own slot.
	forcedByKid map[string]time.Time
}

// maxForcedKids bounds forcedByKid. kids come only from IdP-issued tokens
// (getKey is reachable solely via ExchangeCode), so the real key set is
// small; the cap is a belt-and-suspenders guard against unbounded growth.
const maxForcedKids = 128

// getKey resolves the signing key for kid, fetching/refreshing the JWKS as
// needed. A forced refetch on an unknown kid is rate-limited to once per
// forcedRefetchGap so a flood of unknown-kid tokens cannot hammer the IdP.
func (c *jwksCache) getKey(ctx context.Context, jwksURI, kid string) (oidcKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if c.set == nil || now.Sub(c.fetchedAt) > c.ttl {
		if err := c.fetchLocked(ctx, jwksURI); err != nil {
			// Fall back to a stale set if we have one; otherwise fail.
			if c.set == nil {
				return oidcKey{}, err
			}
		}
	}
	if k, ok := c.set.lookup(kid); ok {
		return k, nil
	}
	// Unknown kid: a key rotation may have just published it. Force one
	// refetch, rate-limited PER kid so repeated attempts for one kid don't
	// amplify to the IdP yet a different (legitimately rotated) kid is never
	// blocked by another kid's spent slot.
	if last, seen := c.forcedByKid[kid]; !seen || now.Sub(last) >= forcedRefetchGap {
		if c.forcedByKid == nil || len(c.forcedByKid) >= maxForcedKids {
			// Reset rather than grow without bound. kids aren't attacker-
			// controlled, so this only ever trips under extreme rotation.
			c.forcedByKid = make(map[string]time.Time, 8)
		}
		c.forcedByKid[kid] = now
		if err := c.fetchLocked(ctx, jwksURI); err == nil {
			if k, ok := c.set.lookup(kid); ok {
				return k, nil
			}
		}
	}
	return oidcKey{}, fmt.Errorf("oidc: no signing key found for kid %q", kid)
}

func (c *jwksCache) fetchLocked(ctx context.Context, jwksURI string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("oidc: jwks returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, jwksMaxBody))
	if err != nil {
		return err
	}
	set, err := parseJWKS(body)
	if err != nil {
		return err
	}
	c.set = set
	c.fetchedAt = time.Now()
	return nil
}

// rawJWK is the JSON shape of a single JWK.
type rawJWK struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// parseJWKS parses a JWKS document. Unparsable keys are skipped — an IdP may
// advertise keys for algorithms we don't use; it is only an error if NO usable
// key remains.
func parseJWKS(body []byte) (*jwksSet, error) {
	var doc struct {
		Keys []rawJWK `json:"keys"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	set := &jwksSet{byKid: map[string]oidcKey{}}
	for _, k := range doc.Keys {
		pk, ok := parseJWK(k)
		if !ok {
			continue
		}
		set.list = append(set.list, pk)
		if k.Kid != "" {
			set.byKid[k.Kid] = pk
		}
	}
	if len(set.list) == 0 {
		return nil, errors.New("oidc: jwks has no usable signing keys")
	}
	return set, nil
}

// parseJWK parses one JWK into an oidcKey. ok=false means skip it.
//
// Keys are skipped unless their own metadata permits signature use: a
// `use` of anything but "sig" (an encryption key must never verify
// signatures, even if it could), and a declared `alg` other than the
// family algorithm we verify with (a PS256/RS512 key must not silently
// verify RS256 — the key's own binding wins).
func parseJWK(k rawJWK) (oidcKey, bool) {
	if k.Use != "" && k.Use != "sig" {
		return oidcKey{}, false
	}
	switch k.Kty {
	case "RSA":
		if k.Alg != "" && k.Alg != "RS256" {
			return oidcKey{}, false
		}
		pk, err := buildRSAKey(k.N, k.E)
		if err != nil {
			return oidcKey{}, false
		}
		return oidcKey{kid: k.Kid, alg: "RS256", rsa: pk}, true
	case "EC":
		if k.Alg != "" && k.Alg != "ES256" {
			return oidcKey{}, false
		}
		pk, err := buildECKey(k.Crv, k.X, k.Y)
		if err != nil {
			return oidcKey{}, false
		}
		return oidcKey{kid: k.Kid, alg: "ES256", ec: pk}, true
	}
	return oidcKey{}, false
}

func buildRSAKey(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, err
	}
	n := new(big.Int).SetBytes(nBytes)
	// Reject sub-2048-bit moduli as weak (NIST SP 800-131A). The brief gives
	// the "<256 bytes" heuristic; BitLen()<2048 is the precise equivalent and
	// also catches 1024-bit keys with a leading zero byte.
	if n.BitLen() < 2048 {
		return nil, fmt.Errorf("oidc: rsa modulus too weak (%d bits)", n.BitLen())
	}
	e := new(big.Int).SetBytes(eBytes)
	if !e.IsUint64() || e.BitLen() > 31 {
		return nil, errors.New("oidc: rsa exponent out of range")
	}
	// A valid RSA public exponent is odd and >= 3. e=1 in particular makes
	// PKCS1v15 "signatures" trivially forgeable (sig^1 mod n == sig).
	if e.Int64() < 3 || e.Bit(0) == 0 {
		return nil, errors.New("oidc: degenerate rsa exponent")
	}
	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}

func buildECKey(crv, xB64, yB64 string) (*ecdsa.PublicKey, error) {
	if crv != "P-256" {
		return nil, fmt.Errorf("oidc: unsupported ec crv %q", crv)
	}
	xBytes, err := base64.RawURLEncoding.DecodeString(xB64)
	if err != nil {
		return nil, err
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(yB64)
	if err != nil {
		return nil, err
	}
	x := new(big.Int).SetBytes(xBytes)
	y := new(big.Int).SetBytes(yBytes)
	curve := elliptic.P256()
	// Validate the point is actually on the curve — a crafted off-curve point
	// could otherwise let an attacker pick a weak subgroup.
	if !curve.IsOnCurve(x, y) {
		return nil, errors.New("oidc: ec point not on P-256")
	}
	return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
}

// ─── id_token verification ───────────────────────────────────────────────────

// verifyIDToken verifies a compact OIDC id_token's signature against the IdP's
// JWKS and then its claims, returning the parsed claims on success.
//
// Order matters — all of this runs BEFORE the claims are trusted:
//  1. exactly three dot-separated parts, base64url-decodable header/payload/sig;
//  2. alg pinned to RS256 or ES256 (rejects "none", HS256, case variants);
//  3. key resolved from JWKS by kid (with one rate-limited rotation refetch);
//  4. JWK kty/crv matches the alg (no RSA-vs-EC confusion);
//  5. signature verified with crypto/rsa (PKCS1v15) or crypto/ecdsa (raw R||S);
//  6. iss/aud/exp/iat/sub claims validated.
func (p *OIDCProvider) verifyIDToken(ctx context.Context, idToken, jwksURI string) (map[string]interface{}, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, errors.New("oidc: malformed id_token")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("oidc: malformed id_token header")
	}
	var hdr struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerBytes, &hdr); err != nil {
		return nil, errors.New("oidc: malformed id_token header")
	}
	// Algorithm allowlist — exact match. Rejects "none", HS256 and case
	// variants BEFORE any key lookup, closing the classic alg-confusion attack
	// where an HMAC token is "verified" against an RSA key's public bytes.
	if hdr.Alg != "RS256" && hdr.Alg != "ES256" {
		return nil, fmt.Errorf("oidc: id_token alg %q not allowed", hdr.Alg)
	}

	key, err := p.jwks.getKey(ctx, jwksURI, hdr.Kid)
	if err != nil {
		return nil, err
	}
	// kty/crv must match the alg: never verify an RSA token with an EC key or
	// vice versa.
	if hdr.Alg == "RS256" && key.rsa == nil {
		return nil, errors.New("oidc: alg/key type mismatch (RS256 token, non-RSA key)")
	}
	if hdr.Alg == "ES256" && key.ec == nil {
		return nil, errors.New("oidc: alg/key type mismatch (ES256 token, non-EC key)")
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, errors.New("oidc: malformed id_token signature")
	}
	signingInput := []byte(parts[0] + "." + parts[1])

	switch hdr.Alg {
	case "RS256":
		sum := sha256.Sum256(signingInput)
		// rsa.VerifyPKCS1v15 is constant-time over the signature.
		if err := rsa.VerifyPKCS1v15(key.rsa, crypto.SHA256, sum[:], sig); err != nil {
			return nil, errors.New("oidc: id_token signature verification failed")
		}
	case "ES256":
		// JOSE encodes EC signatures as raw R||S, each padded to the curve
		// order size (32 bytes for P-256) — NOT ASN.1/DER. Reject anything that
		// is not exactly 64 bytes.
		if len(sig) != 64 {
			return nil, fmt.Errorf("oidc: es256 signature must be 64 raw bytes, got %d", len(sig))
		}
		r := new(big.Int).SetBytes(sig[:32])
		s := new(big.Int).SetBytes(sig[32:])
		sum := sha256.Sum256(signingInput)
		if !ecdsa.Verify(key.ec, sum[:], r, s) {
			return nil, errors.New("oidc: id_token signature verification failed")
		}
	default:
		// Unreachable while the alg allowlist upstream admits only RS256/
		// ES256, but making the switch fail closed here means no future
		// refactor of that allowlist can leave a token unverified.
		return nil, fmt.Errorf("oidc: unsupported id_token alg %q", hdr.Alg)
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("oidc: malformed id_token payload")
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, errors.New("oidc: malformed id_token payload")
	}
	if err := p.verifyClaims(claims); err != nil {
		return nil, err
	}
	return claims, nil
}
