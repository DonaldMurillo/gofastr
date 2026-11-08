package cache_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/cache"
)

// themeOrigin answers GET with the request's X-Theme and the given
// version, declaring Vary: X-Theme when vary is set. An unsafe request
// bumps the version.
func themeOrigin(vary bool, calls *atomic.Int32) http.Handler {
	var version atomic.Int32
	version.Store(1)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			version.Add(1)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		calls.Add(1)
		if vary {
			w.Header().Set("Vary", "X-Theme")
		}
		io.WriteString(w, "theme="+r.Header.Get("X-Theme")+" v"+strconv.Itoa(int(version.Load())))
	})
}

// themeRequest sends method to url over a real connection with X-Theme
// set and returns the body and X-Cache.
func themeRequest(t *testing.T, method, url, theme string) (string, string) {
	t.Helper()
	request, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Theme", theme)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body), response.Header.Get("X-Cache")
}

// TestVaryVariantsKeepOwnEntries is C06: light, light, dark, light gave
// MISS HIT MISS MISS with an hour TTL and room for 32 entries, because
// the dark variant overwrote the light one in a single slot.
func TestVaryVariantsKeepOwnEntries(t *testing.T) {
	memory := cache.NewMemoryCache(cache.WithTTL(time.Hour), cache.WithMaxEntries(32))
	t.Cleanup(func() { memory.Close() })
	var calls atomic.Int32
	server := httptest.NewServer(cache.CacheMiddleware(memory, time.Hour)(themeOrigin(true, &calls)))
	t.Cleanup(server.Close)

	var states []string
	for _, theme := range []string{"light", "light", "dark", "light", "dark"} {
		body, state := themeRequest(t, http.MethodGet, server.URL+"/page", theme)
		if body != "theme="+theme+" v1" {
			t.Fatalf("%s got body %q", theme, body)
		}
		states = append(states, state)
	}
	if want := []string{"MISS", "HIT", "MISS", "HIT", "HIT"}; !slices.Equal(states, want) {
		t.Errorf("X-Cache = %v, want %v", states, want)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("origin ran %d times, want once per variant", got)
	}
}

// TestNoVaryResponseSharedAcrossHeaders is the control: without Vary the
// header is not part of the representation, so every request after the
// first is a HIT of the first body.
func TestNoVaryResponseSharedAcrossHeaders(t *testing.T) {
	memory := cache.NewMemoryCache(cache.WithTTL(time.Hour))
	t.Cleanup(func() { memory.Close() })
	var calls atomic.Int32
	server := httptest.NewServer(cache.CacheMiddleware(memory, time.Hour)(themeOrigin(false, &calls)))
	t.Cleanup(server.Close)

	var states []string
	for _, theme := range []string{"light", "dark", "light"} {
		body, state := themeRequest(t, http.MethodGet, server.URL+"/page", theme)
		if body != "theme=light v1" {
			t.Fatalf("%s got body %q", theme, body)
		}
		states = append(states, state)
	}
	if want := []string{"MISS", "HIT", "HIT"}; !slices.Equal(states, want) {
		t.Errorf("X-Cache = %v, want %v", states, want)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("origin ran %d times, want 1", got)
	}
}

// TestUnsafeRequestEvictsEveryVariant: a DELETE evicts every stored
// variant of the URL, and re-storing one variant afterwards must not
// bring the other's pre-DELETE body back.
func TestUnsafeRequestEvictsEveryVariant(t *testing.T) {
	memory := cache.NewMemoryCache(cache.WithTTL(time.Hour))
	t.Cleanup(func() { memory.Close() })
	var calls atomic.Int32
	server := httptest.NewServer(cache.CacheMiddleware(memory, time.Hour)(themeOrigin(true, &calls)))
	t.Cleanup(server.Close)

	for _, theme := range []string{"light", "dark", "light", "dark"} {
		themeRequest(t, http.MethodGet, server.URL+"/page", theme)
	}
	themeRequest(t, http.MethodDelete, server.URL+"/page", "light")

	steps := []struct{ theme, body, state string }{
		{"light", "theme=light v2", "MISS"},
		{"dark", "theme=dark v2", "MISS"},
		{"light", "theme=light v2", "HIT"},
		{"dark", "theme=dark v2", "HIT"},
	}
	for _, step := range steps {
		body, state := themeRequest(t, http.MethodGet, server.URL+"/page", step.theme)
		if body != step.body || state != step.state {
			t.Errorf("%s after DELETE = %q %s, want %q %s", step.theme, body, state, step.body, step.state)
		}
	}
}

// TestVaryVariantsExpireWithTTL: every variant entry ends at the
// middleware TTL, the index included, and the next request refetches.
func TestVaryVariantsExpireWithTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		memory := cache.NewMemoryCache(cache.WithTTL(time.Hour))
		defer memory.Close()
		var calls atomic.Int32
		handler := cache.CacheMiddleware(memory, time.Minute)(themeOrigin(true, &calls))
		serve := func(theme string) string {
			request := httptest.NewRequest(http.MethodGet, "/page", nil)
			request.Header.Set("X-Theme", theme)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			return recorder.Header().Get("X-Cache")
		}
		serve("light")
		serve("dark")
		time.Sleep(30 * time.Second)
		if got := []string{serve("light"), serve("dark")}; !slices.Equal(got, []string{"HIT", "HIT"}) {
			t.Fatalf("inside the TTL = %v, want HIT HIT", got)
		}
		time.Sleep(31 * time.Second)
		if got := []string{serve("light"), serve("dark")}; !slices.Equal(got, []string{"MISS", "MISS"}) {
			t.Errorf("past the TTL = %v, want MISS MISS", got)
		}
	})
}
