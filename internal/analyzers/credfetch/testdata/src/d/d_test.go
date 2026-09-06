package d

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestMintAgainstStubServer is the identical shape in a _test.go file:
// an unfenced client, an api_key form, an unbounded decode. A test
// client talking to a test server carries a fixture, not a credential
// — credfetch must stay quiet on all of it.
func TestMintAgainstStubServer(t *testing.T) {
	form := url.Values{}
	form.Set("api_key", "fixture-key")
	req, err := http.NewRequest(http.MethodPost, "https://stub.example/grant", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	stubClient := &http.Client{Timeout: 200 * time.Millisecond}
	resp, err := stubClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	_ = out
}

// TestBadgeOverrideWithFencedClient writes a provably guarded client
// to the production field from a test file. If credfetch ever let
// _test.go knowledge into its package state, fencedCopy's return
// would flip Badge.httpClient to guarded and silence the Mint
// findings d.go demands — the `want` comments there are the
// tripwire.
func TestBadgeOverrideWithFencedClient(t *testing.T) {
	b := newBadge()
	b.httpClient = fencedCopy(&http.Client{Timeout: 10e9})
	if _, err := b.Mint("fixture-key"); err != nil {
		t.Fatal(err)
	}
}

// fencedCopy is the oidcNoRedirect shape declared inside a test file.
func fencedCopy(c *http.Client) *http.Client {
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return c
}
