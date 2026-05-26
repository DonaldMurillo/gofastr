package openapi

import (
	"encoding/json"
	"net/http"
)

// Handler returns an http.Handler that serves the OpenAPI spec as JSON
// at the root path (typically mounted at /openapi.json or /docs/openapi.json).
//
// The spec lists every registered route — a route enumeration tool for
// any caller. If your spec describes admin or internal endpoints you
// don't want anonymous clients to discover, wrap the returned handler
// in [GatedHandler] (or your own auth middleware).
func Handler(spec *Spec) http.Handler {
	doc := spec.Build()
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		data = []byte(`{"error":"failed to marshal spec"}`)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write(data)
	})
}

// GatedHandler wraps the OpenAPI spec handler in an authorisation
// predicate. Requests for which allow(r) returns false get 404 (not
// 401/403 — leaking that an OpenAPI endpoint exists is itself the
// disclosure we're trying to avoid). 404 keeps the existence of the
// endpoint indistinguishable from a vanilla missing route.
//
// Typical use:
//
//	openapi.GatedHandler(spec, func(r *http.Request) bool {
//	    u, ok := handler.GetUser(r.Context())
//	    return ok && u.(*MyUser).IsAdmin()
//	})
func GatedHandler(spec *Spec, allow func(*http.Request) bool) http.Handler {
	inner := Handler(spec)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allow == nil || !allow(r) {
			http.NotFound(w, r)
			return
		}
		inner.ServeHTTP(w, r)
	})
}

// SwaggerUIHandler returns an http.Handler that serves a minimal Swagger UI
// page which loads the spec from basePath+"/openapi.json".
func SwaggerUIHandler(spec *Spec, basePath string) http.Handler {
	specHandler := Handler(spec)
	uiHTML := `<!DOCTYPE html>
<html>
<head>
  <title>Swagger UI</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    SwaggerUIBundle({
      url: "` + basePath + `/openapi.json",
      dom_id: '#swagger-ui',
    });
  </script>
</body>
</html>`

	mux := http.NewServeMux()
	mux.Handle("/openapi.json", specHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(uiHTML))
	})

	// Strip the basePath prefix so internal routes (/openapi.json, /) match
	// when mounted at a sub-path like /docs/
	return http.StripPrefix(basePath, mux)
}
