package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
)

// MaxArchiveSize is the absolute cap on any archive download (256 MiB)
// regardless of what the feed declares.
const MaxArchiveSize = 256 << 20

// ErrBadURL is the sentinel for a feed or archive URL outside the
// allowed scheme rule: https, or http only to 127.0.0.1 (tests).
var ErrBadURL = errors.New("update URL must be https (plain http only to 127.0.0.1)")

// ErrFetch is the sentinel for a failed download; the detail stays in
// the wrapped error for the server log, never for the page.
var ErrFetch = errors.New("update download failed")

// ErrTooLarge is the sentinel for a body over its cap.
var ErrTooLarge = errors.New("update download exceeded its size cap")

// AllowedURL reports whether raw may be fetched: scheme https, or
// scheme http with host exactly 127.0.0.1.
func AllowedURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "https":
		return u.Host != ""
	case "http":
		return u.Hostname() == "127.0.0.1"
	}
	return false
}

// Fetch downloads raw, capped at max bytes: the body is read through a
// limit of max+1 so one byte over the cap is detectable.
func Fetch(ctx context.Context, client *http.Client, raw string, max int64) ([]byte, error) {
	if !AllowedURL(raw) {
		return nil, ErrBadURL
	}
	if max <= 0 || max > MaxArchiveSize {
		return nil, ErrTooLarge
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: building the request failed", ErrFetch)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFetch, ctxErrDetail(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrFetch, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, fmt.Errorf("%w: reading the body failed", ErrFetch)
	}
	if int64(len(body)) > max {
		return nil, ErrTooLarge
	}
	return body, nil
}

// ctxErrDetail reduces a transport error to a fixed, url-free detail.
// The URL itself is never part of an error that could reach a log
// line; the status class is enough to debug a broken feed host.
func ctxErrDetail(err error) string {
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	return "transport error"
}

// PlatformKey is the feed's platform key for the running process
// ("darwin-arm64").
func PlatformKey() string { return runtime.GOOS + "-" + runtime.GOARCH }
