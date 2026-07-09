package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ─── shared test helpers (used by oidc_test.go and oidc_security_test.go) ────
//
// A configurable in-process IdP: it serves OIDC discovery, JWKS, the token
// endpoint (minting real RS256/ES256 id_tokens) and userinfo. Tests mutate the
// exported fields before driving a flow so each case can stage a different
// failure mode without forking the server.

func b64u(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func mustJSONMarshal(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func mustRSAKey(t *testing.T, bits int) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatalf("rsa generate: %v", err)
	}
	return k
}

func mustECKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa generate: %v", err)
	}
	return k
}

// rsaJWKMap builds the JWK JSON object for an RSA public key.
func rsaJWKMap(kid string, key *rsa.PublicKey) map[string]interface{} {
	return map[string]interface{}{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": kid,
		"n": b64u(key.N.Bytes()),
		"e": b64u(big.NewInt(int64(key.E)).Bytes()),
	}
}

// ecJWKMap builds the JWK JSON object for an EC P-256 public key.
func ecJWKMap(kid string, key *ecdsa.PublicKey) map[string]interface{} {
	return map[string]interface{}{
		"kty": "EC", "use": "sig", "alg": "ES256", "kid": kid, "crv": "P-256",
		"x": b64u(key.X.FillBytes(make([]byte, 32))),
		"y": b64u(key.Y.FillBytes(make([]byte, 32))),
	}
}

func signRS256(t *testing.T, key *rsa.PrivateKey, signingInput []byte) []byte {
	t.Helper()
	h := sha256.Sum256(signingInput)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
	if err != nil {
		t.Fatalf("rsa sign: %v", err)
	}
	return sig
}

// signES256Raw produces the JOSE raw R||S (64-byte) EC signature.
func signES256Raw(t *testing.T, key *ecdsa.PrivateKey, signingInput []byte) []byte {
	t.Helper()
	h := sha256.Sum256(signingInput)
	r, s, err := ecdsa.Sign(rand.Reader, key, h[:])
	if err != nil {
		t.Fatalf("ecdsa sign: %v", err)
	}
	out := make([]byte, 64)
	r.FillBytes(out[:32])
	s.FillBytes(out[32:])
	return out
}

// signES256DER produces an ASN.1/DER EC signature (the WRONG encoding for JOSE
// — used only by the negative suite).
func signES256DER(t *testing.T, key *ecdsa.PrivateKey, signingInput []byte) []byte {
	t.Helper()
	h := sha256.Sum256(signingInput)
	r, s, err := ecdsa.Sign(rand.Reader, key, h[:])
	if err != nil {
		t.Fatalf("ecdsa sign: %v", err)
	}
	der, err := encodeECSignatureDER(r, s)
	if err != nil {
		t.Fatalf("der encode: %v", err)
	}
	return der
}

// encodeECSignatureDER builds the ASN.1 SEQUENCE{ INTEGER(r), INTEGER(s) } used
// by X.509 — hand-rolled to avoid pulling crypto/x509 + ecdsa-specific helpers.
func encodeECSignatureDER(r, s *big.Int) ([]byte, error) {
	mkInt := func(v *big.Int) []byte {
		b := v.Bytes()
		if len(b) == 0 {
			b = []byte{0}
		}
		if b[0]&0x80 != 0 {
			b = append([]byte{0x00}, b...)
		}
		out := []byte{0x02, byte(len(b))}
		return append(out, b...)
	}
	body := append(append([]byte{}, mkInt(r)...), mkInt(s)...)
	out := []byte{0x30, byte(len(body))}
	return append(out, body...), nil
}

// buildCompact assembles header.payload.sig from already-marshalled pieces.
func buildCompact(headerJSON, payloadJSON []byte, sig []byte) string {
	return b64u(headerJSON) + "." + b64u(payloadJSON) + "." + b64u(sig)
}

// standardIDClaims returns a valid claim set for the given issuer/client.
func standardIDClaims(issuer, clientID string) map[string]interface{} {
	return map[string]interface{}{
		"iss":     issuer,
		"sub":     "user-123",
		"aud":     clientID,
		"exp":     time.Now().Add(time.Hour).Unix(),
		"iat":     time.Now().Unix(),
		"email":   "alice@example.com",
		"name":    "Alice Example",
		"picture": "https://example.com/alice.png",
	}
}

