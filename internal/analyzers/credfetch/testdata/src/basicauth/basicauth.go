// Package basicauth: req.SetBasicAuth writes the Authorization header,
// the same credential a Header.Set("Authorization", …) carries, so a
// client with no CheckRedirect handing it to Do is the same finding
// (2026-11-02 guard bash: the rule read only Header writes).
package basicauth

import (
	"io"
	"net/http"
)

func fetch(user, pass string) ([]byte, error) {
	c := &http.Client{Timeout: 8e9} // want `no CheckRedirect on a credential-bearing fetch`
	req, _ := http.NewRequest(http.MethodGet, "https://ledger.example/v1", nil)
	req.SetBasicAuth(user, pass)
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// Silent: the client refuses redirects.
func fetchGuarded(user, pass string) ([]byte, error) {
	c := &http.Client{Timeout: 8e9, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, _ := http.NewRequest(http.MethodGet, "https://ledger.example/v1", nil)
	req.SetBasicAuth(user, pass)
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}
