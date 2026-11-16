package framework

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func (a *App) apiNamespacePrefix() string {
	if p := a.apiPrefix(); p != "" {
		return p
	}
	return "/api"
}

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

var (
	problemNotFound         = i18n.Define("framework.api_not_found", "Not Found")
	problemNoRoute          = i18n.Define("framework.api_no_route", "no route matched {{path}}")
	problemMethodNotAllowed = i18n.Define("framework.api_method_title", "Method Not Allowed")
	problemWrongMethod      = i18n.Define("framework.api_method_not_allowed", "{{path}} does not take {{method}}; the Allow header lists the methods it takes")
)

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
