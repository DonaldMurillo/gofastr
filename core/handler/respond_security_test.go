package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Custom ResponseType.ContentType() flows into Set("Content-Type", ...)
// unsanitized; CR/LF/NUL there would smuggle a second header line into
// the response. nosniff used to be set only on the default JSON path,
// leaving HTML, SSE, RawBytes, and third-party ResponseType impls
// without the canonical anti-MIME-sniff defense.

type ctTestResponse struct {
	ct   string
	body string
}

func (r ctTestResponse) ContentType() string { return r.ct }
func (r ctTestResponse) WriteBody(w http.ResponseWriter) error {
	_, err := w.Write([]byte(r.body))
	return err
}

func TestRespond_SanitizesContentType(t *testing.T) {
	bad := []string{
		"application/json\r\nX-Injected: 1",
		"text/html\nSet-Cookie: owned=1",
		"image/png\rContent-Length: 0",
		"text/css\x00text/html",
		"application/json\x7ftrailer",
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, ct := range bad {
		t.Run(ct, func(t *testing.T) {
			rec := httptest.NewRecorder()
			Respond(rec, req, ctTestResponse{ct: ct, body: "ok"})
			got := rec.Header().Get("Content-Type")
			if strings.ContainsAny(got, "\r\n\x00\x7f") {
				t.Fatalf("unsanitized Content-Type reached response: %q", got)
			}
		})
	}
}

func TestRespond_SetsNosniffOnCustomTypes(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	cases := map[string]func(*httptest.ResponseRecorder){
		"html":   func(r *httptest.ResponseRecorder) { Respond(r, req, HTML("<p>hi</p>")) },
		"sse":    func(r *httptest.ResponseRecorder) { Respond(r, req, SSE{Event: "msg", Data: "ok"}) },
		"raw":    func(r *httptest.ResponseRecorder) { Respond(r, req, RawBytes{Data: []byte("x"), CT: "image/png"}) },
		"custom": func(r *httptest.ResponseRecorder) { Respond(r, req, ctTestResponse{ct: "application/zip", body: "x"}) },
	}
	for name, write := range cases {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			write(rec)
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Fatalf("custom response %q missing nosniff header (got %q)", name, got)
			}
		})
	}
}

func TestSSEStream_SetsNosniff(t *testing.T) {
	events := make(chan SSE, 1)
	events <- SSE{Event: "msg", Data: "ok"}
	close(events)

	rec := httptest.NewRecorder()
	SSEStream(rec, events)

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("SSEStream missing nosniff (got %q)", got)
	}
}

// Property: every ResponseType surface routes its caller-supplied
// Content-Type through sanitizeHeaderValue before it reaches the wire,
// and a value that sanitizes to EMPTY falls back to
// application/octet-stream rather than an empty header. Surfaces:
// RawBytes.CT (the public field most likely to carry interpolated input)
// across CRLF / lone CR / NUL / DEL shapes plus the all-control-bytes
// fallback, and a custom ResponseType hitting the same fallback branch.
func TestRespond_RawBytesCTSanitized(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"crlf smuggle", "image/png\r\nX-Injected: 1", "image/pngX-Injected: 1"},
		{"lone CR", "text/csv\rSet-Cookie: owned=1", "text/csvSet-Cookie: owned=1"},
		{"nul", "a\x00b", "ab"},
		{"del", "x\x7fy", "xy"},
		{"all control -> fallback", "\r\n\x00\x7f", "application/octet-stream"},
		{"empty -> fallback", "", "application/octet-stream"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			Respond(rec, req, RawBytes{Data: []byte("x"), CT: tc.in})
			got := rec.Header().Get("Content-Type")
			if strings.ContainsAny(got, "\r\n\x00\x7f") {
				t.Fatalf("unsanitized Content-Type reached the wire: %q", got)
			}
			if got != tc.want {
				t.Fatalf("Content-Type = %q, want %q", got, tc.want)
			}
		})
	}

	rec := httptest.NewRecorder()
	Respond(rec, req, ctTestResponse{ct: "", body: "x"})
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("custom empty Content-Type fallback: got %q", got)
	}
}

// Property: only a BARE *Error renders its own code and message; wrapping
// (fmt.Errorf %w) or a nil error must fall through to the generic 500 so
// no inner error text reaches the client by accident. WriteError matches
// with a plain type assertion, so a wrapped *Error keeps its cause out of
// the body — per the doc contract ("any other error type is treated as an
// internal failure").
func TestWriteError_WrappedErrorGeneric500(t *testing.T) {
	inner := Errorf(http.StatusNotFound, "user 42 in shard db-prod-07")
	cases := []struct {
		name string
		err  error
	}{
		{"wrapped *Error", fmt.Errorf("lookup failed: %w", inner)},
		{"nil error", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			WriteError(rec, tc.err)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("expected generic 500, got %d", rec.Code)
			}
			body := rec.Body.String()
			for _, secret := range []string{"user 42", "shard", "lookup failed"} {
				if strings.Contains(body, secret) {
					t.Fatalf("inner error text leaked into response body: %s", body)
				}
			}
			if !strings.Contains(body, "internal server error") {
				t.Fatalf("expected generic 500 body, got: %s", body)
			}
		})
	}
}
