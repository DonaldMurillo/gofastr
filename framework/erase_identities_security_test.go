package framework

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)

// The identities kept for a retry go once expired, even when no later
// erasure runs (primitives app.privacy erase-retention-expires).
func TestExpiredKeptIdentityIsSwept(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	ctx := context.Background()
	seedRetention(t, db, "", "u1", "s", "u1@x.test", time.Now().Add(-time.Hour))
	seedRetention(t, db, "", "u2", "s", "u2@x.test", time.Now().Add(time.Hour))
	ctx2, cancel := context.WithCancel(ctx)
	defer cancel()
	app.startRetentionSweep(ctx2)
	deadline := time.Now().Add(5 * time.Second)
	for countWhere(t, db, eraseIdentitiesTable, "user_id", "u1") != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the App's start sweep left an expired kept email stored")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n := countWhere(t, db, eraseIdentitiesTable, "user_id", "u2"); n != 1 {
		t.Fatalf("the sweep removed an unexpired row: %d left", n)
	}
}

// An erasure that fails a store, with nothing of its own to keep, still
// sweeps other users' expired rows.
func TestEraseFailureWithNothingToKeepSweeps(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	ctx := context.Background()
	registerMagicLinkEraseScenario(t)
	regStore(t, datexport.StoreEraser{Name: "down", Source: "test",
		EraseResolved: func(context.Context, datexport.ErasedUser, bool) (int, error) { return 0, errors.New("down") }})
	seedRetention(t, db, "", "old", "down", "old@x.test", time.Now().Add(-time.Hour))
	if _, err := app.EraseUserData(ctx, "nobody"); err == nil {
		t.Fatal("a failed store did not fail the erasure")
	}
	if n := countWhere(t, db, eraseIdentitiesTable, "user_id", "old"); n != 0 {
		t.Fatal("an expired row survived an erasure that failed with nothing to keep")
	}
}

