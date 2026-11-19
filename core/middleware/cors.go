package middleware

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"golang.org/x/net/http/httpguts"
)

// CORSConfig holds configuration for the CORS middleware.
type CORSConfig struct {
	// AllowedOrigins lists the exact origins allowed, or "*" for all; empty denies every origin.
	AllowedOrigins []string

	// AllowedMethods defaults to GET, POST, PUT, DELETE, PATCH, OPTIONS.
	AllowedMethods []string

	// AllowedHeaders is the list of allowed request headers.
	AllowedHeaders []string

	// AllowAnyHeader answers an allowed origin's preflight with the well-formed header names it asked for, at most 64.
	AllowAnyHeader bool

	// AllowAnyMethod answers an allowed origin's preflight with the method token it asked for.
	AllowAnyMethod bool
}

// CORS returns middleware that adds CORS headers for an allowed origin, echoing the matching origin, and answers a preflight with 204, or 403 for a rejected origin.
func CORS(cfg CORSConfig) Middleware {
	methods := "GET, POST, PUT, DELETE, PATCH, OPTIONS"
	if len(cfg.AllowedMethods) > 0 {
		methods = strings.Join(sanitizeCORSTokens(cfg.AllowedMethods), ", ")
	}

	headers := "Content-Type, Authorization"
	if len(cfg.AllowedHeaders) > 0 {
		headers = strings.Join(sanitizeCORSTokens(cfg.AllowedHeaders), ", ")
	}

	allowAll := false
	originSet := make(map[string]bool, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		if o == "*" {
			allowAll = true
		}
		originSet[o] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			allowed := false
			if allowAll {
				w.Header().Set("Access-Control-Allow-Origin", "*")
				allowed = true
			} else {
				w.Header().Add("Vary", "Origin")
				if origin != "" && originSet[origin] {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					allowed = true
				}
			}

			if allowed {
				m, h := methods, headers
				if cfg.AllowAnyMethod {
					w.Header().Add("Vary", "Access-Control-Request-Method")
					if req := requestedMethod(r); req != "" {
						m = req
					}
				}
				if cfg.AllowAnyHeader {
					w.Header().Add("Vary", "Access-Control-Request-Headers")
					if req := requestedHeaders(r); req != "" {
						h = req
					}
				}
				w.Header().Set("Access-Control-Allow-Methods", m)
				w.Header().Set("Access-Control-Allow-Headers", h)
			}

			preflight := r.Method == http.MethodOptions && origin != "" &&
				r.Header.Get("Access-Control-Request-Method") != ""
			if preflight {
				if !allowed {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			sw := stripCredsWriter{ResponseWriter: w}
			defer sw.stripCredentialsForWildcard()
			next.ServeHTTP(sw, r)
		})
	}
}

type stripCredsWriter struct {
	http.ResponseWriter
}

func (s stripCredsWriter) stripCredentialsForWildcard() {
	if s.ResponseWriter.Header().Get("Access-Control-Allow-Origin") == "*" {
		s.ResponseWriter.Header().Del("Access-Control-Allow-Credentials")
	}
}

func (s stripCredsWriter) WriteHeader(code int) {
	s.stripCredentialsForWildcard()
	s.ResponseWriter.WriteHeader(code)
}

func (s stripCredsWriter) Write(b []byte) (int, error) {
	s.stripCredentialsForWildcard()
	return s.ResponseWriter.Write(b)
}

func (s stripCredsWriter) ReadFrom(src io.Reader) (int64, error) {
	s.stripCredentialsForWildcard()
	return io.Copy(s.ResponseWriter, src)
}

func (s stripCredsWriter) Flush() {
	s.stripCredentialsForWildcard()
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s stripCredsWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := s.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errors.New("middleware: underlying ResponseWriter does not implement http.Hijacker")
}

func (s stripCredsWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

const maxEchoedHeaders = 64

func requestedHeaders(r *http.Request) string {
	var out []string
	seen := map[string]bool{}
	for _, line := range r.Header.Values("Access-Control-Request-Headers") {
		for _, name := range strings.Split(line, ",") {
			name = strings.ToLower(strings.TrimSpace(name))
			if name == "" || seen[name] || !httpguts.ValidHeaderFieldName(name) {
				continue
			}
			seen[name] = true
			out = append(out, name)
			if len(out) == maxEchoedHeaders {
				return strings.Join(out, ", ")
			}
		}
	}
	return strings.Join(out, ", ")
}

func requestedMethod(r *http.Request) string {
	m := strings.TrimSpace(r.Header.Get("Access-Control-Request-Method"))
	if m == "" || !httpguts.ValidHeaderFieldName(m) {
		return ""
	}
	return m
}

func sanitizeCORSTokens(in []string) []string {
	out := make([]string, 0, len(in))
	for _, t := range in {
		clean := textsafe.SanitizeControlBytes(t)
		clean = strings.TrimSpace(clean)
		if clean == "" {
			continue
		}
		out = append(out, clean)
	}
	return out
}
