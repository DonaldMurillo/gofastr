package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// wildcardACAO drives a handler through CORS with ACAO "*" and returns the
// two headers that must never ship together.
func wildcardACAO(t *testing.T, method string, h http.HandlerFunc) (acao, acac string) {
	t.Helper()
	srv := httptest.NewServer(CORS(CORSConfig{AllowedOrigins: []string{"*"}})(h))
	defer srv.Close()

	req, _ := http.NewRequest(method, srv.URL+"/thing", nil)
	req.Header.Set("Origin", "https://evil.example")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	return resp.Header.Get("Access-Control-Allow-Origin"),
		resp.Header.Get("Access-Control-Allow-Credentials")
}

// stripCredsWriter strips in Write, WriteHeader and Flush. A handler that
// sets the header and returns without writing hits none of the three:
// net/http's (*response).finishRequest calls WriteHeader(200) on the REAL
// response, not on the wrapper. An empty 200 is an ordinary shape for a
// DELETE or a PUT.
func TestStripCredsCoversEmptyResponse(t *testing.T) {
	acao, acac := wildcardACAO(t, http.MethodDelete, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		// no Write, no WriteHeader
	})
	if acao == "*" && acac != "" {
		t.Fatalf("wildcard ACAO shipped with Access-Control-Allow-Credentials: %q", acac)
	}
}

// The paths that already worked, kept so the added post-handler strip
// cannot be mistaken for the only one.
func TestStripCredsCoversWrittenResponse(t *testing.T) {
	for _, tc := range []struct {
		name string
		h    http.HandlerFunc
	}{
		{"write", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			_, _ = w.Write([]byte("ok"))
		}},
		{"writeheader", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.WriteHeader(http.StatusAccepted)
		}},
		{"flush", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acao, acac := wildcardACAO(t, http.MethodPost, tc.h)
			if acao == "*" && acac != "" {
				t.Fatalf("wildcard ACAO shipped with Access-Control-Allow-Credentials: %q", acac)
			}
		})
	}
}

func TestCORSStripsCredsAfterPanic(t *testing.T) {
	h := Recovery()(CORS(CORSConfig{AllowedOrigins: []string{"*"}})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			panic("boom")
		})))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/thing", nil)
	req.Header.Set("Origin", "https://app.example")
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("credentials header survived panic recovery with wildcard ACAO: %q", got)
	}
}

func TestCORSStripsHandlerWildcard(t *testing.T) {
	h := CORS(CORSConfig{AllowedOrigins: []string{"https://app.example"}})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.WriteHeader(http.StatusOK)
		}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/thing", nil)
	req.Header.Set("Origin", "https://app.example")
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("credentials header survived handler-emitted wildcard ACAO: %q", got)
	}
}
