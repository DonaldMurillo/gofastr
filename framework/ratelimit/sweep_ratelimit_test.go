package ratelimit

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRetryAfterSecondsEdges(t *testing.T) {
	for d, want := range map[time.Duration]string{
		-1500 * time.Millisecond:     "0",
		0:                            "0",
		time.Nanosecond:              "1",
		time.Duration(math.MaxInt64): "9223372037",
	} {
		if got := RetryAfterSeconds(d); got != want {
			t.Errorf("RetryAfterSeconds(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestWindowDropsOnlyStaleAttempts(t *testing.T) {
	rl := NewLimiter(Config{MaxAttempts: 2, Window: 100 * time.Millisecond, BlockDuration: time.Hour})
	rl.Allow("k")
	time.Sleep(60 * time.Millisecond)
	rl.Allow("k")
	time.Sleep(60 * time.Millisecond)
	if ok, _ := rl.Allow("k"); !ok {
		t.Fatal("an attempt older than Window still counted")
	}
}

func TestEvictKeepsBlockOutlastingWindow(t *testing.T) {
	rl := NewLimiter(Config{MaxAttempts: 1, Window: time.Minute, BlockDuration: time.Hour})
	now := time.Now()
	rl.states["victim"] = &rlState{attempts: []time.Time{now.Add(-time.Hour)}, blockedUntil: now.Add(time.Hour), blockOrder: 1}
	rl.mu.Lock()
	rl.evictLocked(now)
	rl.mu.Unlock()
	if _, ok := rl.states["victim"]; !ok {
		t.Fatal("eviction dropped an active block whose attempts left the window")
	}
}

func TestShedBreaksExpiryTiesNewestFirstDownToLowWater(t *testing.T) {
	rl := NewLimiter(Config{MaxAttempts: 1, Window: time.Minute, BlockDuration: time.Hour})
	now := time.Now()
	until := now.Add(time.Hour)
	for i := range maxKeys {
		rl.states[uniqueKey(i)] = &rlState{blockedUntil: until, blockOrder: uint64(i + 2)}
	}
	rl.states[uniqueKey(0)].blockOrder = 1
	rl.mu.Lock()
	rl.evictLocked(now)
	rl.mu.Unlock()
	if _, ok := rl.states[uniqueKey(0)]; !ok {
		t.Fatal("a same-tick tie shed the oldest block")
	}
	if got := len(rl.states); got != maxKeys*9/10 {
		t.Fatalf("shed left %d entries, want the low-water mark %d", got, maxKeys*9/10)
	}
}

func TestNewLimiterFillsDefaults(t *testing.T) {
	rl := NewLimiter(Config{})
	if rl.cfg.MaxAttempts != 10 || rl.cfg.Window != 15*time.Minute || rl.cfg.BlockDuration != 30*time.Minute {
		t.Fatalf("defaults = %d/%v/%v, want 10/15m/30m", rl.cfg.MaxAttempts, rl.cfg.Window, rl.cfg.BlockDuration)
	}
}

func TestAdmitReportsStoreErrorAndDenies(t *testing.T) {
	rl := NewLimiter(Config{Store: errorStore{}})
	ok, retry, err := rl.Admit(context.Background(), "k")
	if ok || err == nil || retry != storeErrRetryAfter {
		t.Fatalf("Admit on a failing store = %v, %v, %v; want false, %v, an error", ok, retry, err, storeErrRetryAfter)
	}
}

func TestMiddlewareSendsNoBudgetHeaders(t *testing.T) {
	rl := NewLimiter(Config{MaxAttempts: 1, Window: time.Hour, BlockDuration: time.Hour})
	h := rl.MiddlewareByKey(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for i, step := range []struct {
		addr string
		code int
	}{{"1.2.3.4:1", 200}, {"1.2.3.4:1", 429}, {"5.6.7.8:1", 200}} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = step.addr
		h.ServeHTTP(rec, req)
		for _, name := range []string{"RateLimit-Limit", "RateLimit-Remaining", "RateLimit-Reset", "X-RateLimit-Remaining"} {
			if rec.Header().Get(name) != "" {
				t.Fatalf("response %d carries %s", i, name)
			}
		}
		if rec.Code != step.code {
			t.Fatalf("a nil keyFunc must key on the client IP; request %d from %s got %d, want %d", i, step.addr, rec.Code, step.code)
		}
	}
}
