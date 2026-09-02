package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"io"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── negative / security suite ───────────────────────────────────────────────
//
// Every case here MUST fail closed: a forged or malformed id_token never yields
// a usable *OAuth2Token or *OAuth2UserInfo. Helpers live in oidc_test.go.

// mustFailExchange runs ExchangeCode and fails the test if it did NOT error.
func mustFailExchange(t *testing.T, p *OIDCProvider) error {
	t.Helper()
	tok, err := p.ExchangeCode(ctxBg(), "any-code")
	if err == nil {
		t.Fatalf("ExchangeCode unexpectedly succeeded: %+v", tok)
	}
	return err
}

// cloneClaims returns a shallow copy so each test can mutate freely.
func cloneClaims(base map[string]any) map[string]any {
	out := make(map[string]any, len(base))
	maps.Copy(out, base)
	return out
}

// baseClaims is a valid claim set for f that tests then perturb.
func baseClaims(f *fakeIdP) map[string]any {
	return map[string]any{
		"iss": f.issuer, "sub": "user-123", "aud": f.clientID,
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		"email": "alice@example.com",
	}
}

// ── alg confusion ────────────────────────────────────────────────────────────

// TestOIDCSec_AlgNone: an unsigned ("none") token is rejected.
func TestOIDCSec_AlgNone(t *testing.T) {
	f := newFakeIdP(t)
	f.header = map[string]any{"alg": "none"}
	f.sign = func([]byte) []byte { return nil } // empty signature
	p := newTestProvider(t, f)
	mustFailExchange(t, p)
}

// TestOIDCSec_AlgHS256: a token HMAC'd with a JWKS RSA key's public bytes (the
// classic alg-confusion attack) is rejected at the alg allowlist.
func TestOIDCSec_AlgHS256(t *testing.T) {
	f := newFakeIdP(t)
	f.header = map[string]any{"alg": "HS256"}
	f.sign = func(signingInput []byte) []byte {
		mac := hmac.New(sha256.New, f.rsaKey.PublicKey.N.Bytes())
		mac.Write(signingInput)
		return mac.Sum(nil)
	}
	p := newTestProvider(t, f)
	mustFailExchange(t, p)
}

// TestOIDCSec_AlgLowercase: "rs256" is not "RS256", exact match only.
func TestOIDCSec_AlgLowercase(t *testing.T) {
	f := newFakeIdP(t)
	f.header = map[string]any{"alg": "rs256"} // validly RS256-signed below
	p := newTestProvider(t, f)
	mustFailExchange(t, p)
}

// ── integrity ────────────────────────────────────────────────────────────────

// TestOIDCSec_TamperedPayload: flipping one payload byte breaks the signature.
func TestOIDCSec_TamperedPayload(t *testing.T) {
	f := newFakeIdP(t)
	f.transform = func(tok string) string {
		parts := strings.Split(tok, ".")
		if len(parts) != 3 || len(parts[1]) == 0 {
			return tok
		}
		b := []byte(parts[1])
		if b[0] == 'A' {
			b[0] = 'B'
		} else {
			b[0] = 'A'
		}
		return parts[0] + "." + string(b) + "." + parts[2]
	}
	p := newTestProvider(t, f)
	mustFailExchange(t, p)
}

// TestOIDCSec_MalformedIDToken: tokens that are not exactly three dot-parts,
// or whose header is not valid base64url JSON, are rejected.
func TestOIDCSec_MalformedIDToken(t *testing.T) {
	for _, tok := range []string{
		"only.two",        // two parts
		"a.b.c.d",         // four parts
		"!!!.payload.sig", // header not valid base64url
		".",               // degenerate
	} {
		f := newFakeIdP(t)
		f.idToken = tok
		p := newTestProvider(t, f)
		if _, err := p.ExchangeCode(ctxBg(), "any-code"); err == nil {
			t.Errorf("token %q unexpectedly accepted", tok)
		}
	}
}

// TestOIDCSec_ECKeyForRS256: an EC key served where an RSA key is expected is
// rejected (kty/alg mismatch), even though the kid matches.
func TestOIDCSec_ECKeyForRS256(t *testing.T) {
	f := newFakeIdP(t)
	f.jwksFn = func(int) []map[string]any {
		// Serve the EC public key under the RSA kid the token references.
		return []map[string]any{ecJWKMap(f.rsaKID, &f.ecKey.PublicKey)}
	}
	// Token stays RS256, signed with the RSA key, kid rsa-1.
	p := newTestProvider(t, f)
	mustFailExchange(t, p)
}

