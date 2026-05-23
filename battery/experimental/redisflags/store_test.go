package redisflags

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRedis is an in-memory RedisClient that supports Scan + MGet.
type fakeRedis struct {
	mu       sync.Mutex
	data     map[string]string
	getErr   error
	scanCall int
	keysCall int
	mgetCall int
}

func newFakeRedis() *fakeRedis {
	return &fakeRedis{data: map[string]string{}}
}

func (f *fakeRedis) Get(ctx context.Context, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return "", f.getErr
	}
	v, ok := f.data[key]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (f *fakeRedis) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[key] = value
	return nil
}

func (f *fakeRedis) Del(ctx context.Context, keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		delete(f.data, k)
	}
	return nil
}

func (f *fakeRedis) Keys(ctx context.Context, pattern string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keysCall++
	prefix := strings.TrimSuffix(pattern, "*")
	out := []string{}
	for k := range f.data {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out, nil
}

func (f *fakeRedis) Scan(ctx context.Context, cursor uint64, pattern string, count int64) (uint64, []string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scanCall++
	prefix := strings.TrimSuffix(pattern, "*")
	out := []string{}
	for k := range f.data {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return 0, out, nil // single-shot scan
}

func (f *fakeRedis) MGet(ctx context.Context, keys ...string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mgetCall++
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = f.data[k]
	}
	return out, nil
}

// Finding 4: Get must surface non-not-found errors instead of returning nil.
func TestGetSurfacesRedisError(t *testing.T) {
	r := newFakeRedis()
	r.getErr = errors.New("connection refused")
	s := New(r, Config{})

	flag, err := s.Get(context.Background(), "anything")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if flag != nil {
		t.Fatalf("flag = %+v, want nil on error", flag)
	}
}

// Get should still treat ErrNotFound as (nil, nil) — "absent, not failed".
func TestGetReturnsNilOnNotFound(t *testing.T) {
	r := newFakeRedis()
	s := New(r, Config{})
	flag, err := s.Get(context.Background(), "missing")
	if err != nil {
		t.Fatalf("expected nil error for missing key, got %v", err)
	}
	if flag != nil {
		t.Fatalf("flag = %+v, want nil", flag)
	}
}

// Finding 15: List must use Scan + MGet, never KEYS.
func TestListUsesScanNotKeys(t *testing.T) {
	r := newFakeRedis()
	s := New(r, Config{})

	// seed
	flag := &Flag{Key: "a", Enabled: true}
	if err := s.Set(context.Background(), flag); err != nil {
		t.Fatal(err)
	}

	if _, err := s.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.keysCall != 0 {
		t.Fatalf("List called KEYS %d times (must be 0)", r.keysCall)
	}
	if r.scanCall == 0 {
		t.Fatal("List did not call Scan")
	}
}

// Finding 19: Set must validate RolloutPct ∈ [0, 100].
func TestSetValidatesRolloutPct(t *testing.T) {
	r := newFakeRedis()
	s := New(r, Config{})

	bad := &Flag{Key: "x", RolloutPct: 200}
	if err := s.Set(context.Background(), bad); err == nil {
		t.Fatal("expected validation error for RolloutPct=200")
	}

	bad2 := &Flag{Key: "x", RolloutPct: -1}
	if err := s.Set(context.Background(), bad2); err == nil {
		t.Fatal("expected validation error for RolloutPct=-1")
	}

	good := &Flag{Key: "x", RolloutPct: 50}
	if err := s.Set(context.Background(), good); err != nil {
		t.Fatalf("unexpected error for valid pct: %v", err)
	}
}

// sanity: List round-trips one flag
func TestListReturnsStoredFlag(t *testing.T) {
	r := newFakeRedis()
	s := New(r, Config{})
	in := &Flag{Key: "a", Enabled: true}
	if err := s.Set(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	out, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d flags, want 1", len(out))
	}
	if out[0].Key != "a" || !out[0].Enabled {
		// pretty print
		b, _ := json.Marshal(out[0])
		t.Fatalf("flag = %s", b)
	}
}
