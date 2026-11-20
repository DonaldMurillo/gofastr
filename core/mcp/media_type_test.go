package mcp

import (
	"net/http/httptest"
	"testing"
)

func TestWantsSSEMixedCase(t *testing.T) {
	for _, accept := range []string{"Text/Event-Stream", "text/event-stream", "", "application/json"} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept", accept)
		want := accept == "Text/Event-Stream" || accept == "text/event-stream"
		if got := wantsSSE(r); got != want {
			t.Errorf("Accept %q: got %v, want %v", accept, got, want)
		}
	}
}
