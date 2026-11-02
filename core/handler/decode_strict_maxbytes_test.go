package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// DecodeStrict flattens the read error's text into its 400 envelope,
// but the cause stays reachable: a caller that capped the body with
// http.MaxBytesReader answers 413 by asking for the cap's error, and
// that only works if the envelope wraps it. The docs site's newsletter
// handler was answering 400 for an oversized body before this held.
func TestDecodeStrict_WrapsMaxBytesError(t *testing.T) {
	rec := httptest.NewRecorder()
	body := http.MaxBytesReader(rec, io.NopCloser(strings.NewReader(`{"message":"`+strings.Repeat("a", 64)+`"}`)), 16)
	var dst struct {
		Message string `json:"message"`
	}
	err := DecodeStrict(body, &dst)
	if err == nil {
		t.Fatal("DecodeStrict over a capped reader must fail")
	}
	var he *Error
	if !errors.As(err, &he) || he.Code != 400 {
		t.Fatalf("DecodeStrict error = %T %v, want *Error with code 400", err, err)
	}
	var mbe *http.MaxBytesError
	if !errors.As(err, &mbe) {
		t.Fatalf("the cap's *http.MaxBytesError is not reachable through the envelope: %v", err)
	}
}

// Bind's own 1 MiB cap answered an oversized body 400 "invalid JSON" and
// dropped the read error, so no caller could tell an oversized body from
// a malformed one. Past the cap it is a 413 whose Err is the cap's
// *http.MaxBytesError; any other read failure stays a 400 with the error
// wrapped.
func TestBind_OversizedBodyIs413(t *testing.T) {
	type req struct {
		Name string `json:"name"`
	}
	body := `{"name":"` + strings.Repeat("a", maxBodyBytes) + `"}`

	h := HandlerAdapter(Handler[req, string](func(context.Context, req) (string, error) { return "", nil }))
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body: status %d body %s, want 413", w.Code, strings.TrimSpace(w.Body.String()))
	}

	r = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	var dst req
	err := Bind(r, &dst)
	var he *Error
	if !errors.As(err, &he) || he.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("Bind oversized: %T %v, want *Error with code 413", err, err)
	}
	var mbe *http.MaxBytesError
	if !errors.As(err, &mbe) {
		t.Errorf("Bind oversized: *http.MaxBytesError not reachable through %v", err)
	}
}

type failingBody struct{}

var errBodyBroke = errors.New("connection reset mid-body")

func (failingBody) Read([]byte) (int, error) { return 0, errBodyBroke }
func (failingBody) Close() error             { return nil }

func TestBind_UnreadableBodyIs400WithCause(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Body = failingBody{}
	r.ContentLength = -1
	r.Header.Set("Content-Type", "application/json")
	var dst struct {
		Name string `json:"name"`
	}
	err := Bind(r, &dst)
	var he *Error
	if !errors.As(err, &he) || he.Code != http.StatusBadRequest {
		t.Fatalf("Bind unreadable body: %T %v, want *Error with code 400", err, err)
	}
	if !errors.Is(err, errBodyBroke) {
		t.Errorf("Bind unreadable body: read error not reachable through %v", err)
	}
}
