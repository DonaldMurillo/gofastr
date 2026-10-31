package cache

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A 103 Early Hints before the real answer is not what gets cached: the
// first response and the replay both carry the handler's 201.
func TestCacheKeepsStatusAfterHints(t *testing.T) {
	srv := httptest.NewServer(CacheMiddleware(NewMemoryCache(WithTTL(time.Minute)), time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.WriteHeader(http.StatusEarlyHints)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "made")
	})))
	t.Cleanup(srv.Close)
	for i, want := range []string{"MISS", "HIT"} {
		resp, err := srv.Client().Get(srv.URL + "/thing")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusCreated || string(body) != "made" {
			t.Fatalf("request %d: %d %q, want 201 \"made\"", i+1, resp.StatusCode, body)
		}
		if got := resp.Header.Get("X-Cache"); got != want {
			t.Fatalf("request %d: X-Cache = %q, want %q", i+1, got, want)
		}
	}
}
