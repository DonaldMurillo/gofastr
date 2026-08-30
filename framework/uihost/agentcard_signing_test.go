package uihost

// agentcard_signing_test.go: A2A v1.0 agent-card signature tests. Two
// independent verification layers:
//
//  1. Go-side construction checks (this file): the served card carries
//     verifiable signatures, the JWKS is public-only, and the mount-time
//     guards fail loudly when broken.
//  2. The committed Node/WebCrypto verifier (testdata/signing/), run as
//     fixture tooling, proving an implementation sharing no code with
//     ours verifies the same cards.

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/jcs"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// signingMountPanic runs Mount on a fresh host and returns the
// recovered panic value (nil when Mount does not panic). Unlike the
// strict-mode mountPanic, signing guards panic with error or string.
func signingMountPanic(opts ...Option) (panicVal any) {
	ds := newAgentReadyHost(opts...)
	defer func() { panicVal = recover() }()
	r := router.New()
	ds.Mount(r)
	return nil
}

func mustSignHost(t *testing.T, baseURL string, keys ...AgentCardSigningKey) (*httptest.Server, map[string]crypto.Signer) {
	t.Helper()
	signers := map[string]crypto.Signer{}
	for _, k := range keys {
		signers[k.KeyID] = k.Signer
	}
	ds := newAgentReadyHost(WithAgentReady(AgentReadyConfig{
		BaseURL:   baseURL,
		AgentCard: &AgentCardConfig{Name: "Signed Agent", Description: "d", MCPEndpoint: "/mcp", SigningKeys: keys},
	}))
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)
	return srv, signers
}

func fixedEd25519(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	seedSum := sha256.Sum256([]byte("gofastr a2a card signing fixture"))
	return ed25519.NewKeyFromSeed(seedSum[:])
}

// ─── Mount-time guards ──────────────────────────────────────────────

// TestSigning_RequiresPinnedBaseURL pins the host-header-trap guard:
// signing configured without an explicit BaseURL must fail the mount,
// never serve a request-derived signed card.
func TestSigning_RequiresPinnedBaseURL(t *testing.T) {
	edKey := fixedEd25519(t)
	pv := signingMountPanic(WithAgentCard(AgentCardConfig{
		Name: "X", Description: "d",
		SigningKeys: []AgentCardSigningKey{{KeyID: "k1", Signer: edKey}},
	}))
	if pv == nil {
		t.Fatal("Mount succeeded with signing keys and no pinned BaseURL; want panic")
	}
	msg, ok := pv.(string)
	if !ok || !strings.Contains(msg, "requires an explicit BaseURL") {
		t.Fatalf("panic = %v, want BaseURL guidance", pv)
	}
}

// TestSigning_SitemapBaseURLSatisfiesGuard: the sitemap BaseURL is an
// acceptable pin (one origin configured once serves everywhere).
func TestSigning_SitemapBaseURLSatisfiesGuard(t *testing.T) {
	edKey := fixedEd25519(t)
	if pv := signingMountPanic(
		WithAgentCard(AgentCardConfig{
			Name: "X", Description: "d",
			SigningKeys: []AgentCardSigningKey{{KeyID: "k1", Signer: edKey}},
		}),
		WithSitemap(SitemapConfig{BaseURL: "https://sm.example"}),
	); pv != nil {
		t.Fatalf("Mount panicked with sitemap base pinned: %v", pv)
	}
}

// TestSigning_RejectsUnsupportedKey: an RSA key must fail the mount
// with the key type named, not sign under a guessed algorithm.
func TestSigning_RejectsUnsupportedKey(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pv := signingMountPanic(WithAgentReady(AgentReadyConfig{
		BaseURL: "https://ok.example",
		AgentCard: &AgentCardConfig{
			Name: "X", Description: "d",
			SigningKeys: []AgentCardSigningKey{{KeyID: "k1", Signer: rsaKey}},
		},
	}))
	if pv == nil {
		t.Fatal("Mount succeeded with an RSA signing key; want panic")
	}
	if msg, ok := pv.(error); !ok || !strings.Contains(msg.Error(), "unsupported key type") {
		t.Fatalf("panic = %v, want unsupported key type error", pv)
	}
}

