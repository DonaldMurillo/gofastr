package main

import (
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSONAnswersEncodingFailure(t *testing.T) {
	for _, value := range []any{math.NaN(), make(chan int)} {
		w := httptest.NewRecorder()
		writeJSON(w, value)
		if w.Code != http.StatusInternalServerError || w.Body.Len() == 0 {
			t.Fatalf("response = %d %q; want a nonempty 500", w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("missing no-store on failure: %v", w.Header())
		}
	}
}
