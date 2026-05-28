package openapi

import (
	"encoding/json"
	"html"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// Handler returns an http.Handler that serves the OpenAPI spec as JSON
// at the root path (typically mounted at /openapi.json or /docs/openapi.json).
//
// The spec lists every registered route, so by default the handler
// requires an authenticated context (the framework's auth chain must
// have called [handler.SetUser]). Apps that want a public OpenAPI spec
// can wrap [PublicHandler] around the same spec.
func Handler(spec *Spec) http.Handler {
	inner := PublicHandler(spec)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if _, ok := handler.GetUser(r.Context()); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		inner.ServeHTTP(w, r)
	})
}

// PublicHandler serves the spec without an auth check — use only when
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
// 401/403 — leaking that an OpenAPI endpoint exists is itself the
// disclosure we're trying to avoid). 404 keeps the existence of the
// endpoint indistinguishable from a vanilla missing route. The allow
// predicate is the only auth gate — the wrapped spec handler does NOT
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

// SwaggerUIHandler returns an http.Handler that serves a minimal,
// self-contained API spec landing page. The page links to the OpenAPI
// JSON; viewers like Swagger UI / Insomnia / Stoplight / Postman can
// load it. Earlier revisions embedded swagger-ui-dist via a public CDN
// — that pulled the docs page's supply chain into a third-party host
// (and broke offline / air-gapped deploys), so the CDN reference was
// removed. Hosts that want the interactive UI can vendor swagger-ui
// themselves and mount their own handler.
func SwaggerUIHandler(spec *Spec, basePath string) http.Handler {
	specHandler := Handler(spec)
	// basePath is developer-supplied config but flows into href + visible
	// text; escape it so a CR/LF/quote/angle can't break out of the
	// attribute or inject script content.
	safeBase := html.EscapeString(basePath)
	uiHTML := `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>API spec</title>
  <style>
    body { font-family: -apple-system, system-ui, sans-serif; max-width: 48rem; margin: 3rem auto; padding: 0 1.5rem; line-height: 1.5; color: #111827; }
    code { background: rgba(0,0,0,0.05); padding: 0.1em 0.35em; border-radius: 3px; font-size: 0.95em; }
    a { color: #1d4ed8; }
  </style>
</head>
<body>
  <h1>API specification</h1>
  <p>This service exposes an OpenAPI 3 document at <a href="` + safeBase + `/openapi.json"><code>` + safeBase + `/openapi.json</code></a>.</p>
  <p>Load it in your preferred viewer (Swagger UI, Insomnia, Stoplight, Postman, …) to browse the endpoints interactively.</p>
</body>
</html>`

	csp := "default-src 'self'; style-src 'self' 'unsafe-inline'; base-uri 'self'; frame-ancestors 'none'"

	mux := http.NewServeMux()
	mux.Handle("/openapi.json", specHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Set security headers first so even the 401 response carries them.
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", csp)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if _, ok := handler.GetUser(r.Context()); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(uiHTML))
	})

	// Strip the basePath prefix so internal routes (/openapi.json, /) match
	// when mounted at a sub-path like /docs/
	return http.StripPrefix(basePath, mux)
}
