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

	"github.com/DonaldMurillo/gofastr/core/i18n"
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

// installAPIMissProblems wraps the router's NotFound and MethodNotAllowed
// fall-throughs (after every mount installed its own handler) so a request
// under the API namespace that matched no route, or matched one under
// another method, answers application/problem+json instead of the previous
// handler's HTML page. The UI host's 405 page also minted a guest session
// for an API client; the problem document mints nothing. A 405 keeps the
// Allow header the router set.
//
// Two machine surfaces sit outside the API prefix and get the same guard:
// the MCP mount (/mcp and anything under it), whose clients speak JSON-RPC,
// and the /openapi.json document. Their 405 and 404 used
// to answer the UI host's page and mint a guest session too.
func (a *App) installAPIMissProblems() {
	prefix := a.apiNamespacePrefix()
	under := func(p, root string) bool { return p == root || strings.HasPrefix(p, root+"/") }
	inAPI := func(r *http.Request) bool {
		p := r.URL.Path
		return under(p, prefix) || under(p, "/mcp") || p == "/openapi.json"
	}
	a.router.WrapNotFound(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if inAPI(r) {
				writeProblemNotFound(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	a.router.WrapMethodNotAllowed(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if inAPI(r) {
				writeProblemMethodNotAllowed(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
}

// The problem documents an API miss answers with; the reason member
// carries the detail's stable code.
var (
	problemNotFound         = i18n.Define("framework.api_not_found", "Not Found")
	problemNoRoute          = i18n.Define("framework.api_no_route", "no route matched {{path}}")
	problemMethodNotAllowed = i18n.Define("framework.api_method_title", "Method Not Allowed")
	problemWrongMethod      = i18n.Define("framework.api_method_not_allowed", "{{path}} does not take {{method}}; the Allow header lists the methods it takes")
)

// writeProblemMethodNotAllowed answers a wrong method on an API route
// with an RFC 9457 problem document. The Allow header is the router's.
func writeProblemMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusMethodNotAllowed)
	ctx := r.Context()
	json.NewEncoder(w).Encode(map[string]any{
		"type":   "about:blank",
		"title":  problemMethodNotAllowed.Text(ctx),
		"status": http.StatusMethodNotAllowed,
		"detail": problemWrongMethod.Text(ctx, map[string]any{"path": r.URL.Path, "method": r.Method}),
		"reason": problemWrongMethod.Code(),
	})
}

// writeProblemNotFound answers an unmatched API path with an RFC 9457
// problem document. The request path appears only inside the JSON-encoded
// detail member; json.Encoder's HTML escaping is the sanitizer, so a
// hostile path is never reflected unescaped.
func writeProblemNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusNotFound)
	ctx := r.Context()
	json.NewEncoder(w).Encode(map[string]any{
		"type":   "about:blank",
		"title":  problemNotFound.Text(ctx),
		"status": http.StatusNotFound,
		"detail": problemNoRoute.Text(ctx, map[string]any{"path": r.URL.Path}),
		"reason": problemNoRoute.Code(),
	})
}