// TestOIDCSec_ES256DERSig: an ES256 token whose signature is ASN.1/DER instead
// of raw R||S is rejected (wrong length).
func TestOIDCSec_ES256DERSig(t *testing.T) {
	f := newFakeIdP(t)
	f.signAlg = "ES256"
	f.sign = func(signingInput []byte) []byte {
		return signES256DER(t, f.ecKey, signingInput)
	}
	f.jwksFn = func(int) []map[string]any {
		return []map[string]any{ecJWKMap(f.ecKID, &f.ecKey.PublicKey)}
	}
	p := newTestProvider(t, f)
	mustFailExchange(t, p)
}

// ── claim violations ─────────────────────────────────────────────────────────

// TestOIDCSec_WrongIssuer: id_token iss != configured issuer.
func TestOIDCSec_WrongIssuer(t *testing.T) {
	f := newFakeIdP(t)
	c := baseClaims(f)
	c["iss"] = "https://wrong.example"
	f.claims = c
	p := newTestProvider(t, f)
	mustFailExchange(t, p)
}

// TestOIDCSec_AudMissingClient: aud does not contain client_id.
func TestOIDCSec_AudMissingClient(t *testing.T) {
	f := newFakeIdP(t)
	c := baseClaims(f)
	c["aud"] = "someone-else"
	f.claims = c
	p := newTestProvider(t, f)
	mustFailExchange(t, p)
}

// TestOIDCSec_AudArrayNoAzp: multi-audience token without a matching azp.
func TestOIDCSec_AudArrayNoAzp(t *testing.T) {
	f := newFakeIdP(t)
	c := baseClaims(f)
	c["aud"] = []string{f.clientID, "other"}
	// no azp
	f.claims = c
	p := newTestProvider(t, f)
	mustFailExchange(t, p)
}

// TestOIDCSec_SingleAudBadAzp: a single-audience token whose azp names a
// different client is rejected, the azp check is unconditional (OIDC
// §3.1.3.7.3), not multi-audience-only.
func TestOIDCSec_SingleAudBadAzp(t *testing.T) {
	f := newFakeIdP(t)
	c := baseClaims(f)
	c["aud"] = f.clientID
	c["azp"] = "totally-different-client"
	f.claims = c
	p := newTestProvider(t, f)
	mustFailExchange(t, p)
}

// TestOIDCSec_FutureNbf: a token whose not-before is in the future is
// rejected (RFC 7519 §4.1.5 MUST).
func TestOIDCSec_FutureNbf(t *testing.T) {
	f := newFakeIdP(t)
	c := baseClaims(f)
	c["nbf"] = time.Now().Add(2 * time.Hour).Unix()
	f.claims = c
	p := newTestProvider(t, f)
	mustFailExchange(t, p)
}

// TestOIDCSec_Expired: exp in the past (beyond the 60s leeway).
func TestOIDCSec_Expired(t *testing.T) {
	f := newFakeIdP(t)
	c := baseClaims(f)
	c["exp"] = time.Now().Add(-10 * time.Minute).Unix()
	f.claims = c
	p := newTestProvider(t, f)
	mustFailExchange(t, p)
}

// TestOIDCSec_FutureIat: iat well past the 60s skew window.
func TestOIDCSec_FutureIat(t *testing.T) {
	f := newFakeIdP(t)
	c := baseClaims(f)
	c["iat"] = time.Now().Add(10 * time.Minute).Unix()
	f.claims = c
	p := newTestProvider(t, f)
	mustFailExchange(t, p)
}

// ── token response / discovery / jwks ────────────────────────────────────────

// TestOIDCSec_MissingIDToken: a token response with no id_token is rejected.
func TestOIDCSec_MissingIDToken(t *testing.T) {
	f := newFakeIdP(t)
	f.idMissing = true
	p := newTestProvider(t, f)
	mustFailExchange(t, p)
}

