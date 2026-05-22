package redisidempotency

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRedis is an in-memory RedisClient with SetNX semantics.
type fakeRedis struct {
	mu      sync.Mutex
	data    map[string]string
	calls   map[string]int
	setNXFn func(ctx context.Context, key, val string, ttl time.Duration) (bool, error)
}

func newFakeRedis() *fakeRedis {
	return &fakeRedis{data: map[string]string{}, calls: map[string]int{}}
}

func (f *fakeRedis) Get(ctx context.Context, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["Get"]++
	v, ok := f.data[key]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}
func (f *fakeRedis) Set(ctx context.Context, key, val string, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["Set"]++
	f.data[key] = val
	return nil
}
func (f *fakeRedis) Del(ctx context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["Del"]++
	delete(f.data, key)
	return nil
}
func (f *fakeRedis) Exists(ctx context.Context, key string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["Exists"]++
	_, ok := f.data[key]
	return ok, nil
}
func (f *fakeRedis) SetNX(ctx context.Context, key, val string, ttl time.Duration) (bool, error) {
	if f.setNXFn != nil {
		return f.setNXFn(ctx, key, val, ttl)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["SetNX"]++
	if _, ok := f.data[key]; ok {
		return false, nil
	}
	f.data[key] = val
	return true, nil
}

func (f *fakeRedis) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.data))
	for k := range f.data {
		out = append(out, k)
	}
	return out
}

// Finding 2: CheckAndReserve must be atomic (exactly-one-winner under race).
func TestCheckAndReserveAtomic(t *testing.T) {
	r := newFakeRedis()
	s := New(r, Config{})

	const N = 64
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			val, err := s.CheckAndReserve(context.Background(), "race-key")
			if err != nil {
				return
			}
			if val == nil {
				wins.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := wins.Load(); got != 1 {
		t.Fatalf("winners = %d, want exactly 1", got)
	}
}

// Finding 3: KeyPrefix must be stored and applied.
func TestKeyPrefixApplied(t *testing.T) {
	r := newFakeRedis()
	s := New(r, Config{KeyPrefix: "foo:"})

	_, err := s.CheckAndReserve(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	keys := r.keys()
	if len(keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1; %v", len(keys), keys)
	}
	if !strings.HasPrefix(keys[0], "foo:") {
		t.Fatalf("key %q does not have prefix %q", keys[0], "foo:")
	}
}

// Finding 14: Store must enforce MaxResponseSize.
func TestStoreEnforcesMaxResponseSize(t *testing.T) {
	r := newFakeRedis()
	s := New(r, Config{MaxResponseSize: 1024})

	big := make([]byte, 10*1024*1024) // 10MB
	err := s.Store(context.Background(), "k", big)
	if err == nil {
		t.Fatal("expected error for oversized body, got nil")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Finding 20: in-flight reservation sentinel must be distinguishable from
// a legitimate empty 204 response.
func TestReservationSentinelDistinctFromEmpty(t *testing.T) {
	r := newFakeRedis()
	s := New(r, Config{})

	// 1) Reserve creates an in-flight marker.
	val1, err := s.CheckAndReserve(context.Background(), "key-1")
	if err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	if val1 != nil {
		t.Fatalf("first reserve returned %q, want nil", val1)
	}

	// 2) Calling again before Store should signal in-flight, not a stored body.
	val2, err := s.CheckAndReserve(context.Background(), "key-1")
	if err == nil {
		// the in-flight signal can be either an error OR a sentinel value
		if val2 != nil {
			t.Fatalf("second reserve returned stored bytes %q, want in-flight signal", val2)
		}
		// Otherwise the caller has no way to distinguish "fresh insert" from
		// "in-flight already reserved" — both return (nil, nil).
		// We expect an error like "in flight" / "reserved".
		t.Fatal("second reserve returned (nil, nil) — indistinguishable from a stored empty body")
	}
	if !errors.Is(err, ErrInFlight) {
		t.Fatalf("expected ErrInFlight, got %v", err)
	}

	// 3) After Store with an empty body, CheckAndReserve must return (empty, nil)
	if err := s.Store(context.Background(), "key-1", []byte{}); err != nil {
		t.Fatalf("store empty: %v", err)
	}
	val3, err := s.CheckAndReserve(context.Background(), "key-1")
	if err != nil {
		t.Fatalf("third reserve: %v", err)
	}
	if val3 == nil {
		t.Fatal("stored-empty CheckAndReserve returned nil, want non-nil empty slice")
	}
	if len(val3) != 0 {
		t.Fatalf("stored body = %q, want empty", val3)
	}
}
