package cache

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestCacheKeepsLargeIntegersExactUnderInterface pins that a value read
// back into an interface (map[string]any, *any, []any) keeps every
// integer exact. Both backends decoded with json.Unmarshal, which reads
// a number under an interface as float64, so a cached snowflake id
// 9007199254740993 came back as 9007199254740992: a different row's id,
// with no error. Typed destinations were never affected.
func TestCacheKeepsLargeIntegersExactUnderInterface(t *testing.T) {
	const big = int64(1)<<53 + 1 // 9007199254740993
	ctx := context.Background()
	backends := map[string]Cache{
		"memory":         NewMemoryCache(WithCleanupInterval(0)),
		"memory-bounded": NewMemoryCache(WithCleanupInterval(0), WithMaxEntries(10)),
		"redis":          NewRedisCache(newGoRedisLikeClient()),
	}
	for name, c := range backends {
		t.Run(name, func(t *testing.T) {
			if err := c.Set(ctx, "m", map[string]any{"id": big, "n": 1.5}, time.Minute); err != nil {
				t.Fatal(err)
			}
			var m map[string]any
			if err := c.Get(ctx, "m", &m); err != nil {
				t.Fatal(err)
			}
			if got, _ := json.Marshal(m["id"]); string(got) != "9007199254740993" {
				t.Errorf("map[string]any id = %s, want 9007199254740993", got)
			}
			if f, ok := m["n"].(float64); !ok || f != 1.5 {
				t.Errorf("a number float64 holds exactly should stay float64, got %T %v", m["n"], m["n"])
			}

			var a any
			if err := c.Get(ctx, "m", &a); err != nil {
				t.Fatal(err)
			}
			if got, _ := json.Marshal(a); string(got) != `{"id":9007199254740993,"n":1.5}` {
				t.Errorf("*any = %s", got)
			}

			// GetOrSet reads back the same way.
			var g map[string]any
			if err := GetOrSet(ctx, c, "g", time.Minute, &g, func(context.Context) (any, error) {
				return map[string]any{"id": big}, nil
			}); err != nil {
				t.Fatal(err)
			}
			if got, _ := json.Marshal(g["id"]); string(got) != "9007199254740993" {
				t.Errorf("GetOrSet id = %s, want 9007199254740993", got)
			}
		})
	}
}

// TestCacheNumberTypeBoundaryIsMagnitude pins the rule cache.md states:
// the line between float64 and json.Number under an interface is the
// magnitude 2^53, not whether a float64 holds the value. 1e20 and a
// UnixNano timestamp are exact float64 values and still come back as
// json.Number; 2^53-1 comes back as float64. The first cut of the docs
// said "a float64 when a float64 holds it", which code type-asserting a
// cached UnixNano to float64 would have trusted.
func TestCacheNumberTypeBoundaryIsMagnitude(t *testing.T) {
	ctx := context.Background()
	c := NewMemoryCache(WithCleanupInterval(0))
	in := map[string]any{"e20": 1e20, "nano": int64(1_700_000_000_000_000_000), "below": int64(1)<<53 - 1, "neg": -1e20}
	if err := c.Set(ctx, "b", in, time.Minute); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := c.Get(ctx, "b", &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"e20", "nano", "neg"} {
		if _, ok := m[k].(json.Number); !ok {
			t.Errorf("%s = %T %v, want json.Number (|v| >= 2^53)", k, m[k], m[k])
		}
	}
	if f, ok := m["below"].(float64); !ok || f != float64(int64(1)<<53-1) {
		t.Errorf("below = %T %v, want float64 2^53-1", m["below"], m["below"])
	}
}
