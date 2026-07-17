package auth

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// slowHash hashes at a real bcrypt cost (10) so a CompareHashAndPassword
// against it takes ~60ms — used to widen the read-to-write window in the
// same-code concurrency test so the version CAS is genuinely exercised.
func slowHash(t *testing.T, code string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(code), 10)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	return string(h)
}

// openPGMultiConn returns a Postgres pool whose EVERY connection is pinned
// to a fresh throwaway schema (search_path baked into the DSN options), so
// a genuinely concurrent test can use MaxOpenConns > 1 without racing on
// the per-connection SET search_path that openPGForBattery relies on.
func openPGMultiConn(t *testing.T) *sql.DB {
	t.Helper()
	baseDSN, err := resolveBatteryPG()
	if err != nil {
		t.Skipf("Postgres unavailable: %v", err)
	}
	schema := fmt.Sprintf("battery_auth_mc_%d", time.Now().UnixNano())

	admin, err := sql.Open("postgres", baseDSN)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	// Ping-retry: the container connection can be cold/reset on first touch
	// (same reason openPGForBattery does this before use).
	for i := 0; i < 25; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		err = admin.PingContext(ctx)
		cancel()
		if err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		// resolveBatteryPG already confirmed PG is up, so an unpingable pool
		// now is a real breakage — fail loudly rather than silently skip and
		// quietly drop the CAS coverage from CI (matches openPGForBattery).
		_ = admin.Close()
		t.Fatalf("Postgres was resolved but the pool is unreachable: %v", err)
	}
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		_ = admin.Close()
		t.Fatalf("create schema: %v", err)
	}
	_ = admin.Close()

	u, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	q := u.Query()
	q.Set("options", "-c search_path="+schema)
	u.RawQuery = q.Encode()

	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatalf("open pinned pool: %v", err)
	}
	db.SetMaxOpenConns(8)
	t.Cleanup(func() {
		_, _ = db.Exec("DROP SCHEMA " + schema + " CASCADE")
		_ = db.Close()
	})
	return db
}

// Postgres coverage for EntityTwoFAStore + SQLRateLimitStore. The
// SQLite-only tests ride mattn/go-sqlite3's permissive type handling;
// this file validates the same store code against real Postgres (lib/pq,
// which requires $N placeholders and rejects ?). Skips when Postgres is
// unavailable (same convention as entity_store_pg_test.go). openPGForBattery
// gives a handle with search_path set to a throwaway schema; the stores
// create their own tables via EnsureSchema.

