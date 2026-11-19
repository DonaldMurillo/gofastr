package webmcp

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core/router"
	corestatic "github.com/DonaldMurillo/gofastr/core/static"
)

// MountOption customizes one Mount call.
type MountOption func(*mountConfig)

type mountConfig struct {
	assetMW     []router.Middleware
	private     bool
	pageScope   func(*http.Request) bool
	bridgeDebug bool
	docScope    func(path string) bool
}

// WithBridgeDebug exposes window.__gofastrWebMCP with registration counts
// and the last call status, never inputs, headers or URLs.
func WithBridgeDebug() MountOption {
	return func(c *mountConfig) { c.bridgeDebug = true }
}

// WithAssetAuthorization wraps the script, manifest and instructions routes
// with mw, outermost first, and makes them private.
func WithAssetAuthorization(mw ...router.Middleware) MountOption {
	return func(c *mountConfig) { c.assetMW = append(c.assetMW, mw...) }
}

// WithPrivateAssets serves the assets under Cache-Control: private, no-store.
func WithPrivateAssets() MountOption {
	return func(c *mountConfig) { c.private = true }
}

// WithPageScope serves an empty asset to requests include rejects, and makes
// the assets private.
func WithPageScope(include func(*http.Request) bool) MountOption {
	return func(c *mountConfig) {
		c.pageScope = include
	}
}

func (c *mountConfig) requesterDependent() bool {
	return c.private || c.pageScope != nil || len(c.assetMW) > 0
}

// WithDocumentScope ships the bridge only on pages whose route pattern
// include accepts, with a full document load across that edge.
func WithDocumentScope(include func(path string) bool) MountOption {
	return func(c *mountConfig) {
		c.docScope = include
	}
}

func (c *mountConfig) wrap(h http.Handler, emptyContentType, emptyBody string) http.Handler {
	if c.pageScope != nil {
		h = scopedAsset(c.pageScope, emptyContentType, emptyBody, h)
	}
	for i := len(c.assetMW) - 1; i >= 0; i-- {
		h = c.assetMW[i](h)
	}
	return h
}

func scopedAsset(include func(*http.Request) bool, emptyContentType, emptyBody string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if include(r) {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Type", emptyContentType)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "private, no-store")
		h.Set("Content-Length", strconv.Itoa(len(emptyBody)))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte(emptyBody))
		}
	})
}

func privateTextHandler(contentType, body, etag string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Type", contentType)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("ETag", etag)
		h.Set("Cache-Control", "private, no-store")
		if corestatic.IfNoneMatch(r, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		h.Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write([]byte(body))
	})
}

func privateETag(body []byte) string {
	return fmt.Sprintf("%q", hashHex(body))
}
