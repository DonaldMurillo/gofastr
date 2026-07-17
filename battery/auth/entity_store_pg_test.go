package auth

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Cross-dialect smoke for EntityUserStore + EntitySessionStore. The
// SQLite-only tests in entity_store_test.go ride the mattn/go-sqlite3
// driver's permissive type handling; this file validates the same
// store code against a real Postgres backend (lib/pq, which does NOT
// accept ? placeholders and requires explicit time.Time bindings).
//
// Postgres comes from $TEST_POSTGRES_DSN if set; otherwise an ephemeral
// testcontainer. If neither is reachable the subtest skips — it does
// not fail (same convention as framework/auth/dialect_test.go).

var (
	batteryPGOnce    sync.Once
	batteryPGBaseDSN string
	batteryPGErr     error
	batteryPGUsing   string
	batteryPGLogged  atomic.Bool
	batteryPGKeepRef *tcpostgres.PostgresContainer
)

func resolveBatteryPG() (string, error) {
	batteryPGOnce.Do(func() {
		if dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN")); dsn != "" {
			batteryPGBaseDSN = dsn
			batteryPGUsing = "env"
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		c, err := tcpostgres.Run(ctx, "postgres:16-alpine",
			tcpostgres.WithDatabase("auth_test"),
			tcpostgres.WithUsername("test"),
			tcpostgres.WithPassword("test"),
		)
		if err != nil {
			batteryPGErr = fmt.Errorf("testcontainers: %w", err)
			return
		}
		dsn, err := c.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			batteryPGErr = err
			return
		}
		batteryPGBaseDSN = dsn
		batteryPGUsing = "container"
		batteryPGKeepRef = c
	})
	return batteryPGBaseDSN, batteryPGErr
}

func openPGForBattery(t *testing.T) *sql.DB {
	t.Helper()
	dsn, err := resolveBatteryPG()
	if err != nil {
		t.Skipf("Postgres unavailable: %v", err)
	}
	if !batteryPGLogged.Swap(true) {
		t.Logf("battery/auth Postgres tests using %s", batteryPGUsing)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open pg: %v", err)
	}
	db.SetMaxOpenConns(1)
	for i := 0; i < 25; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		if err := db.PingContext(ctx); err == nil {
			cancel()
			break
		}
		cancel()
		time.Sleep(200 * time.Millisecond)
	}
	schema := fmt.Sprintf("battery_auth_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := db.Exec("SET search_path TO " + schema); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE users (
		id TEXT PRIMARY KEY,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		roles TEXT DEFAULT '["user"]',
		password_set BOOLEAN NOT NULL DEFAULT FALSE
	)`); err != nil {
		t.Fatalf("create users: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE sessions (
		id TEXT PRIMARY KEY,
		token TEXT NOT NULL UNIQUE,
		user_id TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL,
		expires_at TIMESTAMPTZ NOT NULL,
		two_factor_verified BOOLEAN NOT NULL DEFAULT FALSE,
		pending_two_factor BOOLEAN NOT NULL DEFAULT FALSE
	)`); err != nil {
		t.Fatalf("create sessions: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DROP SCHEMA " + schema + " CASCADE")
		db.Close()
	})
	return db
}

