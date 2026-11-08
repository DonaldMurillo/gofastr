package cache

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// keyRecorder is a Cache that records every key and value Set receives.
type keyRecorder struct {
	*MemoryCache
	keys   []string
	values []any
}

func (recorder *keyRecorder) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	recorder.keys = append(recorder.keys, key)
	recorder.values = append(recorder.values, value)
	return recorder.MemoryCache.Set(ctx, key, value, ttl)
}

// TestVariantKeysHideHeaderValues: a variant's request header value
// reaches a key only through its digest, and the index stored under the
// URL's key carries no response header or body.
func TestVariantKeysHideHeaderValues(t *testing.T) {
	recorder := &keyRecorder{MemoryCache: NewMemoryCache(WithTTL(time.Hour))}
	t.Cleanup(func() { recorder.Close() })
	handler := CacheMiddleware(recorder, time.Hour)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Vary", "X-Team")
		w.Header().Set("X-Origin", "kept-on-variant")
		io.WriteString(w, r.Header.Get("X-Team"))
	}))
	request := httptest.NewRequest(http.MethodGet, "/page", nil)
	request.Header.Set("X-Team", "team-sentinel-value")
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if len(recorder.keys) != 2 {
		t.Fatalf("Set calls = %v, want a variant then its index", recorder.keys)
	}
	for _, key := range recorder.keys {
		if strings.Contains(key, "team-sentinel-value") {
			t.Errorf("key %q carries the request header value", key)
		}
	}
	index, ok := recorder.values[1].(varyIndex)
	if !ok || index.Generation == "" || recorder.keys[1] != getKey(request) {
		t.Fatalf("second Set = %q %#v, want the index under the URL key", recorder.keys[1], recorder.values[1])
	}
	var decoded cachedResponse
	if err := recorder.Get(context.Background(), getKey(request), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Headers != nil || decoded.Body != nil || decoded.StatusCode != 0 || lookupStored(context.Background(), recorder, request, getKey(request)) == nil {
		t.Errorf("index decoded as %#v, want no response of its own that still reaches the variant", decoded)
	}
	if variantMatches(request, index.Vary) {
		t.Error("an index's bare Vary names matched as a variant, so a reader without the index would replay it")
	}
	variant, ok := recorder.values[0].(cachedResponse)
	if !ok || variant.Headers["X-Origin"] == nil || string(variant.Body) != "team-sentinel-value" {
		t.Errorf("variant entry = %#v, want the full response", recorder.values[0])
	}
}
