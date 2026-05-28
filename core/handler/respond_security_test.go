package handler

import (
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

func (r ctTestResponse) ContentType() string                  { return r.ct }
func (r ctTestResponse) WriteBody(w http.ResponseWriter) error { _, err := w.Write([]byte(r.body)); return err }

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
