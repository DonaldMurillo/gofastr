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


const (
	jwksMaxBody      = 1 << 20 // 1 MiB cap on a JWKS response
	forcedRefetchGap = 5 * time.Minute
)

type oidcKey struct {
	kid string
	alg string // "RS256" or "ES256"
	rsa *rsa.PublicKey
	ec  *ecdsa.PublicKey
}

type jwksSet struct {
	byKid map[string]oidcKey
	list  []oidcKey // every usable key (for kid-less lookup)
}

func (s *jwksSet) lookup(kid string) (oidcKey, bool) {
	if kid != "" {
		k, ok := s.byKid[kid]
		return k, ok
	}
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
	retryAt time.Time
	gen     uint64
	lastErr error
	fetchSlot chan struct{}
	forcedByKid map[string]time.Time

	now     func() time.Time
	waiting func()
}

func (c *jwksCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *jwksCache) acquireSlot(ctx context.Context, slot chan struct{}, wait bool) (bool, error) {
	select {
	case slot <- struct{}{}:
		return true, nil
	default:
	}
	if !wait {
		return false, nil
	}
	if c.waiting != nil {
		c.waiting()
	}
	select {
	case slot <- struct{}{}:
		return true, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

const maxForcedKids = 128

const jwksRetryBackoff = time.Minute

var errJWKSFetchBusy = errors.New("oidc: jwks fetch already in flight")

func (c *jwksCache) getKey(ctx context.Context, jwksURI, kid string) (oidcKey, error) {
	now := c.clock()
	c.mu.Lock()
	set := c.set
	stale := set == nil || now.Sub(c.fetchedAt) > c.ttl
	backingOff := now.Before(c.retryAt)
	c.mu.Unlock()

	switch {
	case set == nil:
		if err := c.refresh(ctx, jwksURI, true, true); err != nil {
			return oidcKey{}, err
		}
	case stale && !backingOff:
		_ = c.refresh(ctx, jwksURI, false, true)
	}
	c.mu.Lock()
	set = c.set
	c.mu.Unlock()
	if set == nil {
		return oidcKey{}, errors.New("oidc: jwks unavailable")
	}
	if k, ok := set.lookup(kid); ok {
		return k, nil
	}
	c.mu.Lock()
	last, seen := c.forcedByKid[kid]
	force := (!seen || now.Sub(last) >= forcedRefetchGap) && !now.Before(c.retryAt)
	if force {
		if c.forcedByKid == nil || len(c.forcedByKid) >= maxForcedKids {
			c.forcedByKid = make(map[string]time.Time, 8)
		}
		c.forcedByKid[kid] = now
	}
	c.mu.Unlock()
	if force {
		if err := c.refresh(ctx, jwksURI, true, false); err != nil {
			return oidcKey{}, fmt.Errorf("oidc: no signing key found for kid %q", kid)
		}
	} else if c.awaitInFlight(ctx) != nil {
		return oidcKey{}, fmt.Errorf("oidc: no signing key found for kid %q", kid)
	}
	c.mu.Lock()
	set = c.set
	c.mu.Unlock()
	if k, ok := set.lookup(kid); ok {
		return k, nil
	}
	return oidcKey{}, fmt.Errorf("oidc: no signing key found for kid %q", kid)
}

func (c *jwksCache) awaitInFlight(ctx context.Context) error {
	c.mu.Lock()
	if c.fetchSlot == nil {
		c.fetchSlot = make(chan struct{}, 1)
	}
	slot := c.fetchSlot
	c.mu.Unlock()
	if _, err := c.acquireSlot(ctx, slot, true); err != nil {
		return err
	}
	<-slot
	return nil
}

func (c *jwksCache) refresh(ctx context.Context, jwksURI string, wait, reuse bool) error {
	c.mu.Lock()
	if c.fetchSlot == nil {
		c.fetchSlot = make(chan struct{}, 1)
	}
	slot := c.fetchSlot
	gen := c.gen
	c.mu.Unlock()

	held, err := c.acquireSlot(ctx, slot, wait)
	if err != nil {
		return err
	}
	if !held {
		return errJWKSFetchBusy
	}
	defer func() { <-slot }()

	c.mu.Lock()
	if reuse && c.gen != gen {
		err := c.lastErr
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()

	set, err := c.fetch(ctx, jwksURI)

	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return err
		}
		c.gen++
		c.lastErr = err
		c.retryAt = c.clock().Add(jwksRetryBackoff)
		return err
	}
	c.gen++
	c.lastErr = nil
	c.set = set
	c.fetchedAt = c.clock()
	c.retryAt = time.Time{}
	return nil
}

func (c *jwksCache) fetch(ctx context.Context, jwksURI string) (*jwksSet, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc: jwks returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, jwksMaxBody))
	if err != nil {
		return nil, err
	}
	return parseJWKS(body)
}

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
	if n.BitLen() < 2048 {
		return nil, fmt.Errorf("oidc: rsa modulus too weak (%d bits)", n.BitLen())
	}
	if n.BitLen() > maxCOSERSABits {
		return nil, fmt.Errorf("oidc: rsa modulus too large (%d bits)", n.BitLen())
	}
	e := new(big.Int).SetBytes(eBytes)
	if !e.IsUint64() || e.BitLen() > 31 {
		return nil, errors.New("oidc: rsa exponent out of range")
	}
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
	if !curve.IsOnCurve(x, y) {
		return nil, errors.New("oidc: ec point not on P-256")
	}
	return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
}


func (p *OIDCProvider) verifyIDToken(ctx context.Context, idToken, jwksURI string) (map[string]any, error) {
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
	if hdr.Alg != "RS256" && hdr.Alg != "ES256" {
		return nil, fmt.Errorf("oidc: id_token alg %q not allowed", hdr.Alg)
	}

	key, err := p.jwks.getKey(ctx, jwksURI, hdr.Kid)
	if err != nil {
		return nil, err
	}
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
		if err := rsa.VerifyPKCS1v15(key.rsa, crypto.SHA256, sum[:], sig); err != nil {
			return nil, errors.New("oidc: id_token signature verification failed")
		}
	case "ES256":
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
		return nil, fmt.Errorf("oidc: unsupported id_token alg %q", hdr.Alg)
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("oidc: malformed id_token payload")
	}
	var claims map[string]any
	//gofastr:allow(floatnum) claims are read as strings, plus exp/nbf/iat times far below 2^53
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, errors.New("oidc: malformed id_token payload")
	}
	if err := p.verifyClaims(claims); err != nil {
		return nil, err
	}
	return claims, nil
}