// fakeIdP is a configurable OIDC identity provider backed by httptest.
type fakeIdP struct {
	server *httptest.Server
	issuer string

	clientID     string
	clientSecret string

	rsaKey *rsa.PrivateKey
	rsaKID string
	ecKey  *ecdsa.PrivateKey
	ecKID  string

	// jwksFn returns the key set to serve on the Nth JWKS hit (0-indexed). If
	// nil, the default set (the signing key(s)) is served every time.
	jwksFn  func(hit int) []map[string]interface{}
	jwksHit int32 // atomic

	// id-token minting knobs.
	header    map[string]interface{}           // overrides the default header
	claims    map[string]interface{}           // claim set
	signAlg   string                           // "RS256" (default) or "ES256"
	sign      func(signingInput []byte) []byte // overrides alg-based signing
	idMissing bool                             // omit id_token from the token response
	idToken   string                           // if set, returned verbatim instead of minting
	transform func(idToken string) string      // post-process the minted token

	// userinfo.
	userinfo       map[string]interface{}
	userinfoSub    string // if set, used as userinfo "sub"
	omitUserinfoEp bool

	// discovery.
	discoveryIssuer string // overrides the doc's "issuer" field (mismatch test)
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	f := &fakeIdP{
		clientID:     "test-client",
		clientSecret: "test-secret",
		rsaKey:       mustRSAKey(t, 2048),
		rsaKID:       "rsa-1",
		ecKey:        mustECKey(t),
		ecKID:        "ec-1",
		signAlg:      "RS256",
	}
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		issuer := f.issuer
		ui := issuer + "/userinfo"
		doc := map[string]interface{}{
			"issuer":                 f.discoveryIssuer,
			"authorization_endpoint": issuer + "/authorize",
			"token_endpoint":         issuer + "/token",
			"jwks_uri":               issuer + "/jwks",
		}
		if f.discoveryIssuer == "" {
			doc["issuer"] = issuer
		}
		if !f.omitUserinfoEp {
			doc["userinfo_endpoint"] = ui
		}
		writeJSON(t, w, doc)
	})

	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		hit := atomic.AddInt32(&f.jwksHit, 1) - 1
		var keys []map[string]interface{}
		if f.jwksFn != nil {
			keys = f.jwksFn(int(hit))
		} else {
			keys = f.defaultJWKS()
		}
		writeJSON(t, w, map[string]interface{}{"keys": keys})
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		resp := map[string]interface{}{
			"access_token":  "test-access-token",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"refresh_token": "test-refresh",
		}
		if !f.idMissing {
			if f.idToken != "" {
				resp["id_token"] = f.idToken
			} else {
				resp["id_token"] = f.mintIDToken(t)
			}
		}
		writeJSON(t, w, resp)
	})

	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		ui := map[string]interface{}{}
		for k, v := range f.userinfo {
			ui[k] = v
		}
		if s := f.userinfoSub; s != "" {
			ui["sub"] = s
		}
		writeJSON(t, w, ui)
	})

	f.server = httptest.NewServer(mux)
	f.issuer = f.server.URL
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeIdP) defaultJWKS() []map[string]interface{} {
	out := []map[string]interface{}{rsaJWKMap(f.rsaKID, &f.rsaKey.PublicKey)}
	if f.ecKey != nil {
		out = append(out, ecJWKMap(f.ecKID, &f.ecKey.PublicKey))
	}
	return out
}

// defaultHeader returns the header implied by signAlg/kid.
func (f *fakeIdP) defaultHeader() map[string]interface{} {
	if f.signAlg == "ES256" {
		return map[string]interface{}{"alg": "ES256", "kid": f.ecKID, "typ": "JWT"}
	}
	return map[string]interface{}{"alg": "RS256", "kid": f.rsaKID, "typ": "JWT"}
}