// Regression (round-3 F1/F2): the PG self-heal must add the version column
// to the store's OWN table even when a same-named table in ANOTHER schema
// already has it. The original information_schema COUNT (no schema filter)
// false-positived here and skipped the ALTER, bricking 2FA in
// schema-per-tenant deployments. ADD COLUMN IF NOT EXISTS resolves the
// store's table via search_path, so a sibling schema can't fool it.
func TestEntityTwoFA_Postgres_SelfHealIgnoresOtherSchema(t *testing.T) {
	db := openPGMultiConn(t) // pinned to a private throwaway schema via search_path
	ctx := context.Background()

	// A DECOY versioned table with the same name in a different schema.
	if _, err := db.Exec("CREATE SCHEMA decoy_other"); err != nil {
		t.Fatalf("create decoy schema: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec("DROP SCHEMA decoy_other CASCADE") })
	if _, err := db.Exec("CREATE TABLE decoy_other.auth_twofa (user_id TEXT PRIMARY KEY, version BIGINT NOT NULL DEFAULT 0)"); err != nil {
		t.Fatalf("create decoy table: %v", err)
	}

	// The store's OWN table (in the search_path schema) is the OLD shape,
	// missing the version column.
	if _, err := db.Exec("CREATE TABLE auth_twofa (user_id TEXT PRIMARY KEY, enabled BOOLEAN NOT NULL DEFAULT FALSE, secret TEXT NOT NULL DEFAULT '', backup_codes TEXT NOT NULL DEFAULT '[]', verified BOOLEAN NOT NULL DEFAULT FALSE)"); err != nil {
		t.Fatalf("create old-shape table: %v", err)
	}

	s := NewEntityTwoFAStore(db, "auth_twofa")
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema must add version despite the sibling schema: %v", err)
	}
	// End-to-end: the CAS path must work on the self-healed table.
	if err := s.SetTwoFA(ctx, "u1", &TwoFAState{Enabled: true, BackupCodes: []string{hashCode(t, "alpha")}}); err != nil {
		t.Fatalf("SetTwoFA after self-heal: %v", err)
	}
	if ok, err := s.ConsumeBackupCode(ctx, "u1", "alpha"); err != nil || !ok {
		t.Fatalf("ConsumeBackupCode after self-heal: got %v, %v; want true", ok, err)
	}
}

func TestTwoFAPGLegacyBools(t *testing.T) {
	db := openPGMultiConn(t)
	ctx := context.Background()
	if _, err := db.Exec(`CREATE TABLE auth_twofa (
		user_id TEXT PRIMARY KEY,
		enabled INTEGER NOT NULL DEFAULT 0,
		secret TEXT NOT NULL DEFAULT '',
		backup_codes TEXT NOT NULL DEFAULT '[]',
		verified INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		t.Fatalf("create legacy 2FA table: %v", err)
	}
	store := NewEntityTwoFAStore(db, "auth_twofa")
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema legacy: %v", err)
	}
	var typ string
	if err := db.QueryRow(`SELECT data_type FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'auth_twofa' AND column_name = 'enabled'`).Scan(&typ); err != nil {
		t.Fatalf("enabled type: %v", err)
	}
	if typ != "boolean" {
		t.Fatalf("legacy enabled type = %q, want boolean", typ)
	}
	if err := store.SetTwoFA(ctx, "u1", &TwoFAState{Enabled: true}); err != nil {
		t.Fatalf("SetTwoFA after legacy conversion: %v", err)
	}
}

// EnsureSchema must be idempotent / race-safe on Postgres: calling it twice
// (as concurrent replica boots effectively do) must not error on a
// duplicate ALTER.
func TestEntityTwoFA_Postgres_EnsureSchemaIdempotent(t *testing.T) {
	db := openPGMultiConn(t)
	ctx := context.Background()
	s := NewEntityTwoFAStore(db, "auth_twofa")
	for i := 0; i < 3; i++ {
		if err := s.EnsureSchema(ctx); err != nil {
			t.Fatalf("EnsureSchema call %d must be idempotent: %v", i+1, err)
		}
	}
}

func TestEntityTwoFA_Postgres_RoundTripAndConsume(t *testing.T) {
	db := openPGForBattery(t)
	ctx := context.Background()
	s := NewEntityTwoFAStore(db, "auth_twofa")
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// Unenrolled → nil, nil.
	if got, err := s.GetTwoFA(ctx, "u1"); err != nil || got != nil {
		t.Fatalf("unenrolled: got %v, %v; want nil, nil", got, err)
	}

	state := &TwoFAState{Enabled: true, Secret: "JBSWY3DP", BackupCodes: []string{hashCode(t, "alpha"), hashCode(t, "beta")}, Verified: true}
	if err := s.SetTwoFA(ctx, "u1", state); err != nil {
		t.Fatalf("SetTwoFA: %v", err)
	}
	got, err := s.GetTwoFA(ctx, "u1")
	if err != nil {
		t.Fatalf("GetTwoFA: %v", err)
	}
	if !got.Enabled || !got.Verified || got.Secret != "JBSWY3DP" || len(got.BackupCodes) != 2 {
		t.Fatalf("round-trip mismatch on PG: %+v", got)
	}

	// Upsert replaces (ON CONFLICT ... excluded.* on real PG).
	if err := s.SetTwoFA(ctx, "u1", &TwoFAState{Secret: "second", Enabled: true}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if got, _ := s.GetTwoFA(ctx, "u1"); got.Secret != "second" {
		t.Fatalf("upsert did not replace on PG: %+v", got)
	}

	// Re-seed and consume a backup code (CAS on PG).
	if err := s.SetTwoFA(ctx, "u1", state); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	ok, err := s.ConsumeBackupCode(ctx, "u1", "alpha")
	if err != nil || !ok {
		t.Fatalf("consume alpha on PG: got %v, %v; want true", ok, err)
	}
	ok, err = s.ConsumeBackupCode(ctx, "u1", "alpha")
	if err != nil || ok {
		t.Fatalf("double-consume on PG: got %v, %v; want false (single-use)", ok, err)
	}

	if err := s.DeleteTwoFA(ctx, "u1"); err != nil {
		t.Fatalf("DeleteTwoFA: %v", err)
	}
	if got, _ := s.GetTwoFA(ctx, "u1"); got != nil {
		t.Fatalf("after delete on PG: %+v; want nil", got)
	}
}

// The CAS single-use guarantee under concurrency, on real Postgres:
// N goroutines racing to consume the SAME code must yield exactly one
// success and zero corruption of the remaining codes.
func TestEntityTwoFA_Postgres_ConcurrentConsumeSingleUse(t *testing.T) {
	// A multi-connection pool (openPGForBattery pins search_path to only one
	// connection, so it can't drive true cross-connection contention).
	db := openPGMultiConn(t)
	ctx := context.Background()
	s := NewEntityTwoFAStore(db, "auth_twofa")
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	// The seeded hashes use a real bcrypt cost (10, ~60ms/compare) and the
	// shared target sits behind two pads, so each racer spends ~180ms in the
	// bcrypt scan between its SELECT and its UPDATE. That holds the
	// read-to-write window open long enough for all racers to read the SAME
	// version before any UPDATE commits — forcing the version CAS to
	// actually arbitrate. (With MinCost hashes and the target at index 0 the
	// scan returns instantly and the goroutines serialize, making the test a
	// guard that stays green even with the CAS removed.)
	codes := []string{
		slowHash(t, "pad-0"),
		slowHash(t, "pad-1"),
		slowHash(t, "shared"), // index 2
		slowHash(t, "other"),  // index 3
	}
	if err := s.SetTwoFA(ctx, "u1", &TwoFAState{Enabled: true, BackupCodes: codes}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const racers = 8
	results := make(chan bool, racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		go func() {
			<-start
			ok, err := s.ConsumeBackupCode(ctx, "u1", "shared")
			if err != nil {
				t.Errorf("ConsumeBackupCode: %v", err)
			}
			results <- ok
		}()
	}
	close(start)
	wins := 0
	for i := 0; i < racers; i++ {
		if <-results {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("concurrent same-code consume: %d wins, want exactly 1 (double-spend or lost update)", wins)
	}
	// The untouched codes must survive; the shared code must be gone.
	got, err := s.GetTwoFA(ctx, "u1")
	if err != nil {
		t.Fatalf("GetTwoFA after race: %v", err)
	}
	if len(got.BackupCodes) != len(codes)-1 {
		t.Fatalf("after consuming 1 of %d codes: %d remain, want %d", len(codes), len(got.BackupCodes), len(codes)-1)
	}
	if ok, _ := s.ConsumeBackupCode(ctx, "u1", "shared"); ok {
		t.Fatal("shared code should be spent")
	}
	if ok, _ := s.ConsumeBackupCode(ctx, "u1", "other"); !ok {
		t.Fatal("untouched code should still be consumable")
	}
}

// Regression for the CAS retry bound: N DISTINCT valid codes consumed
// concurrently must ALL succeed. Under the old fixed 2-retry bound, a code
// could be wrongly rejected when >2 other codes were consumed between its
// retries (reviewer repro'd it on both engines). The bound is now
// len(codes)+2, so every valid code either wins or is proven gone.
func TestEntityTwoFA_Postgres_ConcurrentDistinctCodesAllSucceed(t *testing.T) {
	db := openPGMultiConn(t)
	ctx := context.Background()
	s := NewEntityTwoFAStore(db, "auth_twofa")
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	const n = 8
	codes := make([]string, n)
	hashes := make([]string, n)
	for i := range codes {
		codes[i] = fmt.Sprintf("code-%d", i)
		hashes[i] = hashCode(t, codes[i])
	}
	if err := s.SetTwoFA(ctx, "u1", &TwoFAState{Enabled: true, BackupCodes: hashes}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	results := make(chan bool, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		go func(code string) {
			<-start
			ok, err := s.ConsumeBackupCode(ctx, "u1", code)
			if err != nil {
				t.Errorf("ConsumeBackupCode(%s): %v", code, err)
			}
			results <- ok
		}(codes[i])
	}
	close(start)
	got := 0
	for i := 0; i < n; i++ {
		if <-results {
			got++
		}
	}
	if got != n {
		t.Fatalf("all %d distinct valid codes must consume; only %d did (retry bound too low)", n, got)
	}
	if st, _ := s.GetTwoFA(ctx, "u1"); len(st.BackupCodes) != 0 {
		t.Fatalf("all codes should be spent; %d remain", len(st.BackupCodes))
	}
}

func TestSQLRateLimit_Postgres_BlockAndShare(t *testing.T) {
	db := openPGForBattery(t)
	ctx := context.Background()
	s := NewSQLRateLimitStore(db, "auth_rate_limits")
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	cfg := rlTestConfig()

	for i := 0; i < cfg.MaxAttempts; i++ {
		if ok, _, err := s.Allow(ctx, "login_ip|1.2.3.4", cfg); err != nil || !ok {
			t.Fatalf("attempt %d on PG: got %v, %v; want allowed", i+1, ok, err)
		}
	}
	if ok, retry, err := s.Allow(ctx, "login_ip|1.2.3.4", cfg); err != nil || ok || retry <= 0 {
		t.Fatalf("over-budget on PG: got ok=%v retry=%v err=%v; want blocked", ok, retry, err)
	}
	// A different scope for the same raw key is independent.
	if ok, _, err := s.Allow(ctx, "twofa|1.2.3.4", cfg); err != nil || !ok {
		t.Fatalf("distinct scope on PG must be unaffected: got %v, %v", ok, err)
	}
}
