package cache

import (
	"context"
	"testing"
	"time"
)

// Doubling ':' inside the prefix was not injective either: WithPrefix("u")
// with key ":alice:x" and WithPrefix("u:alice") with key "x" both wrote
// "u::alice:x", and Clear on "u" (scanning "u:*") deleted the other
// namespace's entry.
func TestRedisPrefixEscapeIsInjective(t *testing.T) {
	ctx := context.Background()
	srv := &stubRedisClient{}
	outer := NewRedisCache(srv, WithPrefix("u"))
	inner := NewRedisCache(srv, WithPrefix("u:alice"))

	if err := inner.Set(ctx, "x", "inner", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := outer.Set(ctx, ":alice:x", "outer", time.Minute); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := inner.Get(ctx, "x", &got); err != nil || got != "inner" {
		t.Fatalf("inner Get = %q, %v; the outer Set overwrote it", got, err)
	}
	if err := outer.Delete(ctx, ":alice:x"); err != nil {
		t.Fatal(err)
	}
	if ok, err := inner.Exists(ctx, "x"); err != nil || !ok {
		t.Fatalf("inner entry gone after the outer Delete (exists=%v, err=%v)", ok, err)
	}
	if err := outer.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, err := inner.Exists(ctx, "x"); err != nil || !ok {
		t.Fatalf("Clear on %q deleted the %q entry (exists=%v, err=%v)", "u", "u:alice", ok, err)
	}
}

// A '%' in a prefix is escaped too, or "a:b" (escaped to "a%3Ab") would
// meet a literal prefix "a%3Ab".
func TestRedisPrefixEscapesPercent(t *testing.T) {
	ctx := context.Background()
	srv := &stubRedisClient{}
	a := NewRedisCache(srv, WithPrefix("a:b"))
	b := NewRedisCache(srv, WithPrefix("a%3Ab"))
	if err := a.Set(ctx, "k", "a", time.Minute); err != nil {
		t.Fatal(err)
	}
	if ok, _ := b.Exists(ctx, "k"); ok {
		t.Fatal(`WithPrefix("a%3Ab") sees WithPrefix("a:b")'s entry`)
	}
	if got := NewRedisCache(srv, WithPrefix("tenant-7")).prefixedKey("k"); got != "tenant-7:k" {
		t.Fatalf("a plain prefix changed shape: %q", got)
	}
}
