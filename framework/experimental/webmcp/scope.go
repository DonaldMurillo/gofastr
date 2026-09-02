package webmcp

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core/router"
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

// WithBridgeDebug bakes the bridge's bounded debug state into the
// served script: window.__gofastrWebMCP = {supported, attempted,
// registered, failed, lastStatus}. It carries feature support,
// registration counts and names of failed registrations, and the last
// invocation status — never inputs, headers, or URLs — so a support
// console can answer "did the browser even register the tools?" from
// the page. Off by default; it adds nothing to a normal mount.
func WithBridgeDebug() MountOption {
	return func(c *mountConfig) { c.bridgeDebug = true }
}

// WithAssetAuthorization wraps the framework-owned asset routes (the
// bridge script and the manifest) with mw, outermost first (the same
// order Router.Use applies). The routes stay registered by Mount — the
// app no longer has to intercept responses to put authorization in
// front of them.
//
// Authorization must cover BOTH assets: the manifest names every tool
// and its endpoint, so discovery is an authority surface even when the
// endpoints themselves check permissions. The WebMCP marker header
// attributes a call; it never grants authorization.
//
// Mounting with this option (or WithPageScope) serves requester-
// dependent assets, so Mount forces the private no-store cache policy
// for them: a shared cache must never be able to replay an
// authenticated script or manifest to anonymous traffic. WithPrivateAssets
// is then implied, and still fine to pass for clarity.
func WithAssetAuthorization(mw ...router.Middleware) MountOption {
	return func(c *mountConfig) { c.assetMW = append(c.assetMW, mw...) }
}

// WithPrivateAssets serves the bridge script and manifest under
// `Cache-Control: private, no-store` instead of the default policy
// (immutable, hash-versioned script; no-cache manifest). Use it
// whenever the assets are credential-gated (authorization middleware,
// page scope) or otherwise requester-dependent. It is implied by
// WithAssetAuthorization and WithPageScope; passing it alongside them
// is harmless.
func WithPrivateAssets() MountOption {
	return func(c *mountConfig) { c.private = true }
}

// WithPageScope gates the framework-owned asset routes on include(r):
// a request for the script or manifest that fails the predicate gets
// an empty response carrying no tool bytes, so a browser outside the
// scope (a public landing page whose HTML still references the script
// URL, a direct fetch) discovers nothing.
//
// The predicate runs on the ASSET request, not the page render: it can
// see request identity (session, role, headers) but not the page path,
// because the fetch's URL is the asset's own path. Scope on identity.
// To keep the script tag out of out-of-scope pages entirely, pass a
// nil registrar and render the returned script URL only on pages that
// need it; WithPageScope then acts as defense in depth for direct
// asset requests.
//
// Page inclusion never substitutes for HTTP authorization: keep
// WithAssetAuthorization (or endpoint auth) alongside it. Mounting
// with this option forces the private no-store cache policy, since the
// same URL now serves different bytes to different requesters.
func WithPageScope(include func(*http.Request) bool) MountOption {
	return func(c *mountConfig) {
		c.pageScope = include
	}
}

// requesterDependent reports whether this mount's assets can differ
// per requester (auth-wrapped or scope-gated), which requires the
// private no-store cache policy.
func (c *mountConfig) requesterDependent() bool {
	return c.private || c.pageScope != nil || len(c.assetMW) > 0
}

// WithDocumentScope gives the bridge a document lifetime on the host's
// script rail (uihost's RegisterDocumentScript): the bridge tag ships
// only on pages the scope predicate accepts — every other page omits it
// entirely — and the host's SPA runtime performs a real document load
// when a navigation enters or leaves that page set.
//
// The reason is a browser fact, not a policy: removing the bridge tag
// does not unregister tools already installed on
// navigator.modelContext, and a partial (SPA) swap never runs a body
// script. A landing page that soft-swapped over a support page would
// keep the support tools alive in the same document. The hard boundary
// is the safe fallback while WebMCP remains experimental; it also means
// back/forward across the edge loads the destination fresh, so a
// document only ever carries its own tools.
//
// The predicate takes the registered route pattern ("/session/:id"),
// the same value at render time and when the manifest is built, so a
// route cannot be in scope on one side and out on the other.
//
// Page scope is structural, not authorization: it decides which
// DOCUMENT carries the bridge, never who may fetch the assets. Keep
// WithAssetAuthorization (or endpoint auth) for that; WithPageScope
// remains the per-request gate. This option requires the registrar to
// implement the document-script seam (a *uihost.UIHost does) and
// refuses a nil registrar, since the host must know the scope to
// enforce the boundary.
func WithDocumentScope(include func(path string) bool) MountOption {
	return func(c *mountConfig) {
		c.docScope = include
	}
}

// wrap composes one asset's handler for registration: the
// authorization middleware outermost, so an anonymous or wrong-role
// request fails with the middleware's 401/403 before the scope
// predicate is consulted (page inclusion never substitutes for HTTP
// authorization); then the page-scope gate; then the asset itself.
func (c *mountConfig) wrap(h http.Handler, emptyContentType, emptyBody string) http.Handler {
	if c.pageScope != nil {
		h = scopedAsset(c.pageScope, emptyContentType, emptyBody, h)
	}
	for i := len(c.assetMW) - 1; i >= 0; i-- {
		h = c.assetMW[i](h)
	}
	return h
}

// scopedAsset answers requests that fail the scope predicate with
// emptyBody under a private no-store policy: no tool bytes, nothing a
// shared cache may retain, no console error on pages whose HTML still
// carries the script tag, and for the JSON assets a document a client
// can still parse (an empty tool set, empty instructions).
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

// privateTextHandler serves a fixed text asset with a strong ETag and
// 304 revalidation under `Cache-Control: private, no-store`: browser
// caches may hold it, shared caches may not, and nothing outlives the
// credential that earned it. The default (public) script and manifest
// policies stay in uihost.ScriptHandler and manifestHandler; this is
// the private variant Mount swaps in for requester-dependent assets.
func privateTextHandler(contentType, body, etag string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Type", contentType)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("ETag", etag)
		h.Set("Cache-Control", "private, no-store")
		if r.Header.Get("If-None-Match") == etag {
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

// privateETag returns the strong ETag webmcp uses for privately served
// assets. It is a content hash, so it changes exactly when the bytes do.
func privateETag(body []byte) string {
	return fmt.Sprintf("%q", hashHex(body))
}
