package auth

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func newRLStore(t *testing.T) (*SQLRateLimitStore, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := NewSQLRateLimitStore(db, "auth_rate_limits")
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return s, db
}

func rlTestConfig() RateLimiterConfig {
	return RateLimiterConfig{MaxAttempts: 3, Window: time.Hour, BlockDuration: time.Hour}
}

func TestSQLRateLimit_BlocksAtMax(t *testing.T) {
	s, _ := newRLStore(t)
	ctx := context.Background()
	cfg := rlTestConfig()

	for i := 0; i < 3; i++ {
		ok, _, err := s.Allow(ctx, "ip:1.2.3.4", cfg)
		if err != nil || !ok {
			t.Fatalf("attempt %d: got %v, %v; want allowed", i+1, ok, err)
		}
	}
	ok, retry, err := s.Allow(ctx, "ip:1.2.3.4", cfg)
	if err != nil || ok {
		t.Fatalf("4th attempt: got %v, %v; want blocked", ok, err)
	}
	if retry != cfg.BlockDuration {
		t.Errorf("retryAfter: got %v, want %v", retry, cfg.BlockDuration)
	}
	// The block holds on subsequent calls.
	ok, retry, err = s.Allow(ctx, "ip:1.2.3.4", cfg)
	if err != nil || ok || retry <= 0 {
		t.Fatalf("blocked follow-up: got %v, %v, %v", ok, retry, err)
	}
}

func TestSQLRateLimit_KeysIndependent(t *testing.T) {
	s, _ := newRLStore(t)
	ctx := context.Background()
	cfg := rlTestConfig()

	for i := 0; i < 4; i++ {
		s.Allow(ctx, "ip:1.1.1.1", cfg) // drive to blocked
	}
	ok, _, err := s.Allow(ctx, "ip:2.2.2.2", cfg)
	if err != nil || !ok {
		t.Fatalf("other key must be unaffected: got %v, %v", ok, err)
	}
}

// The load-bearing multi-replica property: two limiter instances
// (simulating two replicas) sharing one store consume ONE budget, and a
// block established via one is honoured by the other.
func TestSQLRateLimit_SharedAcrossLimiters(t *testing.T) {
	s, _ := newRLStore(t)
	cfg := RateLimiterConfig{MaxAttempts: 3, Window: time.Hour, BlockDuration: time.Hour, Store: s, Scope: "login_ip"}
	replicaA := NewRateLimiter(cfg)
	replicaB := NewRateLimiter(cfg)

	if ok, _ := replicaA.Allow("9.9.9.9"); !ok {
		t.Fatal("A attempt 1 should pass")
	}
	if ok, _ := replicaB.Allow("9.9.9.9"); !ok {
		t.Fatal("B attempt 2 should pass")
	}
	if ok, _ := replicaA.Allow("9.9.9.9"); !ok {
		t.Fatal("A attempt 3 should pass")
	}
	// Budget (3) is spent ACROSS replicas — the 4th attempt blocks…
	if ok, _ := replicaB.Allow("9.9.9.9"); ok {
		t.Fatal("attempt 4 via B must block: budget is shared, not per-replica")
	}
	// …and the block holds on the replica that never saw the overflow.
	if ok, retry := replicaA.Allow("9.9.9.9"); ok || retry <= 0 {
		t.Fatalf("block must propagate to A: got ok=%v retry=%v", ok, retry)
	}
}

// Distinct scopes namespace the same raw key inside one shared store.
func TestSQLRateLimit_ScopesIsolated(t *testing.T) {
	s, _ := newRLStore(t)
	base := RateLimiterConfig{MaxAttempts: 2, Window: time.Hour, BlockDuration: time.Hour, Store: s}
	login := newScopedRateLimiter(base, "login_ip")
	twofa := newScopedRateLimiter(base, "twofa")

	login.Allow("1.2.3.4")
	login.Allow("1.2.3.4")
	if ok, _ := login.Allow("1.2.3.4"); ok {
		t.Fatal("login budget should be exhausted")
	}
	if ok, _ := twofa.Allow("1.2.3.4"); !ok {
		t.Fatal("2FA budget for the same IP must be independent of login's")
	}
}

// Window expiry: attempts older than the window stop counting. Pinned
// deterministically by backdating rows instead of sleeping.
func TestSQLRateLimit_WindowExpiry(t *testing.T) {
	s, db := newRLStore(t)
	ctx := context.Background()
	cfg := rlTestConfig()

	old := time.Now().Add(-2 * cfg.Window).UnixMilli()
	for i := 0; i < 3; i++ {
		if _, err := db.Exec("INSERT INTO auth_rate_limits_attempts (rl_key, attempted_at_ms) VALUES ('ip:5.5.5.5', $1)", old); err != nil {
			t.Fatalf("backdate attempt: %v", err)
		}
	}
	ok, _, err := s.Allow(ctx, "ip:5.5.5.5", cfg)
	if err != nil || !ok {
		t.Fatalf("out-of-window attempts must not count: got %v, %v", ok, err)
	}
}

