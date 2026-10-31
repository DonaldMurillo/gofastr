package handler

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A value JSON cannot encode (NaN, a channel) answers 500, not an empty
// 200 that a client reads as success (2026-10-27 P10).
func TestRespondEncodeFailureIs500(t *testing.T) {
	for name, out := range map[string]any{
		"nan":     map[string]any{"score": math.NaN()},
		"channel": map[string]any{"c": make(chan int)},
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			Respond(w, httptest.NewRequest(http.MethodGet, "/", nil), out)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status %d, want 500; body %q", w.Code, w.Body.String())
			}
			if body := w.Body.String(); strings.Contains(body, "NaN") || strings.Contains(body, "unsupported") {
				t.Fatalf("the encode error leaked into the body: %s", body)
			}
		})
	}

	// An encodable value keeps the encoder's shape, trailing newline included.
	w := httptest.NewRecorder()
	Respond(w, httptest.NewRequest(http.MethodGet, "/", nil), map[string]int{"n": 1})
	if w.Code != http.StatusOK || w.Body.String() != "{\"n\":1}\n" {
		t.Fatalf("ok value: %d %q", w.Code, w.Body.String())
	}
}
