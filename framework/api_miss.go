package framework

// api_miss.go: RFC 9457 problem+json for unmatched API paths.
//
// CRUD record misses already answer JSON (crud.writeJSONError), but any
// /api-prefixed path that matches NO route fell through the router to the
// UI host's HTML 404 page — a machine client probing the API got a human
// page. The guard below intercepts those misses at the router's NotFound
// fall-through and answers an application/problem+json document, so every
// miss in the API namespace is machine-readable regardless of whether a UI
// host is mounted.

import (
	"encoding/json"
	"net/http"
	"strings"
)

// apiNamespacePrefix is the path prefix the JSON miss guard protects: the
// configured APIPrefix (WithAPIPrefix) when the app sets one, "/api" by
// default. The default matches the conventional namespace even for apps
// that leave entity CRUD at the bare root: the framework itself mounts
// /api/docs and /api/llm.md there, and it is what agents and scanners
// probe. Never hard-code the prefix at call sites; resolve it here.
func (a *App) apiNamespacePrefix() string {
	if p := a.apiPrefix(); p != "" {
		return p
	}
	return "/api"
}

// installAPIMissProblem404 wraps the router's NotFound fall-through (after
// every mount installed its own handler) so a request under the API
// namespace that matched no route answers application/problem+json instead
// of the previous handler's HTML 404. Method mismatches on existing API
// routes keep their 405 + Allow semantics; only genuine misses are claimed.
func (a *App) installAPIMissProblem404() {
	prefix := a.apiNamespacePrefix()
	a.router.WrapNotFound(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if p := r.URL.Path; p == prefix || strings.HasPrefix(p, prefix+"/") {
				writeProblemNotFound(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
}

// writeProblemNotFound answers an unmatched API path with an RFC 9457
// problem document. The request path appears only inside the JSON-encoded
// detail member; json.Encoder's HTML escaping is the sanitizer, so a
// hostile path is never reflected unescaped.
func writeProblemNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]any{
		"type":   "about:blank",
		"title":  "Not Found",
		"status": http.StatusNotFound,
		"detail": "no route matched " + r.URL.Path,
	})
}
