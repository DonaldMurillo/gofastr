package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A recovered panic must never leak its raw value into the 500 body.
// The recover path in HandlerAdapter used to format the panic value
// into the client-facing message ("internal server error: %v"),
// surfacing driver strings, internal paths, and DB errors verbatim,
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
		{"raw struct", struct{ Token string }{Token: "sk-live-abc123"}, "sk-live-abc123"}, // not-a-secret: fake token exercising the redaction path under test
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

type panicMidBody struct{}

func (panicMidBody) ContentType() string { return "text/html; charset=utf-8" }
func (panicMidBody) WriteBody(w http.ResponseWriter) error {
	_, _ = w.Write([]byte("<p>partial"))
	panic("render failed")
}

// A panic inside a ResponseType's WriteBody comes after the 200 and part
// of the body went out. The recover path wrote its JSON 500 anyway, which
// net/http could only append to the half-written page under the 200: a
// broken render served as a success. Once the response has started the
// adapter aborts it with http.ErrAbortHandler instead, so net/http drops
// the connection and the client sees a failed response.
func TestHandlerAdapter_PanicAfterResponseStartedAborts(t *testing.T) {
	h := HandlerAdapter(Handler[struct{}, panicMidBody](func(context.Context, struct{}) (panicMidBody, error) {
		return panicMidBody{}, nil
	}))

	w := httptest.NewRecorder()
	func() {
		defer func() {
			if rec := recover(); rec != http.ErrAbortHandler {
				t.Errorf("recovered %v, want http.ErrAbortHandler re-panicked", rec)
			}
		}()
		h(w, httptest.NewRequest(http.MethodGet, "/", nil))
	}()
	if body := w.Body.String(); body != "<p>partial" {
		t.Errorf("body = %q, want the partial page with no error document spliced on", body)
	}

	// Over a real connection the client never sees a complete 200.
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		return // the connection was aborted before the headers arrived
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	if readErr == nil {
		t.Errorf("status %d body %q read to a clean end; want the response aborted", resp.StatusCode, body)
	}
}
