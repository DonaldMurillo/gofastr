package cache

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestGetOrSetValidatesDestinationBeforeFilling(t *testing.T) {
	c := NewMemoryCache()
	defer c.Close()
	ctx := context.Background()
	for _, dest := range []any{nil, (*int)(nil), 123} {
		err := GetOrSet(ctx, c, "key", 0, dest, func(context.Context) (any, error) {
			t.Error("loader called for invalid destination")
			return 42, nil
		})
		var invalid *json.InvalidUnmarshalError
		if !errors.As(err, &invalid) {
			t.Errorf("GetOrSet(%T) = %v; want invalid destination", dest, err)
		}
		if exists, err := c.Exists(ctx, "key"); err != nil || exists {
			t.Fatalf("Exists = %t, %v; invalid destination must not fill the cache", exists, err)
		}
	}
}

func TestGetOrSetHonorsCanceledContextOnMemoryHit(t *testing.T) {
	c := NewMemoryCache()
	defer c.Close()
	if err := c.Set(context.Background(), "key", 42, 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var dest int
	if err := GetOrSet(ctx, c, "key", 0, &dest, nil); !errors.Is(err, context.Canceled) || dest != 0 {
		t.Fatalf("GetOrSet = %d, %v; want unchanged destination and context.Canceled", dest, err)
	}
}
