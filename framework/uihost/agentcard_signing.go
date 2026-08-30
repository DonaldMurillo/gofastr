package uihost

// agentcard_signing.go: A2A v1.0 Agent Card signatures (spec §8.4).
// When AgentCardConfig.SigningKeys is non-empty, the card is served with
// a `signatures` array of AgentCardSignature objects — a JWS (RFC 7515)
// whose payload is the RFC 8785 (JCS) canonical form of the card with
// the `signatures` field excluded — plus a JWKS endpoint publishing the
// verification keys at /.well-known/jwks.json.
//
// Construction, per A2A §8.4.2:
//
//	payload   = JCS(card without `signatures`)
//	protected = base64url(JSON({alg, typ:"JOSE", kid, jku}))
//	signature = base64url(SIGN(ASCII(protected "." base64url(payload))))
//
// The signature object carries `protected` and `signature` (the JWS
// flattened-JSON fields the A2A AgentCardSignature proto defines);
// the payload itself is not embedded — verifiers rebuild it from the
// card, which is what makes this a detached JWS.
//
// Stdlib-only by repo policy: no JOSE dependency. ES256/ES384/ES512
// signatures are converted from Go's ASN.1 DER to the JOSE r||s form.

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/jcs"
)

// AgentCardSigningKey is one key that signs /.well-known/agent-card.json.
// Configure several to support rotation: each key adds one entry to the
// card's `signatures` array and one JWK to the JWKS.
type AgentCardSigningKey struct {
	// KeyID becomes the JWS `kid` header parameter and the JWKS `kid`.
	// When empty it defaults to the RFC 7638 SHA-256 thumbprint of the
	// public key, so the same key always yields the same kid.
	KeyID string

	// Signer holds the private key. Supported: *ecdsa.PrivateKey on
	// P-256 (ES256), P-384 (ES384), or P-521 (ES512), and
	// ed25519.PrivateKey (EdDSA). Other key types fail at mount.
	Signer crypto.Signer
}

// agentCardSigning is the mount-time-resolved signing state: keys
// validated, algorithms pinned, kids computed, JWKS URL absolute.
type agentCardSigning struct {
	keys    []resolvedSigningKey
	jwksURL string // absolute /.well-known/jwks.json URL for the `jku` header
}

type resolvedSigningKey struct {
	alg    string
	kid    string
	signer crypto.Signer
	jwk    map[string]any // public JWK members only — never private material
}

// jwksPath is where the verification keys are published. A2A v1.0 does
// not name a path (its examples use a jwks.json URL of the agent's
// choosing); /.well-known/jwks.json follows the established convention.
const jwksPath = "/.well-known/jwks.json"

// resolveSigningKeys validates the configured keys and resolves their
// algorithms, kids, and public JWKs. Any unsupported key, nil signer, or
// zero key is an error — mount turns it into a boot failure rather than
// serving a card signed with a guessed algorithm.
func resolveSigningKeys(keys []AgentCardSigningKey) ([]resolvedSigningKey, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	out := make([]resolvedSigningKey, 0, len(keys))
	seen := map[string]bool{}
	for i, k := range keys {
		if k.Signer == nil {
			return nil, fmt.Errorf("uihost: agent card SigningKeys[%d]: Signer is nil", i)
		}
		rk, err := resolveSigningKey(k)
		if err != nil {
			return nil, fmt.Errorf("uihost: agent card SigningKeys[%d]: %w", i, err)
		}
		if seen[rk.kid] {
			return nil, fmt.Errorf("uihost: agent card SigningKeys[%d]: duplicate kid %q", i, rk.kid)
		}
		seen[rk.kid] = true
		out = append(out, rk)
	}
	return out, nil
}

