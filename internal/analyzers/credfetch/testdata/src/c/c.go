// Package c holds credfetch positives with no counterpart in this
// repo: a freight API with different identifiers and spellings, and the
// harness-provider shapes (a conditional default literal and a
// default-client function hop) reduced from
// framework/experimental/harness/provider.
package c

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// craneHTTP is the freight API's shared client: unset.
var craneHTTP = &http.Client{Timeout: 4e9} // want `no CheckRedirect on a credential-bearing fetch`

// CraneClient talks to a shipping API.
type CraneClient struct {
	apiKey   string
	http     *http.Client
	tokenURL string
}

func newCrane(key string) *CraneClient {
	return &CraneClient{apiKey: key, http: craneHTTP, tokenURL: "https://crane.example/oauth/token"}
}

// mint posts the api_key form (composite-literal spelling) to the
// tokenURL field and reads the answer whole: the redirect finding lands
// on the shared client's construction, the cap finding on the ReadAll.
func (c *CraneClient) mint() error {
	form := url.Values{"api_key": {c.apiKey}}
	req, _ := http.NewRequest(http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body) // want `decoded with no size bound`
	if err != nil {
		return err
	}
	_ = body
	return nil
}

// fetchLedger puts the credential in the Authorization header of a
// local literal client; the body is capped, so only the redirect fires.
func fetchLedger(session string) ([]byte, error) {
	c := &http.Client{Timeout: 8e9} // want `no CheckRedirect on a credential-bearing fetch`
	req, _ := http.NewRequest(http.MethodGet, "https://ledger.example/v1", nil)
	req.Header.Set("Authorization", "Bearer "+session)
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// engine is the harness-provider shape: the caller may inject a client,
// but the default this function arms has no CheckRedirect, and every
// request carries the provider key.
type engine struct {
	HTTP *http.Client
	key  string
}

func (e *engine) complete() error {
	client := e.HTTP
	if client == nil {
		client = &http.Client{Timeout: 60e9} // want `no CheckRedirect on a credential-bearing fetch`
	}
	req, _ := http.NewRequest(http.MethodPost, "https://engine.example/v1/chat", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+e.key)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(&struct{}{}) // want `decoded with no size bound`
}

// defaultEngineHTTP is the zai.go shape: the unset literal lives behind
// a function hop, and the finding lands on the literal.
func defaultEngineHTTP() *http.Client {
	return &http.Client{Timeout: 30e9} // want `no CheckRedirect on a credential-bearing fetch`
}

func pushStatus(key string) error {
	req, _ := http.NewRequest(http.MethodPost, "https://status.example/report", strings.NewReader("x=1"))
	req.Header.Set("X-API-Key", key)
	resp, err := defaultEngineHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&struct{}{})
}