func (f *fakeIdP) mintIDToken(t *testing.T) string {
	t.Helper()
	header := f.defaultHeader()
	for k, v := range f.header {
		header[k] = v
	}
	claims := f.claims
	if claims == nil {
		claims = standardIDClaims(f.issuer, f.clientID)
	}
	headerJSON := mustJSONMarshal(t, header)
	claimsJSON := mustJSONMarshal(t, claims)
	signingInput := []byte(b64u(headerJSON) + "." + b64u(claimsJSON))
	var sig []byte
	if f.sign != nil {
		sig = f.sign(signingInput)
	} else if f.signAlg == "ES256" {
		sig = signES256Raw(t, f.ecKey, signingInput)
	} else {
		sig = signRS256(t, f.rsaKey, signingInput)
	}
	tok := b64u(headerJSON) + "." + b64u(claimsJSON) + "." + b64u(sig)
	if f.transform != nil {
		tok = f.transform(tok)
	}
	return tok
}

func writeJSON(t *testing.T, w http.ResponseWriter, v interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// newTestProvider builds an OIDCProvider pointing at the fake.
func newTestProvider(t *testing.T, f *fakeIdP) *OIDCProvider {
	t.Helper()
	p, err := NewOIDCProvider(OIDCConfig{
		Issuer:       f.issuer,
		ClientID:     f.clientID,
		ClientSecret: f.clientSecret,
		RedirectURL:  "https://app.example.com/cb",
		ProviderName: "testoidc",
	})
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}
	return p
}

func ctxBg() context.Context { return context.Background() }

// ─── positive tests ──────────────────────────────────────────────────────────

// TestOIDC_RS256_FullFlow: ExchangeCode verifies an RS256 id_token, caches
// claims, and FetchUserInfo maps them.
func TestOIDC_RS256_FullFlow(t *testing.T) {
	f := newFakeIdP(t)
	p := newTestProvider(t, f)

	tok, err := p.ExchangeCode(ctxBg(), "any-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "test-access-token" {
		t.Fatalf("access_token = %q", tok.AccessToken)
	}
	if tok.RefreshToken != "test-refresh" {
		t.Fatalf("refresh_token = %q", tok.RefreshToken)
	}
	if time.Since(tok.Expiry) > 0 && tok.Expiry.IsZero() {
		t.Fatalf("expiry not set")
	}

	info, err := p.FetchUserInfo(ctxBg(), tok.AccessToken)
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if info.ID != "user-123" {
		t.Errorf("ID = %q want user-123", info.ID)
	}
	if info.Email != "alice@example.com" {
		t.Errorf("Email = %q", info.Email)
	}
	if info.Name != "Alice Example" {
		t.Errorf("Name = %q", info.Name)
	}
	if info.AvatarURL != "https://example.com/alice.png" {
		t.Errorf("AvatarURL = %q", info.AvatarURL)
	}
	if info.Provider != "testoidc" {
		t.Errorf("Provider = %q", info.Provider)
	}
}