func resolveSigningKey(k AgentCardSigningKey) (resolvedSigningKey, error) {
	alg, crv, size, err := algorithmFor(k.Signer.Public())
	if err != nil {
		return resolvedSigningKey{}, err
	}
	var jwk map[string]any
	switch pub := k.Signer.Public().(type) {
	case *ecdsa.PublicKey:
		if pub.X == nil || pub.Y == nil {
			return resolvedSigningKey{}, fmt.Errorf("EC key %q has no public coordinates", k.KeyID)
		}
		xb := make([]byte, size)
		yb := make([]byte, size)
		pub.X.FillBytes(xb)
		pub.Y.FillBytes(yb)
		jwk = map[string]any{
			"kty": "EC", "crv": crv,
			"x": base64URL(xb), "y": base64URL(yb),
		}
	case ed25519.PublicKey:
		jwk = map[string]any{
			"kty": "OKP", "crv": crv,
			"x": base64URL(pub),
		}
	default:
		return resolvedSigningKey{}, fmt.Errorf("unsupported public key type %T", k.Signer.Public())
	}
	kid := k.KeyID
	if kid == "" {
		kid = jwkThumbprint(jwk)
	}
	jwk["kid"] = kid
	jwk["alg"] = alg
	jwk["use"] = "sig"
	return resolvedSigningKey{alg: alg, kid: kid, signer: k.Signer, jwk: jwk}, nil
}

// algorithmFor maps the public key type to its JOSE algorithm and curve
// parameters, rejecting everything else. RSA is rejected on purpose:
// guessing an algorithm for an unmodeled key type is how alg-confusion
// bugs are born.
func algorithmFor(pub any) (alg, crv string, coordSize int, err error) {
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		switch k.Curve {
		case elliptic.P256():
			return "ES256", "P-256", 32, nil
		case elliptic.P384():
			return "ES384", "P-384", 48, nil
		case elliptic.P521():
			return "ES512", "P-521", 66, nil
		case nil:
			return "", "", 0, fmt.Errorf("EC key has no curve (want P-256, P-384, or P-521)")
		default:
			return "", "", 0, fmt.Errorf("unsupported EC curve %s (want P-256, P-384, or P-521)", k.Curve.Params().Name)
		}
	case ed25519.PublicKey:
		return "EdDSA", "Ed25519", 32, nil
	default:
		return "", "", 0, fmt.Errorf("unsupported key type %T (want *ecdsa.PrivateKey or ed25519.PrivateKey)", pub)
	}
}

// jwkThumbprint computes the RFC 7638 SHA-256 thumbprint of a public
// JWK: the JCS canonical form of the required members only (EC:
// {crv,kty,x,y}; OKP: {crv,kty,x}), base64url-encoded.
func jwkThumbprint(pub map[string]any) string {
	var members map[string]any
	if pub["kty"] == "EC" {
		members = map[string]any{"crv": pub["crv"], "kty": pub["kty"], "x": pub["x"], "y": pub["y"]}
	} else {
		members = map[string]any{"crv": pub["crv"], "kty": pub["kty"], "x": pub["x"]}
	}
	canonical, err := jcs.Canonicalize(members)
	if err != nil {
		// Members are plain strings produced above; unreachable.
		panic(fmt.Sprintf("jcs: canonicalizing JWK thumbprint members: %v", err))
	}
	sum := sha256.Sum256(canonical)
	return base64URL(sum[:])
}

func base64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// signAgentCard computes the card's `signatures` array: one
// AgentCardSignature per configured key, each over the JCS canonical
// form of the card exactly as passed in (doc must NOT contain
// `signatures` yet).
func signAgentCard(doc map[string]any, signing *agentCardSigning) ([]map[string]any, error) {
	payload, err := jcs.Canonicalize(doc)
	if err != nil {
		return nil, fmt.Errorf("canonicalizing agent card: %w", err)
	}
	payloadB64 := base64URL(payload)
	sigs := make([]map[string]any, 0, len(signing.keys))
	for _, key := range signing.keys {
		// Header keys sort to alg, jku, kid, typ under both encoding/json
		// (maps marshal sorted) and JCS — deterministic bytes either way.
		header := map[string]string{
			"alg": key.alg,
			"typ": "JOSE",
			"kid": key.kid,
			"jku": signing.jwksURL,
		}
		headerJSON, err := jcs.Canonicalize(header)
		if err != nil {
			return nil, fmt.Errorf("canonicalizing JWS header: %w", err)
		}
		protected := base64URL(headerJSON)
		input := protected + "." + payloadB64
		sig, err := signJWSInput(key, []byte(input))
		if err != nil {
			return nil, fmt.Errorf("signing agent card with kid %q: %w", key.kid, err)
		}
		sigs = append(sigs, map[string]any{
			"protected": protected,
			"signature": base64URL(sig),
		})
	}
	return sigs, nil
}

