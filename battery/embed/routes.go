package embed

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// maxRequestBody caps the JSON body size for /index and /query so a
// hostile or buggy client can't exhaust memory by streaming a huge
// payload at us. 1 MiB is well above any realistic single-batch
// document or query payload.
const maxRequestBody = 1 << 20 // 1 MiB

// Handler returns an [http.Handler] that exposes the index over HTTP.
// The handler is framework-agnostic; it can be mounted under any
// prefix on any router or http.ServeMux. The plugin in [Plugin]
// mounts it under "/embed" on a GoFastr framework.App.
//
// Security contract:
//
//   - Every route requires a non-empty Authorization header. The handler
//     does not validate the credential itself — that is the caller's job
//     when the handler is mounted behind an auth middleware. Rejecting
//     anonymous traffic at the handler is a defense-in-depth measure so
//     an unprotected mount cannot accidentally expose the index.
//   - POST routes require Content-Type: application/json (415 otherwise).
//   - Request bodies are capped at 1 MiB (413 otherwise).
//   - Upstream / driver errors are NEVER echoed back to the client; the
//     handler returns a generic "internal error" string instead.
//
// Routes:
//
//   - POST   /index          body: {"documents": [...]}            → {"added": N}
//   - POST   /query          body: Query                           → {"hits": [...]}
//   - GET    /stats                                                → Stats
//   - DELETE /doc/{id}       (or query param ?id=)                 → 204
func Handler(idx Index) http.Handler {
	mux := http.NewServeMux()
	// Middleware order matters: body-size and content-type are cheap
	// shape checks that must run BEFORE auth so probes can't infer
	// whether a route exists by getting a 401 for a malformed request.
	// (Equivalent: a giant body or a wrong content type should be
	// rejected on syntactic grounds, not security grounds.)
	mux.Handle("POST /index", limitBody(requireJSON(requireAuth(indexHandler(idx)))))
	mux.Handle("POST /query", limitBody(requireJSON(requireAuth(queryHandler(idx)))))
	mux.Handle("GET /stats", requireAuth(statsHandler(idx)))
	// DELETE supports both the pattern path param and a fallback query
	// param so callers using mux flavors without Go 1.22 wildcards can
	// still delete.
	mux.Handle("DELETE /doc/{id}", requireAuth(deleteHandler(idx)))
	mux.Handle("DELETE /doc", requireAuth(deleteHandler(idx)))
	return mux
}

// requireAuth rejects requests that don't carry an Authorization header.
// The handler itself doesn't validate the credential — that is the job
// of an auth middleware in front of the mount. This is defense-in-depth
// so an accidentally-unprotected mount can't be probed anonymously.
func requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get("Authorization")) == "" {
			writeErr(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireJSON rejects requests whose Content-Type isn't application/json.
// Bare `application/json` and `application/json; charset=utf-8` both pass.
func requireJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		// Trim any parameter (charset=…).
		if semi := strings.IndexByte(ct, ';'); semi >= 0 {
			ct = ct[:semi]
		}
		if strings.ToLower(strings.TrimSpace(ct)) != "application/json" {
			writeErr(w, http.StatusUnsupportedMediaType, "content-type must be application/json")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// limitBody caps the request body. Requests whose Content-Length
// already exceeds the cap are rejected with 413 up-front; for chunked
// or unknown-length bodies we wrap r.Body in http.MaxBytesReader so the
// subsequent Decode returns a *http.MaxBytesError that the handler
// translates into 413.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxRequestBody {
			writeErr(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		next.ServeHTTP(w, r)
	})
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
			if isBodyTooLarge(err) {
				writeErr(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			writeErr(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if len(body.Documents) == 0 {
			writeErr(w, http.StatusBadRequest, "documents is required and must be non-empty")
			return
		}
		if err := idx.Add(r.Context(), body.Documents...); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusAccepted, indexResponse{Added: len(body.Documents)})
	})
}

func queryHandler(idx Index) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q Query
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			if isBodyTooLarge(err) {
				writeErr(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			writeErr(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if strings.TrimSpace(q.Text) == "" {
			writeErr(w, http.StatusBadRequest, "query.text is required")
			return
		}
		hits, err := idx.Query(r.Context(), q)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal error")
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
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// isBodyTooLarge reports whether the error from a Decode call came from
// hitting the MaxBytesReader cap. The stdlib uses *http.MaxBytesError
// for this since Go 1.19.
func isBodyTooLarge(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
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
