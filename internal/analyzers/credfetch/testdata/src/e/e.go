// Package e is the --from-openapi CLI's self-contained client reduced
// (cmd/gofastr/internal/appgen renderCLISelfClient): the credential
// header is a named constant, the default client is armed inside the
// constructor, and the request goes out through the struct field.
package e

import (
	"context"
	"net/http"
	"time"
)

const (
	tokenHeader = "Authorization"
	tokenPrefix = "Bearer "
)

// petHeader is a typed constant naming a credential header.
const petHeader string = "X-API-Key"

type Client struct {
	BaseURL string
	HTTP    *http.Client
	Token   string
}

func NewClient(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second} // want `no CheckRedirect on a credential-bearing fetch`
	}
	return &Client{BaseURL: baseURL, HTTP: httpClient}
}

func (c *Client) DoRaw(ctx context.Context, method, path string) error {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	if c.Token != "" {
		req.Header.Set(tokenHeader, tokenPrefix+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

type keyed struct {
	HTTP *http.Client
	key  string
}

func newKeyed(key string) *keyed {
	return &keyed{HTTP: &http.Client{Timeout: 5 * time.Second}, key: key} // want `no CheckRedirect on a credential-bearing fetch`
}

func (k *keyed) get(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://pets.example/pet", nil)
	if err != nil {
		return err
	}
	req.Header.Set(petHeader, k.key)
	resp, err := k.HTTP.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// safeClient is the fixed spelling of Client: quiet.
type safeClient struct {
	HTTP  *http.Client
	Token string
}

func newSafeClient(httpClient *http.Client) *safeClient {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &safeClient{HTTP: httpClient}
}

func (c *safeClient) get(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://pets.example/pet", nil)
	if err != nil {
		return err
	}
	req.Header.Set(tokenHeader, tokenPrefix+c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// plainHeader is a constant naming no credential: quiet.
const plainHeader = "Accept"

func (k *keyed) accept(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://pets.example/pet", nil)
	if err != nil {
		return err
	}
	req.Header.Set(plainHeader, "application/json")
	resp, err := k.HTTP.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}
