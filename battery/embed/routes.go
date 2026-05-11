package embed

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// Handler returns an [http.Handler] that exposes the index over HTTP.
// The handler is framework-agnostic; it can be mounted under any
// prefix on any router or http.ServeMux. The plugin in [Plugin]
// mounts it under "/embed" on a GoFastr framework.App.
//
// Routes:
//
//   - POST   /index          body: {"documents": [...]}            → {"added": N}
//   - POST   /query          body: Query                           → {"hits": [...]}
//   - GET    /stats                                                → Stats
//   - DELETE /doc/{id}       (or query param ?id=)                 → 204
func Handler(idx Index) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /index", indexHandler(idx))
	mux.Handle("POST /query", queryHandler(idx))
	mux.Handle("GET /stats", statsHandler(idx))
	// DELETE supports both the pattern path param and a fallback query
	// param so callers using mux flavors without Go 1.22 wildcards can
	// still delete.
	mux.Handle("DELETE /doc/{id}", deleteHandler(idx))
	mux.Handle("DELETE /doc", deleteHandler(idx))
	return mux
}

type indexRequest struct {
	Documents []Document `json:"documents"`
}

type indexResponse struct {
	Added int `json:"added"`
}

type queryResponse struct {
	Hits []Hit `json:"hits"`
}

func indexHandler(idx Index) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body indexRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if len(body.Documents) == 0 {
			writeErr(w, http.StatusBadRequest, "documents is required and must be non-empty")
			return
		}
		if err := idx.Add(r.Context(), body.Documents...); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, indexResponse{Added: len(body.Documents)})
	})
}

func queryHandler(idx Index) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q Query
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if strings.TrimSpace(q.Text) == "" {
			writeErr(w, http.StatusBadRequest, "query.text is required")
			return
		}
		hits, err := idx.Query(r.Context(), q)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, queryResponse{Hits: hits})
	})
}

func statsHandler(idx Index) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, idx.Stats())
	})
}

func deleteHandler(idx Index) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			id = r.URL.Query().Get("id")
		}
		if id == "" {
			writeErr(w, http.StatusBadRequest, "doc id is required")
			return
		}
		if err := idx.Remove(r.Context(), id); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, errorResponse{Error: msg})
}

// errMethodNotAllowed is reserved for future use when we split the
// composite handler into per-route handlers and want to centralise the
// 405 response.
var errMethodNotAllowed = errors.New("method not allowed")

var _ = errMethodNotAllowed