// TestOIDCSec_DiscoveryIssuerMismatch: a discovery doc whose issuer field
// differs from the configured issuer is rejected (issuer-spoofing guard).
func TestOIDCSec_DiscoveryIssuerMismatch(t *testing.T) {
	f := newFakeIdP(t)
	f.discoveryIssuer = "https://attacker.example"
	p := newTestProvider(t, f)
	mustFailExchange(t, p)
}

// TestOIDCSec_WeakRSAKey: a sub-2048-bit RSA key in the JWKS is rejected at
// parse time; with no other usable key, verification fails.
func TestOIDCSec_WeakRSAKey(t *testing.T) {
	f := newFakeIdP(t)
	weak := mustRSAKey(t, 1024)
	f.jwksFn = func(int) []map[string]any {
		return []map[string]any{rsaJWKMap(f.rsaKID, &weak.PublicKey)}
	}
	p := newTestProvider(t, f)
	mustFailExchange(t, p)
}

// TestOIDCSec_UnknownKidRotationSpent: an unknown kid triggers one forced
// refetch; a second attempt within the rate-limit window is suppressed (no
// further JWKS hit) and still rejected.
func TestOIDCSec_UnknownKidRotationSpent(t *testing.T) {
	f := newFakeIdP(t)
	// Token claims a kid the JWKS never serves; JWKS only has the real key.
	f.header = map[string]any{"alg": "RS256", "kid": "absent-kid"}
	f.jwksFn = func(int) []map[string]any {
		return []map[string]any{rsaJWKMap(f.rsaKID, &f.rsaKey.PublicKey)}
	}
	p := newTestProvider(t, f)

	if err := mustFailExchange(t, p); err == nil {
		t.Fatal("first exchange should fail")
	}
	afterFirst := f.jwksHit.Load()
	if afterFirst != 2 {
		t.Fatalf("jwks hits after first = %d, want 2 (initial + one rotation)", afterFirst)
	}
	// Second attempt: cache fresh, rotation already spent → no new hit.
	if err := mustFailExchange(t, p); err == nil {
		t.Fatal("second exchange should fail")
	}
	if got := f.jwksHit.Load(); got != 2 {
		t.Fatalf("jwks hits after second = %d, want 2 (refetch suppressed)", got)
	}
}

// TestOIDCSec_EncUseKeyRejected: a JWK marked use:"enc" must not verify
// signatures, even when it is the very key the token was signed with, an
// encryption key repurposed for signing is outside its certified use.
func TestOIDCSec_EncUseKeyRejected(t *testing.T) {
	f := newFakeIdP(t)
	f.jwksFn = func(int) []map[string]any {
		jwk := rsaJWKMap(f.rsaKID, &f.rsaKey.PublicKey)
		jwk["use"] = "enc"
		return []map[string]any{jwk}
	}
	p := newTestProvider(t, f)
	mustFailExchange(t, p)
}

// TestOIDCSec_JWKAlgMismatchRejected: a JWK that declares alg PS256 must not
// verify an RS256 token, the key's own alg binding wins.
func TestOIDCSec_JWKAlgMismatchRejected(t *testing.T) {
	f := newFakeIdP(t)
	f.jwksFn = func(int) []map[string]any {
		jwk := rsaJWKMap(f.rsaKID, &f.rsaKey.PublicKey)
		jwk["alg"] = "PS256"
		return []map[string]any{jwk}
	}
	p := newTestProvider(t, f)
	mustFailExchange(t, p)
}

// TestOIDCSec_DegenerateRSAExponent: e=1 makes PKCS1v15 signatures trivially
// forgeable (sig^1 mod n == sig); such keys are rejected at JWK parse.
func TestOIDCSec_DegenerateRSAExponent(t *testing.T) {
	real := mustRSAKey(t, 2048)
	for _, e := range []int64{0, 1, 2} {
		nB64 := b64u(real.PublicKey.N.Bytes())
		eB64 := b64u(big.NewInt(e).Bytes())
		if _, err := buildRSAKey(nB64, eB64); err == nil {
			t.Errorf("buildRSAKey accepted degenerate exponent %d", e)
		}
	}
}

// ── userinfo ─────────────────────────────────────────────────────────────────