// TestOIDC_ES256_Flow: the EC (P-256, ES256) path verifies end-to-end.
func TestOIDC_ES256_Flow(t *testing.T) {
	f := newFakeIdP(t)
	f.signAlg = "ES256"
	f.jwksFn = func(hit int) []map[string]interface{} {
		return []map[string]interface{}{ecJWKMap(f.ecKID, &f.ecKey.PublicKey)}
	}
	p := newTestProvider(t, f)

	tok, err := p.ExchangeCode(ctxBg(), "any-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	info, err := p.FetchUserInfo(ctxBg(), tok.AccessToken)
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if info.ID != "user-123" {
		t.Errorf("ID = %q", info.ID)
	}
}

// TestOIDC_ClaimsMappingOverride: custom claim names are honored.
func TestOIDC_ClaimsMappingOverride(t *testing.T) {
	f := newFakeIdP(t)
	f.claims = map[string]interface{}{
		"iss": f.issuer, "sub": "u-9", "aud": f.clientID,
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		"upn":         "bob@corp.example",
		"displayname": "Bob",
		"thumbnail":   "https://example.com/bob.png",
	}
	p, err := NewOIDCProvider(OIDCConfig{
		Issuer: f.issuer, ClientID: f.clientID, ClientSecret: f.clientSecret,
		RedirectURL: "https://app.example.com/cb", ProviderName: "kc",
		Claims: OIDCClaimsMapping{
			EmailClaim:  "upn",
			NameClaim:   "displayname",
			AvatarClaim: "thumbnail",
		},
	})
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}
	tok, err := p.ExchangeCode(ctxBg(), "any-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	info, err := p.FetchUserInfo(ctxBg(), tok.AccessToken)
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if info.ID != "u-9" {
		t.Errorf("ID = %q", info.ID)
	}
	if info.Email != "bob@corp.example" {
		t.Errorf("Email = %q", info.Email)
	}
	if info.Name != "Bob" {
		t.Errorf("Name = %q", info.Name)
	}
	if info.AvatarURL != "https://example.com/bob.png" {
		t.Errorf("AvatarURL = %q", info.AvatarURL)
	}
	if info.Provider != "kc" {
		t.Errorf("Provider = %q", info.Provider)
	}
}

// TestOIDC_UserinfoFallback: id_token lacks email → userinfo supplies it; the
// userinfo sub matches the id_token sub.
func TestOIDC_UserinfoFallback(t *testing.T) {
	f := newFakeIdP(t)
	f.claims = map[string]interface{}{
		"iss": f.issuer, "sub": "user-123", "aud": f.clientID,
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		"name": "Alice", // no email, no picture
	}
	f.userinfo = map[string]interface{}{
		"email":   "from-userinfo@example.com",
		"picture": "https://example.com/ui.png",
	}
	f.userinfoSub = "user-123"
	p := newTestProvider(t, f)

	tok, err := p.ExchangeCode(ctxBg(), "any-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	info, err := p.FetchUserInfo(ctxBg(), tok.AccessToken)
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if info.Email != "from-userinfo@example.com" {
		t.Errorf("Email = %q (want from userinfo)", info.Email)
	}
	if info.AvatarURL != "https://example.com/ui.png" {
		t.Errorf("AvatarURL = %q", info.AvatarURL)
	}
	if info.Name != "Alice" {
		t.Errorf("Name = %q (want from id_token)", info.Name)
	}
}

// TestOIDC_UserinfoOnlyStaleToken: no cached claims (stale token) → built purely
// from userinfo, ID from userinfo sub.
func TestOIDC_UserinfoOnlyStaleToken(t *testing.T) {
	f := newFakeIdP(t)
	f.userinfo = map[string]interface{}{
		"email": "stale@example.com", "name": "Stale",
	}
	f.userinfoSub = "sub-from-ui"
	p := newTestProvider(t, f)

	info, err := p.FetchUserInfo(ctxBg(), "a-token-not-in-cache")
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if info.ID != "sub-from-ui" {
		t.Errorf("ID = %q", info.ID)
	}
	if info.Email != "stale@example.com" {
		t.Errorf("Email = %q", info.Email)
	}
}

// TestOIDC_JWKSRotation: token kid unknown to a stale cache → forced refetch
// surfaces the new key → success.
func TestOIDC_JWKSRotation(t *testing.T) {
	f := newFakeIdP(t)
	// Token is signed with rsaKey (kid rsa-1). Hit 0 serves only a different
	// key; hit >=1 serves the real one (rotation publishes it).
	oldKey := mustRSAKey(t, 2048)
	f.jwksFn = func(hit int) []map[string]interface{} {
		if hit == 0 {
			return []map[string]interface{}{rsaJWKMap("old", &oldKey.PublicKey)}
		}
		return []map[string]interface{}{
			rsaJWKMap("old", &oldKey.PublicKey),
			rsaJWKMap(f.rsaKID, &f.rsaKey.PublicKey),
		}
	}
	p := newTestProvider(t, f)

	tok, err := p.ExchangeCode(ctxBg(), "any-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if hits := atomic.LoadInt32(&f.jwksHit); hits != 2 {
		t.Fatalf("jwks hits = %d, want 2 (initial + one rotation refetch)", hits)
	}
	if info, err := p.FetchUserInfo(ctxBg(), tok.AccessToken); err != nil || info.ID != "user-123" {
		t.Fatalf("FetchUserInfo after rotation: %v (id=%v)", err, info)
	}
}

// TestOIDC_AudArrayWithAzp: multi-audience token with a matching azp passes.
func TestOIDC_AudArrayWithAzp(t *testing.T) {
	f := newFakeIdP(t)
	f.claims = map[string]interface{}{
		"iss": f.issuer, "sub": "user-123",
		"aud": []string{f.clientID, "other-audience"},
		"azp": f.clientID,
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		"email": "alice@example.com",
	}
	p := newTestProvider(t, f)
	tok, err := p.ExchangeCode(ctxBg(), "any-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken == "" {
		t.Fatal("no access token")
	}
}

// TestOIDC_AuthURLParams: AuthURL contains the required params and no nonce.
func TestOIDC_AuthURLParams(t *testing.T) {
	f := newFakeIdP(t)
	p := newTestProvider(t, f)

	got := p.AuthURL("state-xyz")
	if !strings.HasPrefix(got, f.issuer+"/authorize") {
		t.Fatalf("AuthURL = %q, want %s/authorize prefix", got, f.issuer)
	}
	for _, want := range []string{
		"response_type=code",
		"client_id=test-client",
		"redirect_uri=https%3A%2F%2Fapp.example.com%2Fcb",
		"scope=openid+email+profile",
		"state=state-xyz",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("AuthURL missing %q\nfull: %s", want, got)
		}
	}
	if strings.Contains(got, "nonce") {
		t.Errorf("AuthURL must not send a nonce (confidential code flow): %s", got)
	}
	// The confidential code flow relies on the HMAC state token + client
	// secret; it deliberately does not send a PKCE code_challenge.
	if strings.Contains(got, "code_challenge") {
		t.Errorf("AuthURL must not send a PKCE code_challenge (confidential code flow): %s", got)
	}
}

// TestOIDC_AuthURLFallbackOnDiscoveryFailure: when discovery is unreachable,
// AuthURL falls back to <issuer>/authorize rather than erroring.
func TestOIDC_AuthURLFallbackOnDiscoveryFailure(t *testing.T) {
	p, err := NewOIDCProvider(OIDCConfig{
		Issuer:   "http://127.0.0.1:1", // nothing listening
		ClientID: "c", ClientSecret: "s", RedirectURL: "https://app.example.com/cb",
	})
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}
	got := p.AuthURL("st")
	if !strings.HasPrefix(got, "http://127.0.0.1:1/authorize") {
		t.Fatalf("AuthURL fallback = %q", got)
	}
}