func TestEntityUserStore_Postgres_RoundTrip(t *testing.T) {
	db := openPGForBattery(t)
	store := NewEntityUserStore(db, "users")
	ctx := context.Background()

	hash, err := HashPassword("secret123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	u, err := store.CreateUser(ctx, "alice@pg.test", hash, []string{"admin"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.GetID() == "" {
		t.Fatal("empty ID")
	}

	got, gotHash, err := store.FindByEmail(ctx, "alice@pg.test")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if got.GetID() != u.GetID() {
		t.Fatalf("id mismatch: %s vs %s", got.GetID(), u.GetID())
	}
	if !CheckPassword("secret123", gotHash) {
		t.Fatal("hash mismatch")
	}

	if _, err := store.CreateUser(ctx, "alice@pg.test", hash, []string{"user"}); err == nil {
		t.Fatal("expected ErrEmailTaken on duplicate insert")
	} else if err != ErrEmailTaken {
		t.Fatalf("expected ErrEmailTaken sentinel, got %v", err)
	}

	if _, _, err := store.FindByEmail(ctx, "nobody@pg.test"); err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

// TestEntitySessionStore_Postgres_TwoFARoundTrip is the gap this task
// closes: MarkPendingTwoFactor → Get → MarkTwoFactorVerified → Get must
// round-trip the booleans correctly against real Postgres, where lib/pq
// rejects ? placeholders and TIMESTAMPTZ values come back as time.Time
// (not the SQLite TEXT format).
func TestEntitySessionStore_Postgres_TwoFARoundTrip(t *testing.T) {
	db := openPGForBattery(t)
	store := NewEntitySessionStore(db, "sessions")
	ctx := context.Background()

	sess, err := store.Create(ctx, "user-pg", time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sess.Token == "" {
		t.Fatal("empty token")
	}

	loaded, err := store.Get(ctx, sess.Token)
	if err != nil {
		t.Fatalf("Get after Create: %v", err)
	}
	if loaded.TwoFactorVerified || loaded.PendingTwoFactor {
		t.Fatalf("fresh session must start with both 2FA flags false; got verified=%v pending=%v",
			loaded.TwoFactorVerified, loaded.PendingTwoFactor)
	}
	if loaded.CreatedAt.IsZero() || loaded.ExpiresAt.IsZero() {
		t.Fatalf("timestamps lost on round-trip: created=%v expires=%v", loaded.CreatedAt, loaded.ExpiresAt)
	}

	if err := store.MarkPendingTwoFactor(ctx, sess.Token); err != nil {
		t.Fatalf("MarkPendingTwoFactor: %v", err)
	}
	pending, err := store.Get(ctx, sess.Token)
	if err != nil {
		t.Fatalf("Get after MarkPendingTwoFactor: %v", err)
	}
	if !pending.PendingTwoFactor {
		t.Fatal("expected PendingTwoFactor=true after MarkPendingTwoFactor")
	}
	if pending.TwoFactorVerified {
		t.Fatal("MarkPendingTwoFactor must not set TwoFactorVerified")
	}

	if err := store.MarkTwoFactorVerified(ctx, sess.Token); err != nil {
		t.Fatalf("MarkTwoFactorVerified: %v", err)
	}
	verified, err := store.Get(ctx, sess.Token)
	if err != nil {
		t.Fatalf("Get after MarkTwoFactorVerified: %v", err)
	}
	if !verified.TwoFactorVerified {
		t.Fatal("expected TwoFactorVerified=true after MarkTwoFactorVerified")
	}
	if verified.PendingTwoFactor {
		t.Fatal("MarkTwoFactorVerified must clear PendingTwoFactor")
	}

	if err := store.Delete(ctx, sess.Token); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, sess.Token); err != ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound after Delete, got %v", err)
	}
}