// TestOIDCSec_UserinfoSubMismatch: when userinfo is fetched to fill a missing
// email, a userinfo sub that differs from the id_token sub is rejected.
func TestOIDCSec_UserinfoSubMismatch(t *testing.T) {
	f := newFakeIdP(t)
	c := baseClaims(f)
	delete(c, "email") // force the userinfo merge path
	f.claims = c
	f.userinfo = map[string]any{"email": "ui@example.com"}
	f.userinfoSub = "different-subject" // != id_token sub "user-123"
	p := newTestProvider(t, f)

	tok, err := p.ExchangeCode(ctxBg(), "any-code")
	if err != nil {
		t.Fatalf("ExchangeCode should succeed (id_token valid): %v", err)
	}
	if _, err := p.FetchUserInfo(ctxBg(), tok.AccessToken); err == nil {
		t.Fatal("FetchUserInfo should fail on userinfo sub mismatch")
	}
}

// TestOIDCSec_UserinfoMissingSub: a userinfo response that omits its
// subject entirely must not merge into the id_token identity. The
// mismatch check above only fires when BOTH subs are non-empty, so a
// sub-less userinfo response currently merges its email into the signed
// identity unanchored. OIDC Core 5.3.2 requires the UserInfo response
// to carry sub; "absent" cannot match the id_token subject.
func TestOIDCSec_UserinfoMissingSub(t *testing.T) {
	f := newFakeIdP(t)
	c := baseClaims(f)
	delete(c, "email") // force the userinfo merge path
	f.claims = c
	f.userinfo = map[string]any{"email": "unanchored@example.com"}
	f.omitUserinfoSub = true // the response carries no sub at all
	p := newTestProvider(t, f)

	tok, err := p.ExchangeCode(ctxBg(), "any-code")
	if err != nil {
		t.Fatalf("ExchangeCode should succeed (id_token valid): %v", err)
	}
	if _, err := p.FetchUserInfo(ctxBg(), tok.AccessToken); err == nil {
		t.Fatal("SECURITY: [oidc-userinfo-sub] userinfo response with no subject merged into the id_token identity")
	}
}

// ── information-disclosure guard ─────────────────────────────────────────────

// TestOIDCSec_NoSecretsInErrors: error strings must never contain the raw
// id_token, the client secret, or upstream response bodies.
func TestOIDCSec_NoSecretsInErrors(t *testing.T) {
	checks := []struct {
		name  string
		setup func(f *fakeIdP)
		drive func(t *testing.T, p *OIDCProvider, f *fakeIdP) error
	}{
		{
			name: "alg none",
			setup: func(f *fakeIdP) {
				f.header = map[string]any{"alg": "none"}
				f.sign = func([]byte) []byte { return nil }
			},
			drive: func(t *testing.T, p *OIDCProvider, f *fakeIdP) error { return mustFailExchange(t, p) },
		},
		{
			name: "wrong iss",
			setup: func(f *fakeIdP) {
				c := baseClaims(f)
				c["iss"] = "https://wrong.example"
				f.claims = c
			},
			drive: func(t *testing.T, p *OIDCProvider, f *fakeIdP) error { return mustFailExchange(t, p) },
		},
		{
			name:  "discovery mismatch",
			setup: func(f *fakeIdP) { f.discoveryIssuer = "https://attacker.example" },
			drive: func(t *testing.T, p *OIDCProvider, f *fakeIdP) error { return mustFailExchange(t, p) },
		},
	}

	const secret = "test-secret"
	const bodyMarker = "test-access-token"
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeIdP(t)
			tc.setup(f)
			p := newTestProvider(t, f)
			// Mint the exact id_token the handler will serve so we can assert
			// its signature segment never leaks into the error.
			idTok := f.mintIDToken(t)
			sigSegment := strings.Split(idTok, ".")[2]

			err := tc.drive(t, p, f)
			msg := err.Error()
			if strings.Contains(msg, secret) {
				t.Errorf("error leaks client secret: %q", msg)
			}
			if strings.Contains(msg, bodyMarker) {
				t.Errorf("error leaks upstream body value: %q", msg)
			}
			if sigSegment != "" && strings.Contains(msg, sigSegment) {
				t.Errorf("error leaks raw id_token signature segment: %q", msg)
			}
		})
	}
}

// ── issuer scheme / loopback gate ────────────────────────────────────────────