// An expired block clears and the key gets a fresh budget.
func TestSQLRateLimit_BlockExpiry(t *testing.T) {
	s, db := newRLStore(t)
	ctx := context.Background()
	cfg := rlTestConfig()

	past := time.Now().Add(-time.Minute).UnixMilli()
	if _, err := db.Exec("INSERT INTO auth_rate_limits (rl_key, blocked_until_ms) VALUES ('ip:6.6.6.6', $1)", past); err != nil {
		t.Fatalf("seed expired block: %v", err)
	}
	ok, _, err := s.Allow(ctx, "ip:6.6.6.6", cfg)
	if err != nil || !ok {
		t.Fatalf("expired block must clear: got %v, %v", ok, err)
	}
}

// A store failure DENIES: an attacker must not lift the brute-force
// limit by degrading the limiter's backend.
func TestSQLRateLimit_StoreErrorFailsClosed(t *testing.T) {
	s, db := newRLStore(t)
	// Prime the lazy schema init BEFORE breaking the DB, then break it.
	if _, _, err := s.Allow(context.Background(), "ip:7.7.7.7", rlTestConfig()); err != nil {
		t.Fatalf("prime: %v", err)
	}
	_ = db.Close()

	rl := NewRateLimiter(RateLimiterConfig{MaxAttempts: 3, Window: time.Hour, BlockDuration: time.Hour, Store: s, Scope: "login_ip"})
	ok, retry := rl.Allow("7.7.7.7")
	if ok {
		t.Fatal("store error must fail closed (deny)")
	}
	if retry <= 0 {
		t.Fatalf("fail-closed denial should carry a Retry-After hint; got %v", retry)
	}
}

// Regression: a transient schema-init failure must NOT permanently brick
// the limiter. The old sync.Once latched the error forever (fleet-wide
// login DoS surviving DB recovery); ensureSchemaOnce retries until it
// first succeeds.
func TestSQLRateLimit_RecoversAfterTransientSchemaError(t *testing.T) {
	ctx := context.Background()
	cfg := rlTestConfig()

	deadDB, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = deadDB.Close() // EnsureSchema will now error on this handle.

	s := NewSQLRateLimitStore(deadDB, "auth_rate_limits")
	if ok, _, err := s.Allow(ctx, "login_ip|1.2.3.4", cfg); err == nil || ok {
		t.Fatalf("first Allow on a dead DB must fail closed: ok=%v err=%v", ok, err)
	}

	// DB recovers (swap in a live handle — single-goroutine test).
	liveDB, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open live: %v", err)
	}
	t.Cleanup(func() { _ = liveDB.Close() })
	s.db = liveDB

	if ok, _, err := s.Allow(ctx, "login_ip|1.2.3.4", cfg); err != nil || !ok {
		t.Fatalf("after DB recovery, Allow must re-init the schema and succeed: ok=%v err=%v", ok, err)
	}
}

// Regression: the amortized sweep is table-global but must use each
// limiter's OWN Window. A short-window co-tenant (e.g. a 1m 2FA limiter)
// must not delete a long-window limiter's (1h login-account) still-valid
// attempt rows — which would silently collapse the longer window.
func TestSQLRateLimit_SweepScopedNotCrossWindow(t *testing.T) {
	s, db := newRLStore(t)
	ctx := context.Background()

	// A login-account attempt 30 min old — inside its 1h window, must survive.
	thirtyMinAgo := time.Now().Add(-30 * time.Minute).UnixMilli()
	if _, err := db.Exec("INSERT INTO auth_rate_limits_attempts (rl_key, attempted_at_ms) VALUES ('login_account|bob', $1)", thirtyMinAgo); err != nil {
		t.Fatalf("seed long-window attempt: %v", err)
	}

	// A fresh store's lastSweep is zero, so this first Allow sweeps — but
	// with the SHORT-window 2FA limiter's config (Window=1m). Pre-fix, the
	// unscoped sweep would delete bob's 30-min-old row.
	shortCfg := RateLimiterConfig{MaxAttempts: 3, Window: time.Minute, BlockDuration: time.Hour, Scope: "twofa"}
	if _, _, err := s.Allow(ctx, "twofa|attacker", shortCfg); err != nil {
		t.Fatalf("2FA Allow: %v", err)
	}

	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM auth_rate_limits_attempts WHERE rl_key = 'login_account|bob'").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("short-window (1m) sweep deleted a long-window (1h) co-tenant's still-valid attempt: %d remain, want 1", n)
	}
}

