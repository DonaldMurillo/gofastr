package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AppleIssuer is Sign in with Apple's issuer.
const AppleIssuer = "https://appleid.apple.com"

// AppleConfig configures an AppleProvider from the Apple developer
// account's Services ID, team and Sign in with Apple key.
type AppleConfig struct {
	// ClientID is the Services ID, e.g. "com.example.web". Required.
	ClientID string
	// TeamID is the 10-character team identifier. Required.
	TeamID string
	// KeyID is the private key's 10-character identifier. Required.
	KeyID string
	// PrivateKey is the contents of the key's .p8 file. Required.
	PrivateKey []byte
	// RedirectURL is the callback registered for the Services ID,
	// ending /oauth/apple/callback. Required.
	RedirectURL string
	// Issuer overrides AppleIssuer. Only tests set it.
	Issuer string
	// HTTPClient overrides the default 10s-deadline client.
	HTTPClient *http.Client
	// DisablePKCE stops the PKCE challenge and verifier.
	DisablePKCE bool
}

// AppleProvider is Sign in with Apple: an OIDCProvider that signs its
// client secret and takes a form-post callback.
type AppleProvider struct {
	*OIDCProvider
}

// NewAppleProvider validates cfg and parses the key. It does no network I/O.
func NewAppleProvider(cfg AppleConfig) (*AppleProvider, error) {
	switch {
	case cfg.ClientID == "":
		return nil, errors.New("apple: ClientID (the Services ID) is required")
	case cfg.TeamID == "":
		return nil, errors.New("apple: TeamID is required")
	case cfg.KeyID == "":
		return nil, errors.New("apple: KeyID is required")
	case cfg.RedirectURL == "":
		return nil, errors.New("apple: RedirectURL is required")
	}
	key, err := parseAppleKey(cfg.PrivateKey)
	if err != nil {
		return nil, err
	}
	issuer := cfg.Issuer
	if issuer == "" {
		issuer = AppleIssuer
	}
	inner, err := NewOIDCProvider(OIDCConfig{
		Issuer:          issuer,
		ClientID:        cfg.ClientID,
		ClientSecret:    "signed-per-exchange",
		RedirectURL:     cfg.RedirectURL,
		ProviderName:    "apple",
		Scopes:          []string{"openid", "email"},
		HTTPClient:      cfg.HTTPClient,
		TokenAuthMethod: OIDCAuthClientSecretPost,
		DisablePKCE:     cfg.DisablePKCE,
	})
	if err != nil {
		return nil, err
	}
	aud := normalizeIssuer(issuer)
	inner.clientSecret = func() (string, error) {
		return appleClientSecret(key, cfg.KeyID, cfg.TeamID, cfg.ClientID, aud, time.Now())
	}
	return &AppleProvider{OIDCProvider: inner}, nil
}

// AuthURL implements OAuth2Provider.
func (a *AppleProvider) AuthURL(state string) string { return a.AuthURLWithChallenge(state, "") }

// AuthURLWithChallenge implements OAuth2PKCEProvider.
func (a *AppleProvider) AuthURLWithChallenge(state, challenge string) string {
	raw := a.OIDCProvider.AuthURLWithChallenge(state, challenge)
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("response_mode", "form_post")
	u.RawQuery = q.Encode()
	return u.String()
}

// CallbackIsFormPost implements OAuth2FormPostProvider.
func (a *AppleProvider) CallbackIsFormPost() bool { return true }

func parseAppleKey(b []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("apple: PrivateKey is not PEM: pass the .p8 file's contents")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("apple: PrivateKey is not a PKCS #8 key: pass the .p8 file's contents")
	}
	ec, ok := k.(*ecdsa.PrivateKey)
	if !ok || ec.Curve != elliptic.P256() {
		return nil, errors.New("apple: PrivateKey must be a P-256 key")
	}
	return ec, nil
}

const appleClientSecretTTL = 5 * time.Minute

func appleClientSecret(key *ecdsa.PrivateKey, keyID, teamID, clientID, aud string, now time.Time) (string, error) {
	enc := base64.RawURLEncoding
	header, err := json.Marshal(map[string]string{"alg": "ES256", "kid": keyID})
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(map[string]any{
		"iss": teamID,
		"iat": now.Unix(),
		"exp": now.Add(appleClientSecretTTL).Unix(),
		"aud": aud,
		"sub": clientID,
	})
	if err != nil {
		return "", err
	}
	signing := enc.EncodeToString(header) + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + enc.EncodeToString(sig), nil
}

// OAuth2FormPostProvider is implemented by a provider whose callback is
// a form POST. The plugin then also mounts
// POST {basePath}/oauth/{provider}/callback.
type OAuth2FormPostProvider interface {
	OAuth2Provider
	CallbackIsFormPost() bool
}

func isFormPostCallback(basePath, path string) bool {
	rest, ok := strings.CutPrefix(path, basePath+"/oauth/")
	if !ok {
		return false
	}
	name, ok := strings.CutSuffix(rest, "/callback")
	return ok && name != "" && !strings.Contains(name, "/")
}
