package cache

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// valueCache is a valid Cache implementation with value receivers. Separate
// instances share a concrete type but keep independent storage.
type valueCache struct{ values map[string][]byte }

func newValueCache() valueCache {
	return valueCache{values: map[string][]byte{}}
}

func (c valueCache) Get(ctx context.Context, key string, dest any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, ok := c.values[key]
	if !ok {
		return ErrCacheMiss
	}
	return json.Unmarshal(data, dest)
}

func (c valueCache) Set(ctx context.Context, key string, value any, _ time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	c.values[key] = data
	return nil
}

func (c valueCache) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	delete(c.values, key)
	return nil
}

func (c valueCache) Exists(ctx context.Context, key string) (bool, error) {
	var value json.RawMessage
	err := c.Get(ctx, key, &value)
	if err == ErrCacheMiss {
		return false, nil
	}
	return err == nil, err
}

func (c valueCache) Clear(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for key := range c.values {
		delete(c.values, key)
	}
	return nil
}

func TestGetOrSetDoesNotJoinSeparateValueCacheInstances(t *testing.T) {
	cacheA, cacheB := newValueCache(), newValueCache()
	started := make(chan struct{})
	release := make(chan struct{})
	doneA := make(chan error, 1)
	go func() {
		var got string
		doneA <- GetOrSet(context.Background(), cacheA, "shared", time.Minute, &got, func(context.Context) (any, error) {
			close(started)
			<-release
			return "a", nil
		})
	}()
	<-started

	type result struct {
		value string
		err   error
	}
	doneB := make(chan result, 1)
	go func() {
		var got string
		err := GetOrSet(context.Background(), cacheB, "shared", time.Minute, &got, func(context.Context) (any, error) {
			return "b", nil
		})
		doneB <- result{value: got, err: err}
	}()

	select {
	case got := <-doneB:
		close(release)
		if got.err != nil || got.value != "b" {
			t.Fatalf("independent value cache fill = (%q, %v), want (b, nil)", got.value, got.err)
		}
	case <-time.After(50 * time.Millisecond):
		close(release)
		<-doneA
		got := <-doneB
		if got.err != nil {
			t.Fatalf("second cache instance joined another instance's fill: %v", got.err)
		}
		t.Fatalf("second cache instance waited for a different cache's fill and got %q", got.value)
	}
	if err := <-doneA; err != nil {
		t.Fatalf("first cache fill: %v", err)
	}
}
