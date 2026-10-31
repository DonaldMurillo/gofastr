package webmcp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// A tool that sent 103 Early Hints was reported as status 103 with no
// error class, whatever it answered after.
func TestObserverReportsStatusAfterHints(t *testing.T) {
	var log eventLog
	h := New(WithObserver(log.observe))
	rt := router.New()
	if err := h.Handle(rt, validTool(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusEarlyHints)
		w.WriteHeader(http.StatusConflict)
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/echo", nil)
	req.Header.Set(MarkerHeader, "1")
	rt.ServeHTTP(httptest.NewRecorder(), req)
	inv := log.byPhase(PhaseInvoke)
	if len(inv) != 1 || inv[0].StatusCode != http.StatusConflict || inv[0].ErrClass != "http_409" {
		t.Fatalf("events after hints: %+v", inv)
	}
}