// TestOIDC_NewProviderValidation: required fields + http(s) scheme rules.
func TestOIDC_NewProviderValidation(t *testing.T) {
	cases := []struct {
		name string
		cfg  OIDCConfig
	}{
		{"missing issuer", OIDCConfig{ClientID: "c", ClientSecret: "s", RedirectURL: "r"}},
		{"missing client_id", OIDCConfig{Issuer: "https://a.com", ClientSecret: "s", RedirectURL: "r"}},
		{"missing secret", OIDCConfig{Issuer: "https://a.com", ClientID: "c", RedirectURL: "r"}},
		{"missing redirect", OIDCConfig{Issuer: "https://a.com", ClientID: "c", ClientSecret: "s"}},
		{"plain http remote", OIDCConfig{Issuer: "http://keycloak.example", ClientID: "c", ClientSecret: "s", RedirectURL: "r"}},
		{"ftp scheme", OIDCConfig{Issuer: "ftp://localhost", ClientID: "c", ClientSecret: "s", RedirectURL: "r"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewOIDCProvider(tc.cfg); err == nil {
				t.Fatalf("expected error, got nil")
			}
		})
	}
	// localhost http is accepted.
	for _, iss := range []string{"http://localhost:8080", "http://127.0.0.1:8080"} {
		if _, err := NewOIDCProvider(OIDCConfig{Issuer: iss, ClientID: "c", ClientSecret: "s", RedirectURL: "r"}); err != nil {
			t.Fatalf("expected %s accepted, got %v", iss, err)
		}
	}
}

// TestOIDC_NoKidSingleKey: a header without kid is resolved against a JWKS
// that advertises exactly one signing key.
func TestOIDC_NoKidSingleKey(t *testing.T) {
	f := newFakeIdP(t)
	f.header = map[string]interface{}{"alg": "RS256", "kid": "", "typ": "JWT"} // kid absent/empty
	f.jwksFn = func(int) []map[string]interface{} {
		return []map[string]interface{}{rsaJWKMap("", &f.rsaKey.PublicKey)}
	}
	p := newTestProvider(t, f)
	tok, err := p.ExchangeCode(ctxBg(), "any-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken == "" {
		t.Fatal("no access token")
	}
}