// TestOIDCHttpIssuerOnlyLiteralLoopback pins that a plain-http Issuer is
// accepted ONLY for literal loopback hostnames. isLocalhostHost is a
// literal string switch, so hosts that merely RESOLVE to loopback
// (localtest.me → 127.0.0.1), other loopback /8 addresses, DNS names
// suffixed with localhost, and IPv4-mapped IPv6 must all be rejected —
// otherwise any DNS rebinding of "resolves-to-loopback" name downgrades
// the issuer to cleartext transport.
func TestOIDCHttpIssuerOnlyLiteralLoopback(t *testing.T) {
	reject := []string{
		"http://localtest.me",       // public DNS name resolving to 127.0.0.1
		"http://127.0.0.2",          // different loopback /8 address
		"http://localhost.evil.com", // attacker domain ending in localhost
		"http://[::ffff:127.0.0.1]", // IPv4-mapped IPv6 loopback
	}
	for _, iss := range reject {
		cfg := OIDCConfig{Issuer: iss, ClientID: "c", ClientSecret: "s", RedirectURL: "r"}
		if _, err := NewOIDCProvider(cfg); err == nil {
			t.Errorf("SECURITY: [oidc-loopback] http issuer %q accepted: only literal loopback hosts may use http", iss)
		}
	}
	accept := []string{
		"http://localhost",
		"http://localhost:8080",
		"http://127.0.0.1:8080",
		"http://[::1]:9999",
	}
	for _, iss := range accept {
		cfg := OIDCConfig{Issuer: iss, ClientID: "c", ClientSecret: "s", RedirectURL: "r"}
		if _, err := NewOIDCProvider(cfg); err != nil {
			t.Errorf("literal loopback issuer %q must be accepted, got %v", iss, err)
		}
	}
}

// TestOIDCNoKidAmbiguousKeyset pins the kid-less lookup's ambiguity rule: a
// header without kid may only be verified against a JWKS advertising
// exactly ONE usable key. With two keys in the set, verification must fail
// closed even if one of them signed the token — picking either key would
// let the other issuer's key vouch for tokens it never signed.
func TestOIDCNoKidAmbiguousKeyset(t *testing.T) {
	f := newFakeIdP(t)
	f.header = map[string]any{"alg": "RS256", "kid": "", "typ": "JWT"} // kid absent
	f.jwksFn = func(int) []map[string]any {
		return []map[string]any{
			rsaJWKMap("rsa-1", &f.rsaKey.PublicKey),
			ecJWKMap("ec-1", &f.ecKey.PublicKey),
		}
	}
	p := newTestProvider(t, f)
	if _, err := p.ExchangeCode(ctxBg(), "any-code"); err == nil {
		t.Fatal("SECURITY: [oidc-kid] a kid-less id_token verified against a TWO-key JWKS; " +
			"lookup must fail closed unless the set is unambiguous")
	}
}

// ── discovery endpoint pinning + credential redirect refusal (hardening) ────
//
// Extends the issuer-pinning family: TestOIDCHttpIssuerOnlyLiteralLoopback
// pins the ISSUER's transport; these pin the endpoints the issuer's
// discovery document hands us, and the redirect posture of the fetches.
// The loopback-http dev posture itself stays pinned by every fakeIdP test
// in this suite (all of them run a literal-loopback http IdP end to end).

// Property: every URL the provider fetches from discovery (token, jwks,
// userinfo) keeps the issuer's transport trust — https from any host, or
// plain http only on a literal-loopback host under a loopback-http issuer —
// and never carries embedded userinfo. Distinct hosts remain allowed:
// OIDC discovery legitimately serves jwks/userinfo from another origin.
// Surfaces: fetchDiscovery's acceptance of each endpoint field, driven
// through its consuming method.
func TestOIDCSec_EndpointDowngradeRefused(t *testing.T) {
	for _, field := range []string{"token_endpoint", "jwks_uri", "userinfo_endpoint"} {
		t.Run(field, func(t *testing.T) {
			f := newFakeIdP(t)
			f.endpointsOverride = map[string]string{field: "http://attacker.example/leak"}
			p := newTestProvider(t, f)
			var err error
			switch field {
			case "token_endpoint", "jwks_uri":
				_, err = p.ExchangeCode(ctxBg(), "code-1")
			case "userinfo_endpoint":
				_, err = p.FetchUserInfo(ctxBg(), "some-access-token")
			}
			if err == nil {
				t.Fatalf("downgraded %s was accepted", field)
			}
			if !strings.Contains(err.Error(), "must be https") {
				t.Fatalf("err = %v, want the discovery endpoint transport-validation error", err)
			}
		})
	}
}

