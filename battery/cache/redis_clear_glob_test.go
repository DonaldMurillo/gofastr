package cache

import (
	"context"
	"errors"
	"path"
	"testing"
	"time"
)

// globScanner matches SCAN patterns the way Redis does: *, ? and [...]
// are wildcards and a backslash escapes the next byte.
type globScanner struct{ *stubRedisClient }

func (g globScanner) Keys(_ context.Context, pattern string) ([]string, error) {
	var out []string
	for k := range g.kv {
		if ok, err := path.Match(pattern, k); err != nil {
			return nil, err
		} else if ok {
			out = append(out, k)
		}
	}
	return out, nil
}

// A prefix holding glob characters still clears its own keys: the scan
// read "a[b]c:*" as a character class, matched none of the literal
// "a[b]c:" keys, and Clear returned nil with every entry still stored.
func TestRedisClearGlobPrefix(t *testing.T) {
	for _, prefix := range []string{"a[b]c", "t*", "q?", `back\slash`} {
		srv := globScanner{&stubRedisClient{}}
		c := NewRedisCache(srv, WithPrefix(prefix))
		ctx := context.Background()
		if err := c.Set(ctx, "k", "v", time.Minute); err != nil {
			t.Fatal(err)
		}
		if err := c.Clear(ctx); err != nil {
			t.Fatalf("%q: Clear: %v", prefix, err)
		}
		var v string
		if err := c.Get(ctx, "k", &v); !errors.Is(err, ErrCacheMiss) {
			t.Errorf("prefix %q: Clear left the entry (Get = %q, %v)", prefix, v, err)
		}
	}
}
