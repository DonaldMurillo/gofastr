package auth

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// The shared store used to count and then insert in two autocommit
// statements, so every caller in a concurrent burst read the same
// pre-insert count and was admitted. The audit measured 19 to 34
// admissions out of 50 against MaxAttempts=10 on Postgres. These tests
// release one barrier across two store handles (two replicas) and
// assert the budget holds exactly.

// burstAdmitted fires n goroutines released by one barrier, spread
// round-robin over the stores, each calling Allow on the same key.
func burstAdmitted(t *testing.T, stores []*SQLRateLimitStore, key string, cfg RateLimiterConfig, n int) int64 {
	t.Helper()
	var admitted atomic.Int64
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	ready.Add(n)
	done.Add(n)
	for i := range n {
		s := stores[i%len(stores)]
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			ok, _, err := s.Allow(context.Background(), key, cfg)
			if err != nil {
				t.Errorf("Allow: %v", err)
				return
			}
			if ok {
				admitted.Add(1)
			}
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	return admitted.Load()
}

func assertBurstCapped(t *testing.T, stores []*SQLRateLimitStore) {
	t.Helper()
	cfg := RateLimiterConfig{MaxAttempts: 10, Window: time.Minute, BlockDuration: 15 * time.Minute, Scope: "twofa"}
	for trial := range 5 {
		key := fmt.Sprintf("twofa|203.0.113.%d", trial+1)
		got := burstAdmitted(t, stores, key, cfg, 50)
		// At most MaxAttempts. Fewer is allowed: when every insert of an
		// over-budget burst commits before any count runs, all of them see
		// the full burst and all are denied. The key blocks either way.
		if got > int64(cfg.MaxAttempts) {
			t.Fatalf("trial %d: %d of 50 concurrent attempts admitted, MaxAttempts=%d", trial, got, cfg.MaxAttempts)
		}
		// The key is blocked after the burst on every replica.
		for i, s := range stores {
			if ok, _, err := s.Allow(context.Background(), key, cfg); err != nil || ok {
				t.Fatalf("trial %d: replica %d admitted after the burst: ok=%v err=%v", trial, i, ok, err)
			}
		}
	}
}

func TestSQLRateLimitBurstCappedSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rl.db")
	open := func() *sql.DB {
		db, err := sql.Open("sqlite3", "file:"+path+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)")
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		db.SetMaxOpenConns(16)
		t.Cleanup(func() { _ = db.Close() })
		return db
	}
	a := NewSQLRateLimitStore(open(), "auth_rate_limits")
	b := NewSQLRateLimitStore(open(), "auth_rate_limits")
	if err := a.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	assertBurstCapped(t, []*SQLRateLimitStore{a, b})
}

// Clearing an expired block must only drop the attempts that block
// covered. A concurrent caller may already have recorded attempts after
// the expiry; erasing those by key would hand out a second budget.
func TestSQLRateLimitExpiryKeepsNewerAttempts(t *testing.T) {
	s, db := newRLStore(t)
	cfg := rlTestConfig() // MaxAttempts 3, Window 1h
	blockedUntil := time.Now().Add(-time.Minute).UnixMilli()
	if _, err := db.Exec("INSERT INTO auth_rate_limits (rl_key, blocked_until_ms) VALUES ('ip:8.8.8.8', $1)", blockedUntil); err != nil {
		t.Fatalf("seed block: %v", err)
	}
	// Three attempts recorded after the block expired: the budget is spent.
	after := time.Now().Add(-30 * time.Second).UnixMilli()
	for range 3 {
		if _, err := db.Exec("INSERT INTO auth_rate_limits_attempts (rl_key, attempted_at_ms) VALUES ('ip:8.8.8.8', $1)", after); err != nil {
			t.Fatalf("seed attempt: %v", err)
		}
	}
	ok, _, err := s.Allow(context.Background(), "ip:8.8.8.8", cfg)
	if err != nil || ok {
		t.Fatalf("post-expiry attempts must still count: ok=%v err=%v", ok, err)
	}
}

func TestSQLRateLimitBurstCappedPostgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	open := func() *sql.DB {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Fatalf("open pg: %v", err)
		}
		db.SetMaxOpenConns(20)
		t.Cleanup(func() { _ = db.Close() })
		return db
	}
	table := fmt.Sprintf("rl_burst_%d", time.Now().UnixNano())
	a := NewSQLRateLimitStore(open(), table)
	b := NewSQLRateLimitStore(open(), table)
	if err := a.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	t.Cleanup(func() {
		db := open()
		_, _ = db.Exec("DROP TABLE IF EXISTS " + table)
		_, _ = db.Exec("DROP TABLE IF EXISTS " + table + "_attempts")
	})
	assertBurstCapped(t, []*SQLRateLimitStore{a, b})
}
