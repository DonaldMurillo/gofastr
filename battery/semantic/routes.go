package semantic

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

const maxRequestBody = 1 << 20

// Handler returns an http.Handler exposing the index over HTTP; with no auth
// option it refuses every request.
func Handler(idx Index, opts ...HandlerOption) http.Handler {
	cfg := handlerConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	mux := http.NewServeMux()
	auth := requireAuth(cfg)
	mux.Handle("POST /index", limitBody(requireJSON(auth(indexHandler(idx)))))
	mux.Handle("POST /query", limitBody(requireJSON(auth(queryHandler(idx)))))
	mux.Handle("GET /stats", auth(statsHandler(idx)))
	mux.Handle("DELETE /doc/{id}", auth(deleteHandler(idx)))
	mux.Handle("DELETE /doc", auth(deleteHandler(idx)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

func requireAuth(cfg handlerConfig) func(http.Handler) http.Handler {
	const bearerPrefix = "Bearer "
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.insecure {
				next.ServeHTTP(w, r)
				return
			}
			if cfg.authToken == "" {
				writeErr(w, r, http.StatusUnauthorized, handler.ReasonAuthenticationRequired)
				return
			}
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, bearerPrefix) {
				writeErr(w, r, http.StatusUnauthorized, handler.ReasonAuthenticationRequired)
				return
			}
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

// WithAuthToken requires this bearer token, compared in constant time.
func WithAuthToken(token string) HandlerOption {
	return func(c *handlerConfig) { c.authToken = token }
}

// WithInsecureDisabledAuth turns authentication off, for local development
// only.
func WithInsecureDisabledAuth() HandlerOption {
	return func(c *handlerConfig) { c.insecure = true }
}

func requireJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
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
		if err := handler.UnmarshalStrict(raw, &body, handler.UseNumber()); err != nil {
			writeErr(w, r, http.StatusBadRequest, i18n.ReasonInvalidJSONBody)
			return
		}
		if len(body.Documents) == 0 {
			writeErr(w, r, http.StatusBadRequest, reasonDocumentsRequired)
			return
		}
		for _, d := range body.Documents {
			if d.ID == "" {
				writeErr(w, r, http.StatusBadRequest, reasonDocIdIsRequired)
				return
			}
		}
		if err := idx.Add(r.Context(), body.Documents...); err != nil {
			if invalid := (*InvalidDocumentTextError)(nil); errors.As(err, &invalid) {
				writeErr(w, r, http.StatusBadRequest, reasonDocumentInvalidText)
				return
			}
			if outOfRange := (*MetadataNumberRangeError)(nil); errors.As(err, &outOfRange) {
				writeErr(w, r, http.StatusBadRequest, reasonMetadataNumberOutOfRange)
				return
			}
			if ce := (*chunkError)(nil); errors.As(err, &ce) {
				writeErr(w, r, http.StatusBadRequest, reasonDocumentUnchunkable)
				return
			}
			writeErr(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
		writeJSON(w, r, http.StatusAccepted, indexResponse{Added: len(body.Documents)})
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
		if err := handler.UnmarshalStrict(raw, &q, handler.UseNumber()); err != nil {
			writeErr(w, r, http.StatusBadRequest, i18n.ReasonInvalidJSONBody)
			return
		}
		if strings.TrimSpace(q.Text) == "" {
			writeErr(w, r, http.StatusBadRequest, reasonQueryTextIsRequired)
			return
		}
		hits, err := idx.Query(r.Context(), q)
		if err != nil {
			if invalid := (*InvalidQueryError)(nil); errors.As(err, &invalid) {
				writeErr(w, r, http.StatusBadRequest, reasonQueryInvalid)
				return
			}
			writeErr(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
		writeJSON(w, r, http.StatusOK, queryResponse{Hits: hits})
	})
}

func statsHandler(idx Index) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, idx.Stats())
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
		if !textsafe.Storable(id) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err := idx.Remove(r.Context(), id); err != nil {
			writeErr(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func isBodyTooLarge(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

func writeJSON(w http.ResponseWriter, r *http.Request, code int, v any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		buf.Reset()
		p := i18n.StatusPhrase(http.StatusInternalServerError)
		_ = json.NewEncoder(&buf).Encode(errorResponse{Error: p.Text(r.Context()), Reason: p.Code()})
		code = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_, _ = w.Write(buf.Bytes())
}

type errorResponse struct {
	Error  string `json:"error"`
	Reason string `json:"reason"`
}

func writeErr(w http.ResponseWriter, r *http.Request, code int, reason i18n.Phrase, params ...map[string]any) {
	writeJSON(w, r, code, errorResponse{Error: reason.Text(r.Context(), params...), Reason: reason.Code()})
}

var errMethodNotAllowed = errors.New("method not allowed")

var _ = errMethodNotAllowed
