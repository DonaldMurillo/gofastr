package effect

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func TestResolveAcceptsJSONNumberStatus(t *testing.T) {
	r, err := Resolve(t.Context(), world.Action{Kind: world.ActionRespondJSON, Params: map[string]any{"status": json.Number("201")}}, Scope{})
	if err != nil || r.Status != 201 {
		t.Fatalf("got %+v, %v; want status 201", r, err)
	}
}

func TestResolveRejectsBooleanStatus(t *testing.T) {
	_, err := Resolve(t.Context(), world.Action{Kind: world.ActionRespondJSON, Params: map[string]any{"status": true}}, Scope{})
	if err == nil {
		t.Fatal("boolean status accepted")
	}
}

func TestResponseEncodeErrorLeavesStatusUncommitted(t *testing.T) {
	w := httptest.NewRecorder()
	if err := (Response{Status: 201, Body: make(chan int)}).WriteTo(w); err == nil {
		t.Fatal("want encoding error")
	}
	w.WriteHeader(500)
	if w.Code != 500 {
		t.Fatalf("status = %d; want fallback 500", w.Code)
	}
}

func TestResponseNoContentWritesNoBody(t *testing.T) {
	w := httptest.NewRecorder()
	if err := (Response{Status: 204, Body: "omitted"}).WriteTo(w); err != nil {
		t.Fatal(err)
	}
	if w.Code != 204 || w.Body.Len() != 0 {
		t.Fatalf("got status %d, body %s; want empty 204", w.Code, w.Body.String())
	}
}

type shortResponseWriter struct{ http.ResponseWriter }

func (w shortResponseWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestResponseReportsShortWrite(t *testing.T) {
	err := (Response{Body: "message"}).WriteTo(shortResponseWriter{httptest.NewRecorder()})
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("got %v; want io.ErrShortWrite", err)
	}
}
