// Package a is the credfetch fixture reduced from the bug site:
// battery/auth/oauth2.go as it is today (probes
// TestProviderFetchRefusesRedirect and TestProviderResponseBodiesCapped,
// 2026-09-04 red round, no fix applied) with the battery/auth/oidc.go
// fix posture — the noRedirect wrapper and the capped reads — sitting
// beside it under a DIFFERENT type with the same httpClient field name,
// which is what pins the (struct type, field) keying.
package a

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// defaultHTTP is the oauth2.go shape: a Timeout but no CheckRedirect.
var defaultHTTP = &http.Client{Timeout: 10e9} // want `no CheckRedirect on a credential-bearing fetch`

// Token is the decoded exchange response.
type Token struct {
	Access string `json:"access_token"`
}

// IdP is the reduced GoogleProvider/GitHubProvider.
type IdP struct {
	id            string
	secret        string
	httpClient    *http.Client
	tokenEndpoint string
}

func newIdP(id, secret string) *IdP {
	return &IdP{id: id, secret: secret, httpClient: defaultHTTP, tokenEndpoint: "https://idp.example/token"}
}

// Exchange, as shipped: the client_secret+code form posts through the
// unfenced client, and the answer decodes unbounded.
func (p *IdP) Exchange(code string) (*Token, error) {
	form := url.Values{}
	form.Set("code", code)
	form.Set("client_secret", p.secret)
	req, err := http.NewRequest(http.MethodPost, p.tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var tok Token
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil { // want `decoded with no size bound`
		return nil, err
	}
	return &tok, nil
}

// noRedirect is battery/auth/oidc.go's oidcNoRedirect verbatim in
// shape: a shallow copy that answers redirects as final responses.
func noRedirect(c *http.Client) *http.Client {
	cp := *c
	cp.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &cp
}

// FederatedIdP carries the SAME httpClient field name as IdP, but its
// writer routes the client through noRedirect: the field is guarded,
// and a name-only key would have condemned or silenced both.
type FederatedIdP struct {
	httpClient *http.Client
	issuer     string
}

func newFederated(issuer string, base *http.Client) *FederatedIdP {
	hc := base
	if hc == nil {
		hc = defaultHTTP
	}
	hc = noRedirect(hc)
	return &FederatedIdP{httpClient: hc, issuer: issuer}
}

// Token is the oidc.go posture: credential form, guarded client, and
// every body read through io.LimitReader — all quiet.
func (f *FederatedIdP) Token(secret string) (*Token, error) {
	form := url.Values{}
	form.Set("client_secret", secret)
	req, err := http.NewRequest(http.MethodPost, f.issuer+"/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var tok Token
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tok); err != nil {
		return nil, err
	}
	return &tok, nil
}
