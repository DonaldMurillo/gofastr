package cache

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// greedyScanner ignores the pattern and lists every key, as a scanner that drops the glob escapes can.
type greedyScanner struct{ *stubRedisClient }

func (g greedyScanner) Keys(context.Context, string) ([]string, error) {
	var out []string
	for k := range g.kv {
		out = append(out, k)
	}
	return out, nil
}

func TestPrimed304IsNotReplayed(t *testing.T) {
	c := NewMemoryCache(WithTTL(time.Minute))
	defer c.Close()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/doc", nil)
	_ = c.Set(context.Background(), getKey(req), cachedResponse{StatusCode: http.StatusNotModified, Headers: map[string][]string{}}, time.Minute)
	h := CacheMiddleware(c, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("fresh"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "fresh" {
		t.Fatalf("a stored 304 was replayed: %d %q", rec.Code, rec.Body.String())
	}
}

func TestRedisClearDeletesOnlyOwnedKeys(t *testing.T) {
	srv := greedyScanner{&stubRedisClient{}}
	ctx := context.Background()
	other := NewRedisCache(srv, WithPrefix("other"))
	if err := other.Set(ctx, "k", "v", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := NewRedisCache(srv, WithPrefix("mine")).Clear(ctx); err != nil {
		t.Fatal(err)
	}
	var v string
	if err := other.Get(ctx, "k", &v); err != nil {
		t.Fatalf("Clear of prefix mine deleted another prefix's key: %v", err)
	}
}
