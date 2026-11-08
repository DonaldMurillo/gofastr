package cache

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type freshnessCache struct {
	Cache
	ttls []time.Duration
}

func (c *freshnessCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	c.ttls = append(c.ttls, ttl)
	return c.Cache.Set(ctx, key, value, ttl)
}

func TestOriginFreshnessBoundsReuse(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cc        []string
		age       string
		outer     bool
		ttl, want time.Duration
	}{
		{name: "zero", cc: []string{"public, max-age=0"}, ttl: time.Minute},
		{name: "revalidate zero", cc: []string{"must-revalidate, max-age=0"}, ttl: time.Minute},
		{name: "shared zero", cc: []string{"max-age=60, s-maxage=0"}, ttl: time.Minute},
		{name: "short positive", cc: []string{"max-age=7"}, ttl: time.Minute, want: 7 * time.Second},
		{name: "quoted", cc: []string{`max-age="11"`}, ttl: time.Minute, want: 11 * time.Second},
		{name: "shared priority", cc: []string{"max-age=0", "S-MAXAGE=13"}, ttl: time.Minute, want: 13 * time.Second},
		{name: "configured cap", cc: []string{"max-age=100"}, ttl: 17 * time.Second, want: 17 * time.Second},
		{name: "aged", cc: []string{"s-maxage=40"}, age: "23", ttl: time.Minute, want: 17 * time.Second},
		{name: "already stale", cc: []string{"max-age=19"}, age: "20", ttl: time.Minute},
		{name: "invalid", cc: []string{"max-age=oops"}, ttl: time.Minute},
		{name: "negative", cc: []string{"max-age=-1"}, ttl: time.Minute},
		{name: "duplicate", cc: []string{"max-age=20", "max-age=30"}, ttl: time.Minute},
		{name: "no expiry config", cc: []string{"max-age=29"}, want: 29 * time.Second},
		{name: "huge lifetime", cc: []string{"max-age=18446744073709551615"}, ttl: 31 * time.Second, want: 31 * time.Second},
		{name: "overflow lifetime", cc: []string{"max-age=18446744073709551616"}, ttl: time.Minute},
		{name: "bad age", cc: []string{"max-age=70"}, age: "not-a-number", ttl: time.Minute},
		{name: "outer zero", cc: []string{"max-age=0"}, outer: true, ttl: time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			memory := NewMemoryCache()
			defer memory.Close()
			cache := &freshnessCache{Cache: memory}
			calls := 0
			handler := CacheMiddleware(cache, tc.ttl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if !tc.outer {
					for _, value := range tc.cc {
						w.Header().Add("Cache-Control", value)
					}
				}
				if tc.age != "" {
					w.Header().Set("Age", tc.age)
				}
				w.Write([]byte("fresh"))
			}))
			for i := 0; i < 2; i++ {
				response := httptest.NewRecorder()
				if tc.outer {
					for _, value := range tc.cc {
						response.Header().Add("Cache-Control", value)
					}
				}
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.com/page", nil))
			}
			if tc.want == 0 {
				if calls != 2 || len(cache.ttls) != 0 {
					t.Errorf("stale response reused: origin=%d TTLs=%v", calls, cache.ttls)
				}
			} else if calls != 1 || len(cache.ttls) != 1 || cache.ttls[0] != tc.want {
				t.Errorf("origin=%d TTLs=%v, want one store at %s", calls, cache.ttls, tc.want)
			}
		})
	}
}

func TestOriginDatesLimitFreshness(t *testing.T) {
	for _, cc := range []string{"max-age=30", "s-maxage=30", ""} {
		memory := NewMemoryCache()
		defer memory.Close()
		cache := &freshnessCache{Cache: memory}
		calls := 0
		origin := CacheMiddleware(cache, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Date", time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat))
			if cc != "" {
				w.Header().Set("Cache-Control", cc)
			} else {
				w.Header().Set("Expires", time.Now().Add(-time.Second).UTC().Format(http.TimeFormat))
			}
			w.Write([]byte("stale"))
		}))
		for range 2 {
			origin.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://example.com/stale", nil))
		}
		if calls != 2 || len(cache.ttls) != 0 {
			t.Errorf("%q reused stale origin: calls=%d TTLs=%v", cc, calls, cache.ttls)
		}
	}
}
