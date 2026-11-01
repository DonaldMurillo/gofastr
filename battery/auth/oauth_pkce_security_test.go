package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// Decision 2026-11-01: the server-side OAuth flows send PKCE. Without it a
// stolen authorization code could be redeemed in an attacker's own flow:
// their state and cookie check out, the server redeems the code with its
// own secret, and the attacker is signed in as the victim (RFC 9700 §4.5).

// pkceIdP is an OAuth2PKCEProvider that binds each issued code to the
// challenge of the flow that asked for it and checks the verifier at the
// exchange, as an IdP enforcing PKCE does.
type pkceIdP struct {
	mu        sync.Mutex
	challenge map[string]string // state -> challenge seen at authorize
	codes     map[string]string // code -> challenge it is bound to
	user      *OAuth2UserInfo
}

func (m *pkceIdP) Name() string                { return "pkce" }
func (m *pkceIdP) AuthURL(state string) string { return m.AuthURLWithChallenge(state, "") }
func (m *pkceIdP) ExchangeCode(ctx context.Context, code string) (*OAuth2Token, error) {
	return m.ExchangeCodeWithVerifier(ctx, code, "")
}
func (m *pkceIdP) FetchUserInfo(context.Context, string) (*OAuth2UserInfo, error) { return m.user, nil }

func (m *pkceIdP) AuthURLWithChallenge(state, challenge string) string {
	m.mu.Lock()
	m.challenge[state] = challenge
	m.mu.Unlock()
	return "https://idp.example/authorize?state=" + url.QueryEscape(state)
}

// issue is the IdP redirecting the user who approved flow state back with code.
func (m *pkceIdP) issue(state, code string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.codes[code] = m.challenge[state]
}

func (m *pkceIdP) ExchangeCodeWithVerifier(_ context.Context, code, verifier string) (*OAuth2Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	bound, ok := m.codes[code]
	if !ok || bound == "" || pkceChallenge(verifier) != bound {
		return nil, errors.New("invalid_grant: code_verifier does not match")
	}
	return &OAuth2Token{AccessToken: "at-" + code}, nil
}

func TestOAuthStolenCodeDoesNotRedeemInAnotherFlow(t *testing.T) {
	idp := &pkceIdP{challenge: map[string]string{}, codes: map[string]string{},
		user: &OAuth2UserInfo{ID: "victim-sub", Email: "victim@example.com", EmailVerified: true}}
	mgr, _ := newOAuth2Manager(t, idp)
	r := mountOAuth2Routes(mgr)
	start := func() (string, *http.Cookie) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/oauth/pkce", nil))
		loc := w.Header().Get("Location")
		state, _ := url.QueryUnescape(loc[strings.LastIndex(loc, "state=")+len("state="):])
		for _, c := range w.Result().Cookies() {
			if c.Name == oauthStateCookie {
				return state, c
			}
		}
		t.Fatal("no state cookie")
		return "", nil
	}
	callback := func(state, code string, ck *http.Cookie) int {
		req := httptest.NewRequest(http.MethodGet, "/auth/oauth/pkce/callback?code="+code+"&state="+url.QueryEscape(state), nil)
		req.AddCookie(ck)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	victimState, victimCookie := start()
	idp.issue(victimState, "victim-code") // the code that leaks
	attackerState, attackerCookie := start()
	if code := callback(attackerState, "victim-code", attackerCookie); code == http.StatusFound {
		t.Fatalf("SECURITY: a code issued to the victim's flow was redeemed in the attacker's flow (%d)", code)
	}
	// Control: the victim's own flow redeems it.
	if code := callback(victimState, "victim-code", victimCookie); code != http.StatusFound {
		t.Errorf("the victim's own callback = %d, want 302", code)
	}
}

// The built-in providers send the S256 challenge and the verifier, and
// DisablePKCE stops both.
func TestBuiltInProvidersSendPKCE(t *testing.T) {
	var gotVerifier string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotVerifier = r.PostForm.Get("code_verifier")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"a"}`))
	}))
	t.Cleanup(srv.Close)
	gh := NewGitHubProvider("id", "secret", "https://app.example/cb")
	gh.tokenEndpoint, gh.httpClient = srv.URL, srv.Client()
	g := NewGoogleProvider("id", "secret", "https://app.example/cb")
	g.tokenEndpoint, g.httpClient = srv.URL, srv.Client()
	for name, p := range map[string]OAuth2PKCEProvider{"github": gh, "google": g} {
		u, _ := url.Parse(p.AuthURLWithChallenge("st", "chal"))
		if u.Query().Get("code_challenge") != "chal" || u.Query().Get("code_challenge_method") != "S256" {
			t.Errorf("%s: authorize URL lacks the S256 challenge: %s", name, u)
		}
		if _, err := p.ExchangeCodeWithVerifier(context.Background(), "c", "ver"); err != nil {
			t.Fatal(err)
		}
		if gotVerifier != "ver" {
			t.Errorf("%s: token request code_verifier = %q, want ver", name, gotVerifier)
		}
	}
	gh.DisablePKCE, g.DisablePKCE = true, true
	for name, p := range map[string]OAuth2PKCEProvider{"github": gh, "google": g} {
		if u, _ := url.Parse(p.AuthURLWithChallenge("st", "chal")); u.Query().Has("code_challenge") {
			t.Errorf("%s: DisablePKCE still sends a challenge", name)
		}
		gotVerifier = "unset"
		if _, err := p.ExchangeCodeWithVerifier(context.Background(), "c", "ver"); err != nil {
			t.Fatal(err)
		}
		if gotVerifier != "" {
			t.Errorf("%s: DisablePKCE still sends a verifier", name)
		}
	}
}

func TestPKCEVerifierShape(t *testing.T) {
	p := NewOAuth2Plugin(OAuth2Config{StateSecret: "test-state-secret-01"})
	v := p.pkceVerifier("some.state")
	if len(v) < 43 || len(v) > 128 || strings.Trim(v, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~") != "" {
		t.Errorf("verifier %q is not an RFC 7636 code_verifier", v)
	}
	if v == p.pkceVerifier("other.state") {
		t.Error("two states share a verifier")
	}
	if NewOAuth2Plugin(OAuth2Config{StateSecret: "another-secret-0001"}).pkceVerifier("some.state") == v {
		t.Error("the verifier does not depend on the state key")
	}
}
