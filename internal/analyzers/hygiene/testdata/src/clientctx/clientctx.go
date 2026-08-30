package clientctx

import (
	"context"
	"net/http"
	"time"
)

// Every call is deadlined, so Client.Timeout would be redundant. Not flagged.
func fetch(ctx context.Context, u string) error {
	c := &http.Client{Transport: http.DefaultTransport}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}
