// Package d pins the 2026-09-04 test-file posture: the identical
// credential-bearing fetch fires in this file and stays quiet in
// d_test.go. The identifiers are deliberately nothing like
// battery/auth's — the shape, not the site.
package d

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// leaseHTTP is the unfenced shape: a Timeout, no CheckRedirect.
var leaseHTTP = &http.Client{Timeout: 10e9} // want `no CheckRedirect on a credential-bearing fetch`

// Badge mints session badges from an OAuth-shaped grant endpoint.
type Badge struct {
	httpClient    *http.Client
	grantEndpoint string
}

func newBadge() *Badge {
	return &Badge{
		httpClient:    leaseHTTP,
		grantEndpoint: "https://badge.example/grant",
	}
}

// Mint posts the api_key form through the unfenced client and decodes
// the answer unbounded: both postures fire.
func (b *Badge) Mint(apiKey string) (map[string]any, error) {
	form := url.Values{}
	form.Set("api_key", apiKey)
	req, err := http.NewRequest(http.MethodPost, b.grantEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil { // want `decoded with no size bound`
		return nil, err
	}
	return out, nil
}