// TestSigning_RejectsNilSignerAndDuplicateKids covers the remaining
// resolve-time rejections.
func TestSigning_RejectsNilSignerAndDuplicateKids(t *testing.T) {
	if pv := signingMountPanic(WithAgentReady(AgentReadyConfig{
		BaseURL:   "https://ok.example",
		AgentCard: &AgentCardConfig{Name: "X", SigningKeys: []AgentCardSigningKey{{KeyID: "k"}}},
	})); pv == nil {
		t.Fatal("nil Signer must fail the mount")
	}
	edKey := fixedEd25519(t)
	if pv := signingMountPanic(WithAgentReady(AgentReadyConfig{
		BaseURL: "https://ok.example",
		AgentCard: &AgentCardConfig{
			Name: "X", Description: "d",
			SigningKeys: []AgentCardSigningKey{
				{KeyID: "same", Signer: edKey},
				{KeyID: "same", Signer: edKey},
			},
		},
	})); pv == nil {
		t.Fatal("duplicate kid must fail the mount")
	}
}

// ─── Signed card end-to-end (Go-verified) ───────────────────────────

// fetchSignedCard fetches the card at path and returns the parsed
// document and its signatures array.
func fetchSignedCard(t *testing.T, srv *httptest.Server, path string) (map[string]any, []any) {
	t.Helper()
	body, resp := getBody(t, srv.URL+path)
	if resp.StatusCode != 200 {
		t.Fatalf("%s: status %d", path, resp.StatusCode)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("%s: invalid JSON: %v", path, err)
	}
	sigs, ok := doc["signatures"].([]any)
	if !ok || len(sigs) == 0 {
		t.Fatalf("%s: no signatures array: %v", path, doc["signatures"])
	}
	return doc, sigs
}

// verifyCardSignatures verifies every signature in the card against the
// public keys by kid, rebuilding the canonical payload the way a client
// does: drop `signatures`, JCS-canonicalize the rest.
func verifyCardSignatures(t *testing.T, body []byte, pubs map[string]crypto.PublicKey) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	sigs, ok := doc["signatures"].([]any)
	if !ok || len(sigs) == 0 {
		t.Fatalf("no signatures: %v", doc["signatures"])
	}
	delete(doc, "signatures")
	payload, err := jcs.Canonicalize(doc)
	if err != nil {
		t.Fatalf("canonicalize card: %v", err)
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	for i, s := range sigs {
		sigObj, ok := s.(map[string]any)
		if !ok {
			t.Fatalf("signature %d not an object", i)
		}
		protected, _ := sigObj["protected"].(string)
		sigB64, _ := sigObj["signature"].(string)
		if protected == "" || sigB64 == "" {
			t.Fatalf("signature %d missing protected/signature", i)
		}
		if _, hasHeader := sigObj["header"]; hasHeader {
			t.Errorf("signature %d has unexpected unprotected header", i)
		}
		hdrBytes, err := base64.RawURLEncoding.DecodeString(protected)
		if err != nil {
			t.Fatalf("signature %d protected not base64url: %v", i, err)
		}
		var hdr struct {
			Alg string `json:"alg"`
			Typ string `json:"typ"`
			Kid string `json:"kid"`
			Jku string `json:"jku"`
		}
		if err := json.Unmarshal(hdrBytes, &hdr); err != nil {
			t.Fatalf("signature %d protected header: %v", i, err)
		}
		pub, ok := pubs[hdr.Kid]
		if !ok {
			t.Fatalf("signature %d kid %q not in key set", i, hdr.Kid)
		}
		input := protected + "." + payloadB64
		sig, err := base64.RawURLEncoding.DecodeString(sigB64)
		if err != nil {
			t.Fatalf("signature %d not base64url: %v", i, err)
		}
		var verified bool
		switch p := pub.(type) {
		case ed25519.PublicKey:
			verified = ed25519.Verify(p, []byte(input), sig)
		case *ecdsa.PublicKey:
			digest := digestFor(t, hdr.Alg, []byte(input))
			der, err := asn1.Marshal(struct{ R, S *big.Int }{
				R: new(big.Int).SetBytes(sig[:len(sig)/2]),
				S: new(big.Int).SetBytes(sig[len(sig)/2:]),
			})
			if err != nil {
				t.Fatal(err)
			}
			verified = ecdsa.VerifyASN1(p, digest, der)
		default:
			t.Fatalf("kid %q: unsupported public key %T", hdr.Kid, pub)
		}
		if !verified {
			t.Errorf("signature %d (kid %q, alg %s) does NOT verify", i, hdr.Kid, hdr.Alg)
		}
	}
}