// A retained address another account has taken since never reaches the
// SQL plan or a store: u2's magic-link token survives u1's retry.
func TestRetainedIdentityNeverErasesItsNewOwner(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	ctx := context.Background()
	registerMagicLinkEraseScenario(t)
	var got []datexport.ErasedUser
	regStore(t, datexport.StoreEraser{Name: "flaky", Source: "test",
		EraseResolved: func(_ context.Context, u datexport.ErasedUser, _ bool) (int, error) {
			got = append(got, u)
			return 0, nil
		}})
	seedRetention(t, db, "", "u1", "flaky", "shared@x.test", time.Now().Add(time.Hour))
	for _, q := range []string{
		`INSERT INTO auth_users (id, email) VALUES ('u2','shared@x.test')`,
		`INSERT INTO magic_link_tokens (token, email, expires_at) VALUES ('t2','shared@x.test', 9999999999)`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := app.EraseUserData(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if n := countWhere(t, db, "magic_link_tokens", "token", "t2"); n != 1 {
		t.Fatal("u1's retry erased the magic link of u2, who now holds the address")
	}
	if _, ok := got[0].Identity(datexport.IdentityEmail); ok {
		t.Fatalf("the store got an address another account holds: %+v", got[0])
	}
}

// Only a store that failed gets the retained identities on a retry; one
// that succeeded gets what resolves now.
func TestRetryReplaysOnlyToFailedStores(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_users (id, email) VALUES ('u1','u1@x.test')`); err != nil {
		t.Fatal(err)
	}
	registerMagicLinkEraseScenario(t)
	got := map[string][]datexport.ErasedUser{}
	fail := true
	for _, name := range []string{"good", "flaky"} {
		regStore(t, datexport.StoreEraser{Name: name, Source: "test",
			EraseResolved: func(_ context.Context, u datexport.ErasedUser, _ bool) (int, error) {
				got[name] = append(got[name], u)
				if name == "flaky" && fail {
					return 0, errors.New("down")
				}
				return 0, nil
			}})
	}
	if _, err := app.EraseUserData(ctx, "u1"); err == nil {
		t.Fatal("a failed store did not fail the erasure")
	}
	fail = false
	if _, err := app.EraseUserData(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["good"][1].Identity(datexport.IdentityEmail); ok {
		t.Fatalf("a store that succeeded got the retained email again: %+v", got["good"][1])
	}
	if e, _ := got["flaky"][1].Identity(datexport.IdentityEmail); e != "u1@x.test" {
		t.Fatalf("the failed store's retry got %+v", got["flaky"][1])
	}
}

// Nothing is kept when only an id-keyed store failed: no store that
// needs an identity is waiting for one.
func TestIDOnlyStoreFailureKeepsNothing(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_users (id, email) VALUES ('u1','u1@x.test')`); err != nil {
		t.Fatal(err)
	}
	registerMagicLinkEraseScenario(t)
	regStore(t, datexport.StoreEraser{Name: "resolved", Source: "test",
		EraseResolved: func(context.Context, datexport.ErasedUser, bool) (int, error) { return 0, nil }})
	regStore(t, datexport.StoreEraser{Name: "byid", Source: "test",
		Erase: func(context.Context, string, bool) (int, error) { return 0, errors.New("down") }})
	if _, err := app.EraseUserData(ctx, "u1"); err == nil {
		t.Fatal("a failed store did not fail the erasure")
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, eraseIdentitiesTable).Scan(&n); err != nil || n != 0 {
		t.Fatalf("an id-only store's failure kept the email: table count %d %v", n, err)
	}
}

// A retry that fails again keeps the first failure's expiry; and a
// retry that fails after a concurrent retry removed the row never writes
// it back.
func TestRetryKeepsTheFirstExpiryAndNeverRestores(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	ctx := context.Background()
	registerMagicLinkEraseScenario(t)
	concurrent := false
	regStore(t, datexport.StoreEraser{Name: "flaky", Source: "test",
		EraseResolved: func(context.Context, datexport.ErasedUser, bool) (int, error) {
			if concurrent {
				// Another retry succeeds meanwhile and removes the row.
				if _, err := db.ExecContext(ctx, `DELETE FROM `+eraseIdentitiesTable); err != nil {
					t.Fatal(err)
				}
			}
			return 0, errors.New("down")
		}})
	first := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	seedRetention(t, db, "", "u1", "flaky", "u1@x.test", first)
	if _, err := app.EraseUserData(ctx, "u1"); err == nil {
		t.Fatal("a failed store did not fail the erasure")
	}
	var exp int64
	if err := db.QueryRowContext(ctx, `SELECT expires_at FROM `+eraseIdentitiesTable+` WHERE user_id = 'u1'`).Scan(&exp); err != nil || exp != first.Unix() {
		t.Fatalf("expiry after a failed retry: %d %v, want the first one %d", exp, err, first.Unix())
	}
	concurrent = true
	if _, err := app.EraseUserData(ctx, "u1"); err == nil {
		t.Fatal("a failed store did not fail the erasure")
	}
	if n := countWhere(t, db, eraseIdentitiesTable, "user_id", "u1"); n != 0 {
		t.Fatal("a slower failed retry wrote back the email a concurrent retry removed")
	}
}

// The request's context ending once the SQL erasure committed does not
// stop the identities a retry needs from being kept.
func TestRetentionLandsAfterTheRequestEnds(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	if _, err := db.Exec(`INSERT INTO auth_users (id, email) VALUES ('u1','u1@x.test')`); err != nil {
		t.Fatal(err)
	}
	registerMagicLinkEraseScenario(t)
	ctx, cancel := context.WithCancel(context.Background())
	regStore(t, datexport.StoreEraser{Name: "slow", Source: "test",
		EraseResolved: func(context.Context, datexport.ErasedUser, bool) (int, error) {
			cancel()
			return 0, errors.New("timed out")
		}})
	if _, err := app.EraseUserData(ctx, "u1"); err == nil {
		t.Fatal("a failed store did not fail the erasure")
	}
	if n := countWhere(t, db, eraseIdentitiesTable, "user_id", "u1"); n != 1 {
		t.Fatalf("kept rows after the request ended: %d, want 1", n)
	}
}

// regStore registers s for this test only.
func regStore(t *testing.T, s datexport.StoreEraser) {
	t.Helper()
	_, unregister := datexport.RegisterStoreEraser(s)
	t.Cleanup(unregister)
}

// The retry flow on each dialect: a failing store keeps the email, the
// retry hands it back, a taken address is withheld, and success removes
// the row.
func TestRetentionFlowEachDialect(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ctx := context.Background()
		for _, ddl := range []string{
			`CREATE TABLE auth_users (id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL)`,
			`INSERT INTO auth_users (id, email) VALUES ('u1','u1@x.test')`,
		} {
			if _, err := db.ExecContext(ctx, ddl); err != nil {
				t.Fatal(err)
			}
		}
		datexport.Reset(t)
		datexport.RegisterIdentityResolver(datexport.IdentityEmail, datexport.DataIdentityResolver{
			Table: "auth_users", IDColumn: "id", ValueColumn: "email",
		})
		datexport.RegisterEraser(datexport.DataEraser{Name: "auth_users", Source: "auth", Table: "auth_users", Column: "id", Mode: datexport.EraseDelete})
		t.Cleanup(func() { datexport.Reset(t) })
		var got []datexport.ErasedUser
		fail := true
		regStore(t, datexport.StoreEraser{Name: "flaky", Source: "test",
			EraseResolved: func(_ context.Context, u datexport.ErasedUser, _ bool) (int, error) {
				got = append(got, u)
				if fail {
					return 0, errors.New("down")
				}
				return 0, nil
			}})
		app := NewApp(WithDB(db))
		if _, err := app.EraseUserData(ctx, "u1"); err == nil {
			t.Fatal("a failed store did not fail the erasure")
		}
		if _, err := app.EraseUserData(ctx, "u1"); err == nil {
			t.Fatal("a failed retry did not fail the erasure")
		}
		if e, _ := got[1].Identity(datexport.IdentityEmail); e != "u1@x.test" {
			t.Fatalf("retry got %+v", got[1])
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO auth_users (id, email) VALUES ('u2','U1@x.test')`); err != nil {
			t.Fatal(err)
		}
		fail = false
		if _, err := app.EraseUserData(ctx, "u1"); err != nil {
			t.Fatal(err)
		}
		if _, ok := got[2].Identity(datexport.IdentityEmail); ok {
			t.Fatalf("a taken address reached the store: %+v", got[2])
		}
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+eraseIdentitiesTable).Scan(&n); err != nil || n != 0 {
			t.Fatalf("rows left after success: %d %v", n, err)
		}
		if err := app.SweepErasureRetention(ctx); err != nil {
			t.Fatal(err)
		}
	})
}
