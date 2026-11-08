package cache

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBypassesAdvertiseCacheStatus(t *testing.T) {
	memory := NewMemoryCache()
	defer memory.Close()
	calls := 0
	handler := CacheMiddleware(memory, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusAccepted) }))
	for _, method := range []string{http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions, http.MethodTrace, http.MethodGet} {
		request := httptest.NewRequest(method, "http://example.com/resource", nil)
		if method == http.MethodGet {
			request.Header.Set("Range", "bytes=0-1")
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Header().Get("X-Cache") != "BYPASS" || response.Code != http.StatusAccepted {
			t.Errorf("%s got %d X-Cache=%q", method, response.Code, response.Header().Get("X-Cache"))
		}
	}
	if calls != 7 {
		t.Errorf("origin called %d times, want 7", calls)
	}
}