func digestFor(t *testing.T, alg string, input []byte) []byte {
	t.Helper()
	switch alg {
	case "ES256":
		s := sha256.Sum256(input)
		return s[:]
	case "ES384":
		s := sha512.Sum384(input)
		return s[:]
	case "ES512":
		s := sha512.Sum512(input)
		return s[:]
	}
	t.Fatalf("no digest for alg %q", alg)
	return nil
}

// TestSignedCard_Verifies: one Ed25519 and one P-256 key, both
// signatures present and verifying, protected header shaped per A2A
// §8.4.2 (alg, typ JOSE, kid, jku → the JWKS), both card paths signed.
func TestSignedCard_Verifies(t *testing.T) {
	edKey := fixedEd25519(t)
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	srv, _ := mustSignHost(t, "https://signed.example",
		AgentCardSigningKey{KeyID: "ed-1", Signer: edKey},
		AgentCardSigningKey{KeyID: "ec-1", Signer: ecKey},
	)
	pubs := map[string]crypto.PublicKey{"ed-1": edKey.Public(), "ec-1": &ecKey.PublicKey}
	for _, path := range []string{"/.well-known/agent-card.json", "/.well-known/agent.json"} {
		body, resp := getBody(t, srv.URL+path)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: status %d", path, resp.StatusCode)
		}
		doc, sigs := fetchSignedCard(t, srv, path)
		if len(sigs) != 2 {
			t.Fatalf("%s: want 2 signatures (rotation set), got %d", path, len(sigs))
		}
		// The interface URL derives from the pinned base, not the test
		// server's 127.0.0.1 origin.
		ifacesAny := doc["supportedInterfaces"].([]any)
		iface := ifacesAny[0].(map[string]any)
		if got := iface["url"].(string); !strings.HasPrefix(got, "https://signed.example/") {
			t.Errorf("%s: interface url %q not from pinned base", path, got)
		}
		verifyCardSignatures(t, []byte(body), pubs)
	}
}

// TestSignedCard_HostileHostHeader: a request with an attacker
// controlled Host header must never produce a signed card containing
// that host. This is the laundered-endpoint attack the mount guard and
// the pinned-base signing make impossible.
func TestSignedCard_HostileHostHeader(t *testing.T) {
	edKey := fixedEd25519(t)
	srv, _ := mustSignHost(t, "https://real.example",
		AgentCardSigningKey{KeyID: "ed-1", Signer: edKey},
	)
	// One request with a hostile Host via a custom transport so the
	// client cannot rewrite it.
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/.well-known/agent-card.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "evil.example"
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	buf := make([]byte, 1<<20)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if strings.Contains(body, "evil.example") {
		t.Fatalf("signed card contains hostile host:\n%s", body)
	}
	// And the card still verifies against the real key.
	verifyCardSignatures(t, []byte(body), map[string]crypto.PublicKey{"ed-1": edKey.Public()})
}