// TestEntitySessionStore_Postgres_ExpiryAndCleanup exercises the expired-
// row path on PG, where time comparisons use TIMESTAMPTZ semantics rather
// than SQLite's permissive string compare. The Cleanup query binds
// time.Now() directly — this verifies lib/pq accepts the time.Time bind
// and the DELETE drops only the expired row.
func TestEntitySessionStore_Postgres_ExpiryAndCleanup(t *testing.T) {
	db := openPGForBattery(t)
	store := NewEntitySessionStore(db, "sessions")
	ctx := context.Background()

	tok, err := newSessionToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	now := time.Now().UTC()
	expired := now.Add(-time.Hour)
	// Mirror EntitySessionStore.Create — Postgres requires the id PK to
	// be supplied since AutoUUID columns are NOT NULL with no DEFAULT.
	q := store.qTable("INSERT INTO %s (id, token, user_id, created_at, expires_at) VALUES ($1, $2, $3, $4, $5)")
	if _, err := db.ExecContext(ctx, q, generateUserID(), tok, "user-expired", now, expired); err != nil {
		t.Fatalf("insert expired: %v", err)
	}

	if _, err := store.Get(ctx, tok); err != ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound for expired session, got %v", err)
	}

	fresh, err := store.Create(ctx, "user-fresh", time.Hour)
	if err != nil {
		t.Fatalf("Create fresh: %v", err)
	}

	n, err := store.Cleanup(ctx)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 expired session cleaned, got %d", n)
	}

	if _, err := store.Get(ctx, fresh.Token); err != nil {
		t.Fatalf("fresh session must remain after cleanup: %v", err)
	}
}

// TestUserStorePGFreshSchema reproduces the fresh-Postgres first-boot
// regression: EnsureSchema must create a boolean-compatible password flag.
func TestUserStorePGFreshSchema(t *testing.T) {
	db := openPGForBattery(t)
	ctx := context.Background()
	if _, err := db.Exec(`DROP TABLE users`); err != nil {
		t.Fatalf("drop users: %v", err)
	}
	store := NewEntityUserStore(db, "users")
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	hash, err := HashPassword("secret123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := store.CreateUser(ctx, "fresh@pg.test", hash, []string{"admin"}); err != nil {
		t.Fatalf("CreateUser after fresh schema: %v", err)
	}
}

func TestUserStorePGLegacyBool(t *testing.T) {
	db := openPGForBattery(t)
	ctx := context.Background()
	if _, err := db.Exec(`DROP TABLE users`); err != nil {
		t.Fatalf("drop users: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE users (
		id TEXT PRIMARY KEY,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		roles TEXT NOT NULL DEFAULT '["user"]',
		password_set INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		t.Fatalf("create legacy users: %v", err)
	}
	store := NewEntityUserStore(db, "users")
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema legacy: %v", err)
	}
	var typ string
	if err := db.QueryRow(`SELECT data_type FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'users' AND column_name = 'password_set'`).Scan(&typ); err != nil {
		t.Fatalf("password_set type: %v", err)
	}
	if typ != "boolean" {
		t.Fatalf("legacy password_set type = %q, want boolean", typ)
	}
	hash, err := HashPassword("secret123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := store.CreateUser(ctx, "legacy@pg.test", hash, []string{"user"}); err != nil {
		t.Fatalf("CreateUser after legacy conversion: %v", err)
	}
}

func TestSessionStorePGLegacyBools(t *testing.T) {
	db := openPGForBattery(t)
	ctx := context.Background()
	if _, err := db.Exec(`DROP TABLE sessions`); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE sessions (
		id TEXT NOT NULL,
		token TEXT UNIQUE NOT NULL,
		user_id TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL,
		expires_at TIMESTAMP NOT NULL,
		two_factor_verified INTEGER NOT NULL DEFAULT 0,
		pending_two_factor INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		t.Fatalf("create legacy sessions: %v", err)
	}
	store := NewEntitySessionStore(db, "sessions")
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema legacy: %v", err)
	}
	var typ string
	if err := db.QueryRow(`SELECT data_type FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'sessions' AND column_name = 'pending_two_factor'`).Scan(&typ); err != nil {
		t.Fatalf("pending_two_factor type: %v", err)
	}
	if typ != "boolean" {
		t.Fatalf("legacy pending_two_factor type = %q, want boolean", typ)
	}
	sess, err := store.Create(ctx, "legacy-user", time.Hour)
	if err != nil {
		t.Fatalf("Create after legacy conversion: %v", err)
	}
	if err := store.MarkPendingTwoFactor(ctx, sess.Token); err != nil {
		t.Fatalf("MarkPendingTwoFactor after conversion: %v", err)
	}
}
