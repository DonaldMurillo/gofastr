package featureflag

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// errOnNthGet wraps a Store and returns an error on the Nth Get call,
// passing all other calls through to the inner store. This reproduces a
// transient store error (connection blip, pool exhaustion, lock timeout)
// that lands on a specific fetch in flight.
type errOnNthGet struct {
	inner Store
	fail  int // 1-based index of the Get call that should error
	calls int
}

func (s *errOnNthGet) Get(ctx context.Context, key string) (*Flag, error) {
	s.calls++
	if s.calls == s.fail {
		return nil, errors.New("featureflag: transient store error")
	}
	return s.inner.Get(ctx, key)
}

// TestBoolDefaultFailsClosed asserts that a kill switch wired with a
// safe-on fallback never fails open: a transient store error on ANY
// fetch must yield the supplied fallback, not the unsafe evaluated value.
func TestBoolDefaultFailsClosed(t *testing.T) {
	mem := NewMemoryStore()
	// A defined, enabled kill switch: when present and on, the protected
	// path is blocked. fallback=true means "block when we can't tell".
	if err := mem.Set(Flag{Key: "kill-payments", Enabled: true, Rollout: 100}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	t.Run("happy path returns evaluated value", func(t *testing.T) {
		e := NewEvaluator(mem)
		if !e.BoolDefault(context.Background(), "kill-payments", true) {
			t.Fatal("enabled flag should evaluate true")
		}
	})

	t.Run("error on first fetch returns fallback", func(t *testing.T) {
		e := NewEvaluator(&errOnNthGet{inner: mem, fail: 1})
		if !e.BoolDefault(context.Background(), "kill-payments", true) {
			t.Fatal("store error must yield fallback=true, not fail open")
		}
	})

	t.Run("error on second fetch returns fallback", func(t *testing.T) {
		// This is the TOCTOU double-read: BoolDefault's own Get succeeds,
		// but the re-fetch inside Bool errors. Must still fail closed.
		e := NewEvaluator(&errOnNthGet{inner: mem, fail: 2})
		if !e.BoolDefault(context.Background(), "kill-payments", true) {
			t.Fatal("error on second fetch must yield fallback=true, not false")
		}
	})

	t.Run("absent flag returns fallback", func(t *testing.T) {
		e := NewEvaluator(NewMemoryStore())
		if !e.BoolDefault(context.Background(), "kill-payments", true) {
			t.Fatal("absent flag must yield fallback=true")
		}
	})
}

// TestBoolStoreErrorFailsClosed asserts the non-default entry point's half
// of the fail-closed contract: Evaluator.Bool (used by every code path
// WITHOUT a safe fallback) must answer false on a store error, a nil store,
// and a nil evaluator, never "flag probably on".
func TestBoolStoreErrorFailsClosed(t *testing.T) {
	mem := NewMemoryStore()
	if err := mem.Set(Flag{Key: "danger", Enabled: true, Rollout: 100}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctx := context.Background()

	if e := NewEvaluator(&errOnNthGet{inner: mem, fail: 1}); e.Bool(ctx, "danger") {
		t.Fatal("SECURITY: [featureflag] Bool returned true on a store error; must fail closed (false). " +
			"Attack: a flaky DB flips a protected path on for every request during the outage.")
	}
	if e := NewEvaluator(nil); e.Bool(ctx, "danger") {
		t.Fatal("nil store must evaluate false, not true")
	}
	var e *Evaluator
	if e.Bool(ctx, "danger") {
		t.Fatal("nil evaluator must evaluate false, not true")
	}
}

// TestSQLTableNameGuardShapes pins the reserved-word / shape guard on
// SQLStore table names across every rejection class it exists for, and —
// the ordering that matters — that rejection happens at construction,
// BEFORE any statement runs against the database.
//
// Layer note: the table name is developer-supplied configuration, not
// request-borne input; safeIdent is a misconfiguration guard (a table named
// "users" or "migrations" silently no-ops CREATE TABLE IF NOT EXISTS
// against a real table and then reads/writes it). These shapes pin the
// documented guard, they do not model an attacker.
func TestSQLTableNameGuardShapes(t *testing.T) {
	rejected := []string{
		"users",                 // reserved: real system table
		"USER",                  // reserved, case-insensitive
		"Migrations",            // reserved, mixed case
		"sessions",              // reserved: real system table
		"select",                // reserved: SQL keyword
		"table",                 // reserved: SQL keyword
		"1flags",                // leading digit
		"flag-table",            // punctuation
		`fl"ags`,                // quote smuggle
		"flags;drop",            // statement smuggle
		" flags",                // leading space
		"",                      // empty
		strings.Repeat("f", 65), // over length cap
	}
	for _, name := range rejected {
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		_, err = NewSQLStore(db, WithSQLTable(name))
		db.Close()
		if err == nil {
			t.Errorf("table name %q was accepted, want rejection", name)
			continue
		}
		// Rejection must come from the guard itself, not from a failed
		// CREATE TABLE: the guard runs before any SQL is executed.
		if !strings.Contains(err.Error(), "unsafe table name") {
			t.Errorf("table name %q rejected with %q, want the unsafe-table-name guard", name, err)
		}
	}

	// Valid names must still be accepted, and a bad dialect rejected.
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	for _, name := range []string{"feature_flags", "flags_v2", "_f"} {
		s, err := NewSQLStore(db, WithSQLTable(name))
		if err != nil {
			t.Errorf("valid table name %q rejected: %v", name, err)
			continue
		}
		_ = s
	}
	if _, err := NewSQLStore(db, WithSQLDialect("mysql")); err == nil {
		t.Error("unsupported dialect was accepted, want rejection (postgres|sqlite only)")
	}
}

// TestSQLCorruptListColumnsFailLoud asserts that a corrupted allow-list
// JSON column makes the store ERROR — never silently evaluate as an empty
// list. The rows are written by admin tooling / other replicas, so a
// partial write or schema drift is a real state an evaluator can hit; a
// silent empty list re-enables (or re-disables) a flag nobody changed.
// Surfaces: Get on users / tenants / envs, and All.
func TestSQLCorruptListColumnsFailLoud(t *testing.T) {
	db, s := openSQLStore(t)
	ctx := context.Background()
	if err := s.Set(Flag{Key: "checkout", Enabled: true, Rollout: 100, Users: []string{"u1"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for _, col := range []string{"users", "tenants", "envs"} {
		if _, err := db.ExecContext(ctx, "UPDATE feature_flags SET "+col+" = ? WHERE key = 'checkout'", "not-json"); err != nil {
			t.Fatalf("corrupt %s: %v", col, err)
		}
		if _, err := s.Get(ctx, "checkout"); err == nil {
			t.Errorf("SECURITY: [featureflag] Get tolerated corrupt %s column, want a loud error. "+
				"Attack: a corrupted allow list silently re-evaluates the flag with nobody watching.", col)
		}
		if _, err := s.All(ctx); err == nil {
			t.Errorf("All tolerated corrupt %s column, want a loud error", col)
		}
	}
	// And the evaluator reading that corrupt row fails closed.
	e := NewEvaluator(s)
	if e.Bool(featureflagCtx(ctx, "u1", "", ""), "checkout") {
		t.Error("evaluator answered true from a corrupt row; must fail closed (false)")
	}
}

// featureflagCtx is a one-line helper: context with an EvalContext.
func featureflagCtx(ctx context.Context, user, tenant, env string) context.Context {
	return WithContext(ctx, EvalContext{UserID: user, TenantID: tenant, Env: env})
}

// TestSQLAdversarialKeyRoundTrips pins that the flag KEY — attacker-influenced
// wherever admin APIs set flags — only ever travels as a bind parameter.
// Get, Set, and Delete interpolate only the (guarded) table name; a key
// full of statement syntax must round-trip as inert data and leave the
// table intact.
func TestSQLAdversarialKeyRoundTrips(t *testing.T) {
	_, s := openSQLStore(t)
	ctx := context.Background()
	if err := s.Set(Flag{Key: "stable", Enabled: true, Rollout: 0}); err != nil {
		t.Fatalf("seed stable: %v", err)
	}
	evil := "x'; DROP TABLE feature_flags; --"
	if err := s.Set(Flag{Key: evil, Enabled: true, Rollout: 100}); err != nil {
		t.Fatalf("set hostile key: %v", err)
	}
	got, err := s.Get(ctx, evil)
	if err != nil || got == nil || !got.Enabled {
		t.Fatalf("hostile key did not round-trip: %v %+v", err, got)
	}
	if err := s.Delete(evil); err != nil {
		t.Fatalf("delete hostile key: %v", err)
	}
	// The table and the unrelated flag survived every hostile-key statement.
	if stable, err := s.Get(ctx, "stable"); err != nil || stable == nil || !stable.Enabled {
		t.Fatalf("SECURITY: [featureflag] hostile key damaged the table: stable=%v err=%v. "+
			"Attack: a crafted flag key executed as SQL drops or alters the flag store.", stable, err)
	}
}
