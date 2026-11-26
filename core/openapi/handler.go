package openapi

import (
	"encoding/json"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// Handler serves the spec as JSON to an authenticated request (see
// [handler.SetUser]) and answers 401 to any other. A spec with a
// RequestView is built per request from that view.
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

func serveSpec(w http.ResponseWriter, r *http.Request, spec *Spec) {
	s := spec.viewFor(r)
	data, err := json.MarshalIndent(s.Build(), "", "  ")
	writeSpecJSON(w, data, err)
}

// PublicHandler serves the whole spec, built once, without an auth check.
func PublicHandler(spec *Spec) http.Handler {
	data, err := json.MarshalIndent(spec.Build(), "", "  ")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeSpecJSON(w, data, err)
	})
}

func writeSpecJSON(w http.ResponseWriter, data []byte, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		data = []byte(`{"error":"failed to marshal spec"}`)
	}
	w.Write(data)
}

// GatedHandler serves the spec to a request allow admits and 404 to any
// other, a nil allow admitting none. An admitted request gets the
// RequestView when the spec has one.
func GatedHandler(spec *Spec, allow func(*http.Request) bool) http.Handler {
	static := PublicHandler(spec)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allow == nil || !allow(r) {
			http.NotFound(w, r)
			return
		}
		if spec.RequestView == nil {
			static.ServeHTTP(w, r)
			return
		}
		serveSpec(w, r, spec)
	})
}

// DocsHandler is [DocsHandlerWith] with the spec at basePath +
// "/openapi.json"; public opens the page and the spec alike.
func DocsHandler(spec *Spec, basePath string, public bool) http.Handler {
	return DocsHandlerWith(spec, DocsConfig{BasePath: basePath, Public: public})
}
