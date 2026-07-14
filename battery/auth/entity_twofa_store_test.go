package auth

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

func newTwoFAStore(t *testing.T) *EntityTwoFAStore {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := NewEntityTwoFAStore(db, "auth_twofa")
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return s
}

func hashCode(t *testing.T, code string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	return string(h)
}

func TestEntityTwoFA_RoundTrip(t *testing.T) {
	s := newTwoFAStore(t)
	ctx := context.Background()

	// Not enrolled → nil, nil (matches MemoryTwoFAStore).
	got, err := s.GetTwoFA(ctx, "u1")
	if err != nil || got != nil {
		t.Fatalf("unenrolled: got %v, %v; want nil, nil", got, err)
	}

	state := &TwoFAState{Enabled: true, Secret: "JBSWY3DP", BackupCodes: []string{hashCode(t, "code-1")}, Verified: true}
	if err := s.SetTwoFA(ctx, "u1", state); err != nil {
		t.Fatalf("SetTwoFA: %v", err)
	}
	got, err = s.GetTwoFA(ctx, "u1")
	if err != nil {
		t.Fatalf("GetTwoFA: %v", err)
	}
	if !got.Enabled || !got.Verified || got.Secret != "JBSWY3DP" || len(got.BackupCodes) != 1 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestEntityTwoFA_Upsert(t *testing.T) {
	s := newTwoFAStore(t)
	ctx := context.Background()

	if err := s.SetTwoFA(ctx, "u1", &TwoFAState{Secret: "first"}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := s.SetTwoFA(ctx, "u1", &TwoFAState{Secret: "second", Enabled: true}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := s.GetTwoFA(ctx, "u1")
	if err != nil {
		t.Fatalf("GetTwoFA: %v", err)
	}
	if got.Secret != "second" || !got.Enabled {
		t.Fatalf("upsert did not replace state: %+v", got)
	}
}

func TestEntityTwoFA_Delete(t *testing.T) {
	s := newTwoFAStore(t)
	ctx := context.Background()

	if err := s.SetTwoFA(ctx, "u1", &TwoFAState{Enabled: true}); err != nil {
		t.Fatalf("SetTwoFA: %v", err)
	}
	if err := s.DeleteTwoFA(ctx, "u1"); err != nil {
		t.Fatalf("DeleteTwoFA: %v", err)
	}
	got, err := s.GetTwoFA(ctx, "u1")
	if err != nil || got != nil {
		t.Fatalf("after delete: got %v, %v; want nil, nil", got, err)
	}
	// Deleting an absent row is not an error.
	if err := s.DeleteTwoFA(ctx, "nobody"); err != nil {
		t.Fatalf("delete absent: %v", err)
	}
}

func TestEntityTwoFA_ConsumeBackupCode(t *testing.T) {
	s := newTwoFAStore(t)
	ctx := context.Background()

	state := &TwoFAState{Enabled: true, BackupCodes: []string{hashCode(t, "alpha"), hashCode(t, "beta")}}
	if err := s.SetTwoFA(ctx, "u1", state); err != nil {
		t.Fatalf("SetTwoFA: %v", err)
	}

	// Wrong code → false, nothing consumed.
	ok, err := s.ConsumeBackupCode(ctx, "u1", "wrong")
	if err != nil || ok {
		t.Fatalf("wrong code: got %v, %v; want false, nil", ok, err)
	}

	// Right code → true, and the code is gone.
	ok, err = s.ConsumeBackupCode(ctx, "u1", "alpha")
	if err != nil || !ok {
		t.Fatalf("consume alpha: got %v, %v; want true, nil", ok, err)
	}
	got, err := s.GetTwoFA(ctx, "u1")
	if err != nil || len(got.BackupCodes) != 1 {
		t.Fatalf("after consume: %d codes, err %v; want 1", len(got.BackupCodes), err)
	}

	// Second consume of the same code → false (single-use).
	ok, err = s.ConsumeBackupCode(ctx, "u1", "alpha")
	if err != nil || ok {
		t.Fatalf("double consume: got %v, %v; want false, nil", ok, err)
	}

	// Unenrolled user → false, no error.
	ok, err = s.ConsumeBackupCode(ctx, "nobody", "alpha")
	if err != nil || ok {
		t.Fatalf("unenrolled consume: got %v, %v; want false, nil", ok, err)
	}
}

func TestEntityTwoFA_CorruptCodesRowErrors(t *testing.T) {
	s := newTwoFAStore(t)
	ctx := context.Background()

	if _, err := s.db.ExecContext(ctx,
		"INSERT INTO auth_twofa (user_id, enabled, secret, backup_codes, verified) VALUES ('u1', 1, '', 'not-json', 0)"); err != nil {
		t.Fatalf("seed corrupt row: %v", err)
	}
	if _, err := s.GetTwoFA(ctx, "u1"); err == nil {
		t.Fatal("corrupt backup_codes must surface as an error, not silent state")
	}
}

// Robustness: ConsumeBackupCode's CAS is on the integer version column,
// NOT the backup_codes bytes — so a row stored with semantically-equal but
// non-canonical JSON (pretty-printed, extra whitespace, e.g. from an admin
// edit) is still consumed correctly. (Earlier the byte-CAS wedged such a
// row's consumption closed until a rewrite; the version CAS removes that.)
func TestEntityTwoFA_NonCanonicalJSONStillConsumes(t *testing.T) {
	s := newTwoFAStore(t)
	ctx := context.Background()
	h := hashCode(t, "alpha")

	// Non-canonical: a space after the comma (what a pretty-printer emits).
	// version defaults to 0.
	nonCanonical := fmt.Sprintf("[%q, %q]", h, hashCode(t, "beta"))
	if _, err := s.db.ExecContext(ctx,
		"INSERT INTO auth_twofa (user_id, enabled, secret, backup_codes, verified) VALUES ('u1', 1, '', $1, 0)",
		nonCanonical); err != nil {
		t.Fatalf("seed non-canonical row: %v", err)
	}

	ok, err := s.ConsumeBackupCode(ctx, "u1", "alpha")
	if err != nil {
		t.Fatalf("ConsumeBackupCode: %v", err)
	}
	if !ok {
		t.Fatal("a valid code in a non-canonically-formatted row must still consume (version CAS, not byte CAS)")
	}
	// Single-use still holds.
	if ok, _ := s.ConsumeBackupCode(ctx, "u1", "alpha"); ok {
		t.Fatal("consumed code must not be reusable")
	}
}

// Regression (A-F1): EnsureSchema must self-heal a table created before the
// version column existed — CREATE TABLE IF NOT EXISTS no-ops on it, so the
// column would be missing and every 2FA op would error.
func TestEntityTwoFA_EnsureSchemaAddsVersionToOldTable(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Simulate an old-shape table WITHOUT the version column.
	if _, err := db.Exec("CREATE TABLE auth_twofa (user_id TEXT PRIMARY KEY, enabled INTEGER NOT NULL DEFAULT 0, secret TEXT NOT NULL DEFAULT '', backup_codes TEXT NOT NULL DEFAULT '[]', verified INTEGER NOT NULL DEFAULT 0)"); err != nil {
		t.Fatalf("seed old table: %v", err)
	}

	s := NewEntityTwoFAStore(db, "auth_twofa")
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema must add the missing column: %v", err)
	}
	// The version-CAS path must now work end to end.
	ctx := context.Background()
	if err := s.SetTwoFA(ctx, "u1", &TwoFAState{Enabled: true, BackupCodes: []string{hashCode(t, "alpha")}}); err != nil {
		t.Fatalf("SetTwoFA after self-heal: %v", err)
	}
	if ok, err := s.ConsumeBackupCode(ctx, "u1", "alpha"); err != nil || !ok {
		t.Fatalf("ConsumeBackupCode after self-heal: got %v, %v; want true", ok, err)
	}
}

// TwoFAEntityFields must expose the version column, or a host auto-migrating
// the entity gets a table the store's CAS can't use.
func TestTwoFAEntityFields_IncludesVersion(t *testing.T) {
	found := false
	for _, f := range TwoFAEntityFields() {
		if f.Name == "version" {
			found = true
		}
	}
	if !found {
		t.Fatal("TwoFAEntityFields must include the version column used by ConsumeBackupCode's CAS")
	}
}

func TestEntityTwoFA_RejectsBadTableName(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewEntityTwoFAStore must panic on an unsafe table name")
		}
	}()
	NewEntityTwoFAStore(nil, "auth_twofa; DROP TABLE users --")
}

// TestEntityTwoFA_PluginEnsuresSchema pins the self-migration contract:
// wiring the store into TwoFAPlugin needs no hand-rolled DDL.
func TestEntityTwoFA_PluginEnsuresSchema(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mgr := New(AuthConfig{JWTSecret: "k"})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewTwoFAPlugin(TwoFAConfig{Store: NewEntityTwoFAStore(db, "auth_twofa")}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// The table exists without any host DDL.
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM auth_twofa").Scan(&n); err != nil {
		t.Fatalf("plugin Init did not create the 2FA table: %v", err)
	}
}
