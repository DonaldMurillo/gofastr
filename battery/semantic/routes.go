package semantic

import (
	"crypto/sha256"
	"crypto/subtle"
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
// mounts it under "/semantic" on a GoFastr framework.App.
//
// Security contract:
//
//   - Every route requires a valid bearer token. Configure it with
//     [WithAuthToken] (or [Plugin.WithAuthToken]); clients must send
//     "Authorization: Bearer <token>", verified in constant time. When no
//     token is configured the handler fails CLOSED — every request is
//     rejected — so an accidentally-unprotected mount cannot expose the
//     index. [WithInsecureDisabledAuth] opts out of auth for local dev only.
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
func Handler(idx Index, opts ...HandlerOption) http.Handler {
	cfg := handlerConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	mux := http.NewServeMux()
	// Middleware order matters: body-size and content-type are cheap
	// shape checks that must run BEFORE auth so probes can't infer
	// whether a route exists by getting a 401 for a malformed request.
	// (Equivalent: a giant body or a wrong content type should be
	// rejected on syntactic grounds, not security grounds.)
	auth := requireAuth(cfg)
	mux.Handle("POST /index", limitBody(requireJSON(auth(indexHandler(idx)))))
	mux.Handle("POST /query", limitBody(requireJSON(auth(queryHandler(idx)))))
	mux.Handle("GET /stats", auth(statsHandler(idx)))
	// DELETE supports both the pattern path param and a fallback query
	// param so callers using mux flavors without Go 1.22 wildcards can
	// still delete.
	mux.Handle("DELETE /doc/{id}", auth(deleteHandler(idx)))
	mux.Handle("DELETE /doc", auth(deleteHandler(idx)))
	return mux
}

// requireAuth returns the auth middleware for the handler's policy. With
// nothing configured it fails CLOSED (every request 401) so an unprotected
// mount is never silently open; with a token it requires a constant-time
// bearer match; WithInsecureDisabledAuth bypasses entirely for local
// development only.
func requireAuth(cfg handlerConfig) func(http.Handler) http.Handler {
	const bearerPrefix = "Bearer "
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.insecure {
				next.ServeHTTP(w, r)
				return
			}
			// Fail closed when no credential is configured: a mount with no
			// token must never serve the index, even to a caller who sends a
			// (meaningless) Authorization header.
			if cfg.authToken == "" {
				writeErr(w, http.StatusUnauthorized, "authentication required")
				return
			}
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, bearerPrefix) {
				writeErr(w, http.StatusUnauthorized, "authentication required")
				return
			}
			// Compare fixed-width digests, not the raw strings.
			// subtle.ConstantTimeCompare returns immediately when the two
			// lengths differ, so comparing tokens directly still tells an
			// attacker when a candidate is the right LENGTH. Hashing first
			// makes both sides 32 bytes, so every candidate costs the same.
			got := sha256.Sum256([]byte(strings.TrimSpace(h[len(bearerPrefix):])))
			want := sha256.Sum256([]byte(cfg.authToken))
			if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
				writeErr(w, http.StatusUnauthorized, "authentication required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// HandlerOption configures [Handler]'s auth policy.
type HandlerOption func(*handlerConfig)

type handlerConfig struct {
	authToken string
	insecure  bool
}

// WithAuthToken requires clients to present this exact bearer token in the
// Authorization header ("Authorization: Bearer <token>"), compared in
// constant time. This is the production auth mode. Pair it with
// [Plugin.WithAuthToken] when mounting via the framework plugin.
func WithAuthToken(token string) HandlerOption {
	return func(c *handlerConfig) { c.authToken = token }
}

// WithInsecureDisabledAuth turns authentication OFF entirely. It is the only
// way to serve the handler without a configured token and is intended for
// local development only — never use it in production. Prefer [WithAuthToken].
func WithInsecureDisabledAuth() HandlerOption {
	return func(c *handlerConfig) { c.insecure = true }
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
