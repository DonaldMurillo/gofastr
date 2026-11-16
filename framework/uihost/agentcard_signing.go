package uihost

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
	"log/slog"
	"math/big"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/jcs"
)

// AgentCardSigningKey is one key that signs the agent card; each key adds
// one signature to the card and one JWK to /.well-known/jwks.json.
type AgentCardSigningKey struct {
	KeyID  string        // the JWS and JWKS kid; empty means the RFC 7638 thumbprint
	Signer crypto.Signer // *ecdsa.PrivateKey on P-256, P-384 or P-521, or ed25519.PrivateKey
}

type agentCardSigning struct {
	keys    []resolvedSigningKey
	jwksURL string
}

type resolvedSigningKey struct {
	alg    string
	kid    string
	signer crypto.Signer
	jwk    map[string]any
}

const jwksPath = "/.well-known/jwks.json"

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
		if _, err := pub.ECDH(); err != nil {
			return resolvedSigningKey{}, fmt.Errorf("EC key %q is not a valid point on %s: %w", k.KeyID, crv, err)
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

func jwkThumbprint(pub map[string]any) string {
	var members map[string]any
	if pub["kty"] == "EC" {
		members = map[string]any{"crv": pub["crv"], "kty": pub["kty"], "x": pub["x"], "y": pub["y"]}
	} else {
		members = map[string]any{"crv": pub["crv"], "kty": pub["kty"], "x": pub["x"]}
	}
	canonical, err := jcs.Canonicalize(members)
	if err != nil {
		panic(fmt.Sprintf("jcs: canonicalizing JWK thumbprint members: %v", err))
	}
	sum := sha256.Sum256(canonical)
	return base64URL(sum[:])
}

func base64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func signAgentCard(doc map[string]any, signing *agentCardSigning) ([]map[string]any, error) {
	payload, err := jcs.Canonicalize(doc)
	if err != nil {
		return nil, fmt.Errorf("canonicalizing agent card: %w", err)
	}
	payloadB64 := base64URL(payload)
	sigs := make([]map[string]any, 0, len(signing.keys))
	for _, key := range signing.keys {
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
		return key.signer.Sign(rand.Reader, input, crypto.Hash(0))
	default:
		return nil, fmt.Errorf("internal: unvalidated key type %T", pub)
	}
}

func (ds *UIHost) handleJWKS(w http.ResponseWriter, req *http.Request) {
	if ds.agentReady == nil || ds.agentReady.signing == nil {
		http.NotFound(w, req)
		return
	}
	keys := make([]map[string]any, 0, len(ds.agentReady.signing.keys))
	for _, k := range ds.agentReady.signing.keys {
		keys = append(keys, k.jwk)
	}
	b, err := json.MarshalIndent(map[string]any{"keys": keys}, "", "  ")
	if err != nil {
		slog.Warn("uihost: encode JWK set", "error", err)
		i18n.HTTPError(w, req, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	w.Header().Set("Content-Type", "application/jwk-set+json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(b)
	w.Write([]byte("\n"))
}

func (ds *UIHost) pinnedBaseURL() string {
	if ds.agentReady != nil && ds.agentReady.baseURL != "" {
		return strings.TrimRight(ds.agentReady.baseURL, "/")
	}
	if ds.sitemapConfig != nil && ds.sitemapConfig.BaseURL != "" {
		return strings.TrimRight(ds.sitemapConfig.BaseURL, "/")
	}
	return ""
}