// Property: a credential-bearing provider fetch never follows a redirect.
// Surface: ExchangeCode's token POST — the provider client serves
// discovery/JWKS/userinfo alike, so the posture covers them all. The
// mechanics this pins: Go's client re-sends method and body verbatim on
// 307/308, so client_secret and the authorization code would arrive at
// whatever origin the token endpoint redirects to. 301/302/303 degrade to
// a bodyless GET and are covered by the same refusal.
func TestOIDCSec_TokenRedirectKeepsSecret(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(fmt.Sprintf("status%d", status), func(t *testing.T) {
			var mu sync.Mutex
			var bodies []string
			collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				mu.Lock()
				bodies = append(bodies, fmt.Sprintf("%s %s", r.Method, b))
				mu.Unlock()
				writeJSON(t, w, map[string]any{}) // no id_token: the exchange must fail either way
			}))
			t.Cleanup(collector.Close)

			var idp *httptest.Server
			idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/.well-known/openid-configuration":
					writeJSON(t, w, map[string]any{
						"issuer":                 idp.URL,
						"authorization_endpoint": idp.URL + "/authorize",
						"token_endpoint":         idp.URL + "/token",
						"jwks_uri":               idp.URL + "/jwks",
						"userinfo_endpoint":      idp.URL + "/userinfo",
					})
				case "/token":
					http.Redirect(w, r, collector.URL+"/steal", status)
				default:
					writeJSON(t, w, map[string]any{})
				}
			}))
			t.Cleanup(idp.Close)

			const secret = "super-secret-do-not-forward"
			p, err := NewOIDCProvider(OIDCConfig{
				Issuer:       idp.URL,
				ClientID:     "test-client",
				ClientSecret: secret,
				RedirectURL:  "https://app.example.com/cb",
			})
			if err != nil {
				t.Fatalf("NewOIDCProvider: %v", err)
			}
			_, err = p.ExchangeCode(ctxBg(), "auth-code-1")
			mu.Lock()
			got := strings.Join(bodies, " | ")
			mu.Unlock()
			if strings.Contains(got, secret) || strings.Contains(got, "auth-code-1") {
				t.Fatalf("SECURITY: redirect target received the exchange payload: %s", got)
			}
			if err == nil {
				t.Fatal("exchange must fail: a redirect answer is not a token response")
			}
		})
	}
}

// TestValidateOIDCEndpoint pins the transport rule itself, both issuer
// postures. Under an https issuer NO plain-http endpoint is accepted, not
// even a literal loopback one (that would downgrade below the issuer's own
// transport). Under the loopback-http dev posture only literal loopback
// endpoint hosts ride cleartext — same literal set as the issuer rule, so
// a dev issuer cannot direct traffic at another cleartext host.
func TestValidateOIDCEndpoint(t *testing.T) {
	httpsIssuer := []struct {
		raw  string
		want bool
	}{
		{"https://any.example/token", true},
		{"https://other.origin.example/jwks", true}, // distinct hosts allowed
		{"https://alice:pw@idp.example/token", false},
		{"http://idp.example/token", false},
		{"http://localhost:8080/jwks", false}, // below the issuer's transport
		{"javascript:alert(1)", false},
		{"/relative/path", false},
	}
	for _, tc := range httpsIssuer {
		if err := validateOIDCEndpoint(tc.raw, false); (err == nil) != tc.want {
			t.Errorf("https issuer: %q err=%v, want ok=%v", tc.raw, err, tc.want)
		}
	}
	loopbackIssuer := []struct {
		raw  string
		want bool
	}{
		{"http://localhost:9999/jwks", true},
		{"http://127.0.0.1:9999/token", true},
		{"http://[::1]:9999/userinfo", true},
		{"http://127.0.0.2:9999/jwks", false}, // non-literal loopback
		{"http://intra.example/token", false}, // cleartext, non-loopback
	}
	for _, tc := range loopbackIssuer {
		if err := validateOIDCEndpoint(tc.raw, true); (err == nil) != tc.want {
			t.Errorf("loopback-http issuer: %q err=%v, want ok=%v", tc.raw, err, tc.want)
		}
	}
}