// A raw key containing the "|" delimiter must NOT let an attacker forge or
// collide another scope's namespace. The composite key is
// trusted-constant-scope + "|" + rawkey, so a pipe in the raw key only
// extends the suffix within its OWN scope.
// Regression: each scope owns its own sweep timer. A busy scope must not
// monopolize a single shared timer and starve another scope's GC, letting
// that scope's abandoned attacker-minted keys accumulate forever.
func TestSQLRateLimit_SweepNotStarvedByBusyScope(t *testing.T) {
	s, db := newRLStore(t)
	ctx := context.Background()

	// 50 abandoned password_reset attempts, all far out of window.
	old := time.Now().Add(-2 * time.Hour).UnixMilli()
	for i := 0; i < 50; i++ {
		if _, err := db.Exec("INSERT INTO auth_rate_limits_attempts (rl_key, attempted_at_ms) VALUES ($1, $2)", fmt.Sprintf("password_reset|bot-%d", i), old); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	cfg := func(scope string) RateLimiterConfig {
		return RateLimiterConfig{MaxAttempts: 100, Window: time.Minute, BlockDuration: time.Hour, Scope: scope}
	}
	// A busy scope takes the "first" sweep. With a single shared timer this
	// would set lastSweep=now for everyone, so password_reset's own sweep
	// never fires and its 50 stale rows survive.
	if _, _, err := s.Allow(ctx, "login_ip|x", cfg("login_ip")); err != nil {
		t.Fatalf("login_ip Allow: %v", err)
	}
	if _, _, err := s.Allow(ctx, "password_reset|user", cfg("password_reset")); err != nil {
		t.Fatalf("password_reset Allow: %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM auth_rate_limits_attempts WHERE rl_key LIKE 'password_reset|%'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	// Only the just-inserted "password_reset|user" attempt should remain.
	if n != 1 {
		t.Fatalf("busy scope starved password_reset GC: %d rows remain, want 1 (the 50 stale bot rows should be swept)", n)
	}
}

// Regression: LIKE metacharacters in a (host-settable) scope name must be
// escaped, or a short-window scope's sweep over-matches into a sibling
// scope and deletes its still-valid rows.
func TestSQLRateLimit_SweepEscapesScopeMetacharacters(t *testing.T) {
	s, db := newRLStore(t)
	ctx := context.Background()

	// Long-window scope "abc" has a still-valid attempt 30 min old.
	thirtyMinAgo := time.Now().Add(-30 * time.Minute).UnixMilli()
	if _, err := db.Exec("INSERT INTO auth_rate_limits_attempts (rl_key, attempted_at_ms) VALUES ('abc|victim', $1)", thirtyMinAgo); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Short-window scope "a_c" sweeps (cutoff now-1m). Unescaped, "a_c|%"
	// treats "_" as a wildcard and matches "abc|victim" — wrongly deleting a
	// row that belongs to the unrelated 1h "abc" scope.
	shortCfg := RateLimiterConfig{MaxAttempts: 3, Window: time.Minute, BlockDuration: time.Hour, Scope: "a_c"}
	if _, _, err := s.Allow(ctx, "a_c|attacker", shortCfg); err != nil {
		t.Fatalf("a_c Allow: %v", err)
	}

	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM auth_rate_limits_attempts WHERE rl_key = 'abc|victim'").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("unescaped LIKE metacharacter in scope deleted a sibling scope's row: %d remain, want 1", n)
	}
}

func TestSQLRateLimit_PipeInKeyCannotCrossScope(t *testing.T) {
	s, _ := newRLStore(t)
	cfg := RateLimiterConfig{MaxAttempts: 2, Window: time.Hour, BlockDuration: time.Hour, Store: s}
	login := newScopedRateLimiter(cfg, "login_ip")
	twofa := newScopedRateLimiter(cfg, "twofa")

	// Attacker hammers login with a raw key crafted to look like a twofa key.
	// Composite becomes "login_ip|twofa|victim" — still under login_ip.
	login.Allow("twofa|victim")
	login.Allow("twofa|victim")
	if ok, _ := login.Allow("twofa|victim"); ok {
		t.Fatal("login budget should be exhausted by the malicious key")
	}
	// The genuine twofa:victim budget must be completely independent.
	if ok, _ := twofa.Allow("victim"); !ok {
		t.Fatal("2FA budget for victim must not be touched by the login-scope attack")
	}
	if ok, _ := twofa.Allow("victim"); !ok {
		t.Fatal("2FA budget for victim should still have room")
	}
}

func TestSQLRateLimit_RejectsBadTableName(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewSQLRateLimitStore must panic on an unsafe table name")
		}
	}()
	NewSQLRateLimitStore(nil, "limits; DROP TABLE users --")
}
