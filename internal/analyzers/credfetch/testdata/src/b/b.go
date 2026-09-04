// Package b pins credfetch's documented silences: every shape the
// analyzer must NOT report.
package b

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// A composite literal that sets CheckRedirect: the fix posture spelled
// as a key. Used with a credential-bearing request — quiet.
var pinned = &http.Client{
	Timeout: 5e9,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func exchangeGuarded(secret string) error {
	form := url.Values{}
	form.Set("client_secret", secret)
	req, _ := http.NewRequest(http.MethodPost, "https://idp.example/token", strings.NewReader(form.Encode()))
	resp, err := pinned.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&struct{}{})
}

// The webfetch shape: the local's last binding is &copy where
// copy.CheckRedirect was assigned — guarded, and the body is capped.
type tool struct {
	HTTP *http.Client
}

func (t *tool) grab(target, bearer string) error {
	client := t.HTTP
	if client == nil {
		client = &http.Client{Timeout: 2e9}
	}
	safe := *client
	safe.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	client = &safe
	req, _ := http.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return err
}

// A parameter client is unknown: the caller's construction decides.
func forwarded(c *http.Client, endpoint, secret string) error {
	form := url.Values{}
	form.Set("client_secret", secret)
	req, _ := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&struct{}{})
}

// An unset client with no credential on the request: the redirect
// hazard needs the credential, and so does the cap posture.
var plainHTTP = &http.Client{Timeout: 3e9}

func ping() error {
	req, _ := http.NewRequest(http.MethodGet, "https://example.com/health", nil)
	resp, err := plainHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(&struct{}{})
}

// A stream is unbounded by design: only document-shaped reads
// (NewDecoder/ReadAll of the body) report, a scanner does not.
func stream(c *http.Client, bearer string) error {
	req, _ := http.NewRequest(http.MethodGet, "https://engine.example/v1/chat", nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		_ = sc.Bytes()
	}
	return sc.Err()
}

// The sugar helpers build no request this pass can read.
func sugar(secret string) error {
	form := url.Values{}
	form.Set("client_secret", secret)
	resp, err := plainHTTP.PostForm("https://idp.example/token", form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
