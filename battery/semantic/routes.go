package semantic

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
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
//     token is configured the handler fails CLOSED, every request is
//     rejected, so an accidentally-unprotected mount cannot expose the
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
				writeErr(w, r, http.StatusUnauthorized, handler.ReasonAuthenticationRequired)
				return
			}
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, bearerPrefix) {
				writeErr(w, r, http.StatusUnauthorized, handler.ReasonAuthenticationRequired)
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
				writeErr(w, r, http.StatusUnauthorized, handler.ReasonAuthenticationRequired)
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
// local development only, never use it in production. Prefer [WithAuthToken].
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
			writeErr(w, r, http.StatusUnsupportedMediaType, i18n.ReasonJSONMediaType)
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
			writeErr(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
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
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			if isBodyTooLarge(err) {
				writeErr(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
				return
			}
			writeErr(w, r, http.StatusBadRequest, i18n.ReasonInvalidJSONBody)
			return
		}
		// Strict decode: stdlib json keeps the LAST duplicate key and
		// matches struct tags case-insensitively, so
		// {"documents":[A],"documents":[B]} indexes batch B while any
		// audit log records A. Refuse the ambiguity.
		if err := handler.UnmarshalStrict(raw, &body); err != nil {
			writeErr(w, r, http.StatusBadRequest, i18n.ReasonInvalidJSONBody)
			return
		}
		if len(body.Documents) == 0 {
			writeErr(w, r, http.StatusBadRequest, reasonDocumentsRequired)
			return
		}
		if err := idx.Add(r.Context(), body.Documents...); err != nil {
			writeErr(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
		writeJSON(w, http.StatusAccepted, indexResponse{Added: len(body.Documents)})
	})
}

func queryHandler(idx Index) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q Query
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			if isBodyTooLarge(err) {
				writeErr(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
				return
			}
			writeErr(w, r, http.StatusBadRequest, i18n.ReasonInvalidJSONBody)
			return
		}
		// Same strict rule as /index: a duplicated or case-folded "text"
		// key must not silently swap the query.
		if err := handler.UnmarshalStrict(raw, &q); err != nil {
			writeErr(w, r, http.StatusBadRequest, i18n.ReasonInvalidJSONBody)
			return
		}
		if strings.TrimSpace(q.Text) == "" {
			writeErr(w, r, http.StatusBadRequest, reasonQueryTextIsRequired)
			return
		}
		hits, err := idx.Query(r.Context(), q)
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
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
			writeErr(w, r, http.StatusBadRequest, reasonDocIdIsRequired)
			return
		}
		if err := idx.Remove(r.Context(), id); err != nil {
			writeErr(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
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
	// Every response on this handler's path sits behind a resolved
	// principal (the bearer-token gate), so no shared or back/forward
	// cache may retain one — RFC 9111's storage rules cover the
	// Authorization request, but the posture is stamped here so no
	// route can forget it, the same choke-point spelling the admin
	// battery's gate uses.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type errorResponse struct {
	Error  string `json:"error"`
	Reason string `json:"reason"`
}

func writeErr(w http.ResponseWriter, r *http.Request, code int, reason i18n.Phrase, params ...map[string]any) {
	writeJSON(w, code, errorResponse{Error: reason.Text(r.Context(), params...), Reason: reason.Code()})
}

// errMethodNotAllowed is reserved for future use when we split the
// composite handler into per-route handlers and want to centralise the
// 405 response.
var errMethodNotAllowed = errors.New("method not allowed")

var _ = errMethodNotAllowed
