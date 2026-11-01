package openapi

import (
	"encoding/json"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// Handler returns an http.Handler that serves the OpenAPI spec as JSON
// at the root path (typically mounted at /openapi.json or /docs/openapi.json).
//
// The spec lists every registered route, so by default the handler
// requires an authenticated context (the framework's auth chain must
// have called [handler.SetUser]). Apps that want a public OpenAPI spec
// can wrap [PublicHandler] around the same spec.
//
// When the spec carries a RequestView (the framework's entity spec
// does), the document is built per request and omits every entity the
// caller holds no read grant for — the same filter /api/llm.md runs.
func Handler(spec *Spec) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if _, ok := handler.GetUser(r.Context()); !ok {
			i18n.HTTPError(w, r, http.StatusUnauthorized, i18n.StatusPhrase(http.StatusUnauthorized))
			return
		}
		serveSpec(w, r, spec)
	})
}

// serveSpec writes the spec's JSON document for THIS request, applying
// RequestView when the spec carries one so a caller without a read
// grant for an entity never sees it in the document. PublicHandler
// deliberately does not route through here: the public opt-in
// (WithPublicOpenAPI) is full disclosure, and its document is identical
// for every caller, so it stays construction-time prebuilt.
func serveSpec(w http.ResponseWriter, r *http.Request, spec *Spec) {
	s := spec
	if spec.RequestView != nil {
		if v := spec.RequestView(r); v != nil {
			s = v.withInheritedInfo(spec)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	data, err := json.MarshalIndent(s.Build(), "", "  ")
	if err != nil {
		data = []byte(`{"error":"failed to marshal spec"}`)
	}
	w.Write(data)
}

// PublicHandler serves the spec without an auth check. Use only when
// the spec is intentionally public. Most apps should prefer [Handler]
// (auth-gated) or [GatedHandler] (custom predicate).
func PublicHandler(spec *Spec) http.Handler {
	doc := spec.Build()
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		data = []byte(`{"error":"failed to marshal spec"}`)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(data)
	})
}

// GatedHandler wraps the OpenAPI spec handler in an authorisation
// predicate. Requests for which allow(r) returns false get 404 (not
// 401/403: leaking that an OpenAPI endpoint exists is itself the
// disclosure we're trying to avoid). 404 keeps the existence of the
// endpoint indistinguishable from a vanilla missing route. The allow
// predicate is the only auth gate. The wrapped spec handler does NOT
// re-check the user context.
//
// Typical use:
//
//	openapi.GatedHandler(spec, func(r *http.Request) bool {
//	    u, ok := handler.GetUser(r.Context())
//	    return ok && u.(*MyUser).IsAdmin()
//	})
func GatedHandler(spec *Spec, allow func(*http.Request) bool) http.Handler {
	inner := PublicHandler(spec)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allow == nil || !allow(r) {
			http.NotFound(w, r)
			return
		}
		inner.ServeHTTP(w, r)
	})
}

// DocsHandler serves the API reference page at basePath + "/" and the
// spec at basePath + "/openapi.json": DocsHandlerWith with those
// defaults. public controls the gate for BOTH the page and the nested
// spec route: an app that published its spec (framework's
// WithPublicOpenAPI) gets a browsable page to match, and an app that
// kept the spec private keeps the page private too.
func DocsHandler(spec *Spec, basePath string, public bool) http.Handler {
	return DocsHandlerWith(spec, DocsConfig{BasePath: basePath, Public: public})
}
