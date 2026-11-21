package auth

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/DonaldMurillo/gofastr/framework/ratelimit"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// Regression test from the 2026-11-01 battery/auth bug bash: the scoped
// sweep deleted expired block rows but kept the attempts each block
// covered, while the per-key path deletes both. With BlockDuration
// shorter than Window, a key whose block had just expired came back
// after any sweep to find its old attempts still counted and was
// blocked again on its first try; the in-memory limiter admits it.
func TestSQLRateLimitSweepClearsAttemptsUnderExpiredBlock(t *testing.T) {
	s, db := newRLStore(t)
	checkSweepClearsCoveredAttempts(t, s, db, "auth_rate_limits")
}

func TestSQLRateLimitSweepClearsAttemptsUnderExpiredBlockPostgres(t *testing.T) {
	dsn, _, err := pgtest.ResolveEnv()
	if err != nil {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open pg: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	table := fmt.Sprintf("rl_sweep_%d", time.Now().UnixNano())
	s := NewSQLRateLimitStore(db, table)
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + table)
		_, _ = db.Exec("DROP TABLE IF EXISTS " + table + "_attempts")
	})
	checkSweepClearsCoveredAttempts(t, s, db, table)
}

func checkSweepClearsCoveredAttempts(t *testing.T, s *SQLRateLimitStore, db *sql.DB, table string) {
	t.Helper()
	ctx := context.Background()
	cfg := RateLimiterConfig{MaxAttempts: 3, Window: time.Hour, BlockDuration: time.Minute, Scope: "login_ip"}
	key := "login_ip|203.0.113.9"
	other := "login_ip|203.0.113.10"

	// Four attempts ten minutes ago tripped a one-minute block that
	// expired nine minutes ago. The attempts are still inside the
	// one-hour window.
	attemptAt := time.Now().Add(-10 * time.Minute).UnixMilli()
	blockedUntil := time.Now().Add(-9 * time.Minute).UnixMilli()
	for _, k := range []string{key, other} {
		for range 4 {
			if _, err := db.Exec(fmt.Sprintf("INSERT INTO %s_attempts (rl_key, attempted_at_ms) VALUES ($1, $2)", table), k, attemptAt); err != nil {
				t.Fatalf("seed attempt: %v", err)
			}
		}
		if _, err := db.Exec(fmt.Sprintf("INSERT INTO %s (rl_key, blocked_until_ms) VALUES ($1, $2)", table), k, blockedUntil); err != nil {
			t.Fatalf("seed block: %v", err)
		}
	}
	// One attempt AFTER the block expired is a fresh one and must keep
	// counting: the sweep clears only what a block covered.
	if _, err := db.Exec(fmt.Sprintf("INSERT INTO %s_attempts (rl_key, attempted_at_ms) VALUES ($1, $2)", table), other, time.Now().Add(-time.Minute).UnixMilli()); err != nil {
		t.Fatalf("seed post-expiry attempt: %v", err)
	}

	// A fresh store sweeps on its first Allow. Spend it on a third key
	// so the two seeded keys meet the sweep, not the per-key path.
	if _, _, err := s.Allow(ctx, "login_ip|198.51.100.1", cfg); err != nil {
		t.Fatalf("sweeping Allow: %v", err)
	}

	ok, retry, err := s.Allow(ctx, key, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Errorf("a key whose block expired was blocked again (retry %v): the sweep dropped the block but kept the attempts it covered", retry)
	}
	var left int
	if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s_attempts WHERE rl_key = $1", table), other).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 1 {
		t.Errorf("sweep left %d attempts for the other key, want 1 (only the post-expiry attempt)", left)
	}
}

func TestRateLimitScopeCaseParity(t *testing.T) {
	check := func(t *testing.T, db *sql.DB) {
		ctx := context.Background()
		const table = "auth_rate_limits"
		store := NewSQLRateLimitStore(db, table)
		if err := store.EnsureSchema(ctx); err != nil {
			t.Fatal(err)
		}
		scope := "login_é%|"
		keys := []string{
			ratelimit.NamespaceScope(scope) + "|covered",
			ratelimit.NamespaceScope("Login_é%|") + "|covered",
		}
		now := time.Now()
		for _, key := range keys {
			for _, at := range []time.Time{now.Add(-time.Hour), now.Add(-20 * time.Second), now.Add(-time.Second)} {
				if _, err := db.Exec("INSERT INTO auth_rate_limits_attempts (rl_key, attempted_at_ms) VALUES ($1, $2)", key, at.UnixMilli()); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec("INSERT INTO auth_rate_limits (rl_key, blocked_until_ms) VALUES ($1, $2)", key, now.Add(-10*time.Second).UnixMilli()); err != nil {
				t.Fatal(err)
			}
		}
		cfg := RateLimiterConfig{Scope: scope, MaxAttempts: 3, Window: time.Minute, BlockDuration: time.Minute}
		if ok, _, err := store.Allow(ctx, ratelimit.NamespaceScope(scope)+"|sweep", cfg); !ok || err != nil {
			t.Fatalf("sweeping Allow = %v, %v", ok, err)
		}
		for i, key := range keys {
			var attempts, blocks int
			if err := db.QueryRow("SELECT COUNT(*) FROM auth_rate_limits_attempts WHERE rl_key = $1", key).Scan(&attempts); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow("SELECT COUNT(*) FROM auth_rate_limits WHERE rl_key = $1", key).Scan(&blocks); err != nil {
				t.Fatal(err)
			}
			if want := 1 + 2*i; attempts != want || blocks != i {
				t.Errorf("%q kept %d attempts and %d blocks; want %d and %d", key, attempts, blocks, want, i)
			}
		}
	}
	t.Run("sqlite", func(t *testing.T) {
		_, db := newRLStore(t)
		check(t, db)
	})
	t.Run("postgres", func(t *testing.T) { check(t, pgtest.DB(t)) })
}
