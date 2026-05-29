package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A recovered panic must never leak its raw value into the 500 body.
// The recover path in HandlerAdapter used to format the panic value
// into the client-facing message ("internal server error: %v"),
// surfacing driver strings, internal paths, and DB errors verbatim —
// exactly the leak WriteError's own doc-comment promises to prevent.
func TestHandlerAdapter_PanicNoLeak(t *testing.T) {
	cases := []struct {
		name   string
		panic  any
		secret string
	}{
		{"db driver string", `pq: password authentication failed for user "admin" at /secret/path`, "pq:"},
		{"wrapped error value", errSecret("connect to 10.0.0.5:5432: refused"), "10.0.0.5"},
		{"internal path", "open /var/lib/app/secrets.key: permission denied", "secrets.key"},
		{"raw struct", struct{ Token string }{Token: "sk-live-abc123"}, "sk-live-abc123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := HandlerAdapter(func(ctx context.Context, in struct{}) (struct{}, error) {
				panic(tc.panic)
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			w := httptest.NewRecorder()
			h(w, req)

			if w.Code != http.StatusInternalServerError {
				t.Fatalf("expected 500, got %d", w.Code)
			}
			body := w.Body.String()
			if strings.Contains(body, tc.secret) {
				t.Errorf("SECURITY: recovered panic leaked %q into response body: %s", tc.secret, body)
			}
			if !strings.Contains(body, "internal server error") {
				t.Errorf("expected generic 500 body, got: %s", body)
			}
		})
	}
}

type errSecret string

func (e errSecret) Error() string { return string(e) }