// signJWSInput signs the ASCII JWS signing input
// BASE64URL(header) || '.' || BASE64URL(payload) with the key's
// algorithm, returning the JOSE signature bytes (r||s for ECDSA, not
// ASN.1 DER).
func signJWSInput(key resolvedSigningKey, input []byte) ([]byte, error) {
	switch pub := key.signer.Public().(type) {
	case *ecdsa.PublicKey:
		var digest []byte
		var hash crypto.Hash
		switch key.alg {
		case "ES256":
			sum := sha256.Sum256(input)
			digest, hash = sum[:], crypto.SHA256
		case "ES384":
			sum := sha512.Sum384(input)
			digest, hash = sum[:], crypto.SHA384
		case "ES512":
			sum := sha512.Sum512(input)
			digest, hash = sum[:], crypto.SHA512
		default:
			return nil, fmt.Errorf("internal: alg %q on EC key", key.alg)
		}
		der, err := key.signer.Sign(rand.Reader, digest, hash)
		if err != nil {
			return nil, err
		}
		// crypto/ecdsa signs in ASN.1 DER; JOSE wants raw r||s.
		// Go 1.27 dropped ecdsa.UnmarshalASN1, so decode the fixed
		// SEQUENCE(INTEGER r, INTEGER s) shape directly.
		var rs struct{ R, S *big.Int }
		if _, err := asn1.Unmarshal(der, &rs); err != nil {
			return nil, fmt.Errorf("decoding own ECDSA signature: %w", err)
		}
		r, s := rs.R, rs.S
		size := (pub.Curve.Params().BitSize + 7) / 8
		jose := make([]byte, 2*size)
		r.FillBytes(jose[:size])
		s.FillBytes(jose[size:])
		return jose, nil
	case ed25519.PublicKey:
		// Pure Ed25519 signs the message itself; crypto.Signer with
		// opts crypto.Hash(0) is the documented way to reach it.
		return key.signer.Sign(rand.Reader, input, crypto.Hash(0))
	default:
		return nil, fmt.Errorf("internal: unvalidated key type %T", pub)
	}
}

// handleJWKS serves the card verification keys as a JWK Set
// (RFC 7517). Public members only: kty, crv, x, y, kid, alg, use.
func (ds *UIHost) handleJWKS(w http.ResponseWriter, req *http.Request) {
	if ds.agentReady == nil || ds.agentReady.signing == nil {
		http.NotFound(w, req)
		return
	}
	// A slice in configuration order: deterministic by construction,
	// and json.Marshal of the shared maps is safe for concurrent reads.
	keys := make([]map[string]any, 0, len(ds.agentReady.signing.keys))
	for _, k := range ds.agentReady.signing.keys {
		keys = append(keys, k.jwk)
	}
	w.Header().Set("Content-Type", "application/jwk-set+json")
	w.Header().Set("Cache-Control", "no-cache")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{"keys": keys})
}

// pinnedBaseURL returns the explicit, server-configured origin the
// signed card may reference, or "" when none is set. Request-derived
// origins (Host header) are deliberately excluded: a signature over a
// request-derived URL launders an attacker-controlled endpoint as
// validly signed content (see the mount-time guard below).
func (ds *UIHost) pinnedBaseURL() string {
	if ds.agentReady != nil && ds.agentReady.baseURL != "" {
		return strings.TrimRight(ds.agentReady.baseURL, "/")
	}
	if ds.sitemapConfig != nil && ds.sitemapConfig.BaseURL != "" {
		return strings.TrimRight(ds.sitemapConfig.BaseURL, "/")
	}
	return ""
}
