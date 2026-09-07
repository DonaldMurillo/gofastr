// Package clientctxok pins the credit postures of the bare-client
// arm: a per-call context deadline (WithTimeout/WithDeadline plus
// http.NewRequestWithContext in the same function) and a file-level
// deadline convention both keep the zero-timeout spellings quiet.
package clientctxok

import (
	"context"
	"net/http"
	"time"
)

// deadlined is evalrunner/mcpprobe's shape: DefaultClient.Do under a
// 15s request context. Quiet.
func deadlined(ctx context.Context, url string) (*http.Response, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

// fileConvention: the file deadlines its calls, so the sugar in the
// next function rides the same convention the literal arm reads.
func deadlinedFetch(ctx context.Context, url string) (*http.Response, error) {
	reqCtx, cancel := context.WithDeadline(ctx, time.Now().Add(5*time.Second))
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

// deadlineNeighbor reads the same file-level credit.
func deadlineNeighbor(url string) bool {
	resp, err := http.Get(url)
	if err != nil {
		return true
	}
	resp.Body.Close()
	return false
}
