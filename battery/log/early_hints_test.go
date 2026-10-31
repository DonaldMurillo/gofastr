package log

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A 103 Early Hints before the real answer is neither the logged status
// nor the one the client gets.
func TestAccessLogKeepsStatusAfterHints(t *testing.T) {
	sink := &memSink{}
	logger := slog.New(newFanoutHandler([]Sink{sink}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	srv := httptest.NewServer(accessMiddleware(logger, false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusEarlyHints)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "ok")
	})))
	t.Cleanup(srv.Close)
	resp, err := srv.Client().Get(srv.URL + "/things")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("client got %d, want 201", resp.StatusCode)
	}
	lines := sink.lines()
	if len(lines) != 1 {
		t.Fatalf("want 1 entry, got %d", len(lines))
	}
	var got map[string]any
	if err := json.Unmarshal(lines[0], &got); err != nil {
		t.Fatal(err)
	}
	if got["status"] != float64(http.StatusCreated) {
		t.Fatalf("logged status = %v, want 201", got["status"])
	}
}
