package handler

import (
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
