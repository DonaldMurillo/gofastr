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