// TestJWKS_PublicKeysOnly: the JWKS must publish the expected public
// members and MUST NOT contain any private parameter. The whole
// response body is scanned for `"d"`, `"p"`, `"q"`, `"dp"`, `"dq"`,
// `"qi"`, `"k"`, and `"oth"` members — any hit is private material.
func TestJWKS_PublicKeysOnly(t *testing.T) {
	edKey := fixedEd25519(t)
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	srv, _ := mustSignHost(t, "https://signed.example",
		AgentCardSigningKey{KeyID: "ed-1", Signer: edKey},
		AgentCardSigningKey{KeyID: "ec-1", Signer: ecKey},
	)
	body, resp := getBody(t, srv.URL+"/.well-known/jwks.json")
	if resp.StatusCode != 200 {
		t.Fatalf("jwks status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/jwk-set+json") {
		t.Errorf("jwks Content-Type %q", ct)
	}
	var set struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.Unmarshal([]byte(body), &set); err != nil {
		t.Fatalf("jwks invalid JSON: %v", err)
	}
	if len(set.Keys) != 2 {
		t.Fatalf("jwks has %d keys, want 2", len(set.Keys))
	}
	byKid := map[string]map[string]any{}
	for _, k := range set.Keys {
		kid, _ := k["kid"].(string)
		byKid[kid] = k
	}
	ed := byKid["ed-1"]
	if ed == nil || ed["kty"] != "OKP" || ed["crv"] != "Ed25519" || ed["alg"] != "EdDSA" || ed["use"] != "sig" {
		t.Errorf("ed-1 JWK wrong: %v", ed)
	}
	if x, _ := ed["x"].(string); x != base64.RawURLEncoding.EncodeToString(edKey.Public().(ed25519.PublicKey)) {
		t.Errorf("ed-1 x mismatch: %v", ed["x"])
	}
	ec := byKid["ec-1"]
	if ec == nil || ec["kty"] != "EC" || ec["crv"] != "P-256" || ec["alg"] != "ES256" {
		t.Errorf("ec-1 JWK wrong: %v", ec)
	}
	for _, member := range []string{"d", "p", "q", "dp", "dq", "qi", "k", "oth"} {
		if strings.Contains(body, `"`+member+`"`) {
			t.Errorf("JWKS response contains private member %q:\n%s", member, body)
		}
	}
}

// TestKidDefaultsToThumbprint: an empty KeyID must yield the RFC 7638
// SHA-256 thumbprint of the public JWK, computed independently here.
func TestKidDefaultsToThumbprint(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	srv, _ := mustSignHost(t, "https://signed.example", AgentCardSigningKey{Signer: ecKey})
	doc, sigs := fetchSignedCard(t, srv, "/.well-known/agent-card.json")
	_ = doc
	sig := sigs[0].(map[string]any)
	protected, _ := base64.RawURLEncoding.DecodeString(sig["protected"].(string))
	var hdr struct {
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(protected, &hdr); err != nil {
		t.Fatal(err)
	}
	// Independent RFC 7638 computation with the sorted-member JSON
	// string built by hand.
	xb := make([]byte, 32)
	yb := make([]byte, 32)
	ecKey.PublicKey.X.FillBytes(xb)
	ecKey.PublicKey.Y.FillBytes(yb)
	canonical := `{"crv":"P-256","kty":"EC","x":"` + base64.RawURLEncoding.EncodeToString(xb) +
		`","y":"` + base64.RawURLEncoding.EncodeToString(yb) + `"}`
	sum := sha256.Sum256([]byte(canonical))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if hdr.Kid != want {
		t.Errorf("kid = %q, want RFC 7638 thumbprint %q", hdr.Kid, want)
	}
}

// TestUnsignedCardUnchanged: with no SigningKeys the card has no
// signatures field and no JWKS route — signing is purely additive.
func TestUnsignedCardUnchanged(t *testing.T) {
	ds := newAgentReadyHost(WithAgentCard(AgentCardConfig{Name: "X", Description: "d"}))
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)
	body, resp := getBody(t, srv.URL+"/.well-known/agent-card.json")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if strings.Contains(body, "signatures") {
		t.Errorf("unsigned card carries signatures: %s", body)
	}
	if _, resp2 := getBody(t, srv.URL+"/.well-known/jwks.json"); resp2.StatusCode != 404 {
		t.Errorf("jwks without signing keys: status %d, want 404", resp2.StatusCode)
	}
}
