package framework

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
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
	// The same email still resolves live, so the failure's insert meets
	// the kept row: the conflict must leave its first expiry.
	if _, err := db.Exec(`INSERT INTO auth_users (id, email) VALUES ('u1','u1@x.test')`); err != nil {
		t.Fatal(err)
	}
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

// A store that runs past the bookkeeping deadline and then fails still
// gets its identity kept: the write's deadline starts after the stores.
func TestRetentionDeadlineStartsAfterTheStores(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	if _, err := db.Exec(`INSERT INTO auth_users (id, email) VALUES ('u1','u1@x.test')`); err != nil {
		t.Fatal(err)
	}
	registerMagicLinkEraseScenario(t)
	old := retentionWriteTimeout
	retentionWriteTimeout = 100 * time.Millisecond
	t.Cleanup(func() { retentionWriteTimeout = old })
	regStore(t, datexport.StoreEraser{Name: "slow", Source: "test",
		EraseResolved: func(context.Context, datexport.ErasedUser, bool) (int, error) {
			time.Sleep(300 * time.Millisecond)
			return 0, errors.New("timed out")
		}})
	if _, err := app.EraseUserData(context.Background(), "u1"); err == nil {
		t.Fatal("a failed store did not fail the erasure")
	}
	if n := countWhere(t, db, eraseIdentitiesTable, "user_id", "u1"); n != 1 {
		t.Fatalf("kept rows after a slow failing store: %d, want 1", n)
	}
}

// A holder check that cannot answer withholds the value and keeps its
// row, even when the store then succeeds, and the erasure says so.
func TestInconclusiveHolderCheckKeepsTheRow(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	registerMagicLinkEraseScenario(t)
	seedRetention(t, db, "", "u1", "b", "u1@x.test", time.Now().Add(time.Hour))
	heldCheckFault = func() error { return errors.New("connection reset") }
	t.Cleanup(func() { heldCheckFault = nil })
	var got datexport.ErasedUser
	regStore(t, datexport.StoreEraser{Name: "b", Source: "test",
		EraseResolved: func(_ context.Context, u datexport.ErasedUser, _ bool) (int, error) {
			got = u
			return 0, nil
		}})
	_, err := app.EraseUserData(context.Background(), "u1")
	if err == nil || !strings.Contains(err.Error(), "could not be told") {
		t.Fatalf("erasure with an inconclusive holder check: %v", err)
	}
	if _, ok := got.Identity(datexport.IdentityEmail); ok {
		t.Fatalf("an unchecked kept value reached the store: %+v", got)
	}
	if n := countWhere(t, db, eraseIdentitiesTable, "store", "4:test/b"); n != 1 {
		t.Fatalf("rows for b after an inconclusive check: %d, want 1", n)
	}
}

// A spelling the resolver folds to the kept value counts as held, even
// one LOWER keeps apart.
func TestHolderCheckUsesTheResolversFold(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	datexport.Reset(t)
	t.Cleanup(func() { datexport.Reset(t) })
	noDots := func(s string) string { return strings.ReplaceAll(strings.ToLower(s), ".", "") }
	datexport.RegisterIdentityResolver(datexport.IdentityEmail, datexport.DataIdentityResolver{
		Table: "auth_users", IDColumn: "id", ValueColumn: "email", Canonicalize: noDots,
	})
	datexport.RegisterEraser(datexport.DataEraser{Name: "auth_users", Source: "auth", Table: "auth_users", Column: "id", Mode: datexport.EraseDelete})
	if _, err := db.Exec(`INSERT INTO auth_users (id, email) VALUES ('u2','a.b@x.test')`); err != nil {
		t.Fatal(err)
	}
	seedRetention(t, db, "", "u1", "s", "ab@xtest", time.Now().Add(time.Hour))
	var got datexport.ErasedUser
	regStore(t, datexport.StoreEraser{Name: "s", Source: "test",
		EraseResolved: func(_ context.Context, u datexport.ErasedUser, _ bool) (int, error) {
			got = u
			return 0, nil
		}})
	if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Identity(datexport.IdentityEmail); ok {
		t.Fatalf("a value another account holds under the resolver's fold reached the store: %+v", got)
	}
}

// An expired row for the same store does not block the new failure's
// row: expired rows go before the insert.
func TestExpiredRowDoesNotBlockANewFailure(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	if _, err := db.Exec(`INSERT INTO auth_users (id, email) VALUES ('u1','u1@x.test')`); err != nil {
		t.Fatal(err)
	}
	registerMagicLinkEraseScenario(t)
	seedRetention(t, db, "", "u1", "flaky", "u1@x.test", time.Now().Add(-time.Hour))
	regStore(t, datexport.StoreEraser{Name: "flaky", Source: "test",
		EraseResolved: func(context.Context, datexport.ErasedUser, bool) (int, error) { return 0, errors.New("down") }})
	if _, err := app.EraseUserData(context.Background(), "u1"); err == nil {
		t.Fatal("a failed store did not fail the erasure")
	}
	var exp int64
	if err := db.QueryRow(`SELECT expires_at FROM ` + eraseIdentitiesTable + ` WHERE user_id = 'u1'`).Scan(&exp); err != nil || exp < time.Now().Unix() {
		t.Fatalf("after a new failure over an expired row: expires %d %v, want one unexpired row", exp, err)
	}
}

// fakeRetentionClock pins the retention clock for a test.
func fakeRetentionClock(t *testing.T, at time.Time) *time.Time {
	t.Helper()
	now := at
	old := retentionNow
	retentionNow = func() time.Time { return now }
	t.Cleanup(func() { retentionNow = old })
	return &now
}

// A kept value that expires while earlier stores run is not handed to a
// later store.
func TestRetentionExpiryIsCheckedAtHandoff(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	registerMagicLinkEraseScenario(t)
	start := time.Now()
	clock := fakeRetentionClock(t, start)
	seedRetention(t, db, "", "u1", "b-late", "u1@x.test", start.Add(10*time.Second))
	// Stores run in name order: a-slow first, and the clock passes the
	// row's expiry while it runs.
	regStore(t, datexport.StoreEraser{Name: "a-slow", Source: "test",
		EraseResolved: func(context.Context, datexport.ErasedUser, bool) (int, error) {
			*clock = start.Add(20 * time.Second)
			return 0, nil
		}})
	var got datexport.ErasedUser
	regStore(t, datexport.StoreEraser{Name: "b-late", Source: "test",
		EraseResolved: func(_ context.Context, u datexport.ErasedUser, _ bool) (int, error) {
			got = u
			return 0, nil
		}})
	if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
		t.Fatal(err)
	}
	if got.ID == "" {
		t.Fatal("the later store never ran")
	}
	if _, ok := got.Identity(datexport.IdentityEmail); ok {
		t.Fatalf("an identity that expired during the run reached a later store: %+v", got)
	}
}

// A value whose holder lookup finishes after its expiry is not handed
// over.
func TestRetentionExpiryIsRecheckedAfterTheLookup(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	registerMagicLinkEraseScenario(t)
	start := time.Now()
	clock := fakeRetentionClock(t, start)
	seedRetention(t, db, "", "u1", "s", "u1@x.test", start.Add(10*time.Second))
	heldCheckFault = func() error { *clock = start.Add(20 * time.Second); return nil }
	t.Cleanup(func() { heldCheckFault = nil })
	var got datexport.ErasedUser
	regStore(t, datexport.StoreEraser{Name: "s", Source: "test",
		EraseResolved: func(_ context.Context, u datexport.ErasedUser, _ bool) (int, error) {
			got = u
			return 0, nil
		}})
	if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Identity(datexport.IdentityEmail); ok {
		t.Fatalf("a value that expired during its lookup reached the store: %+v", got)
	}
}

// A dry run of a retry counts with what was kept for each failed store,
// and writes nothing.
func TestDryRunRetryUsesKeptIdentities(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	registerMagicLinkEraseScenario(t)
	seedRetention(t, db, "", "u1", "s", "u1@x.test", time.Now().Add(time.Hour))
	var got datexport.ErasedUser
	var dry bool
	regStore(t, datexport.StoreEraser{Name: "s", Source: "test",
		EraseResolved: func(_ context.Context, u datexport.ErasedUser, d bool) (int, error) {
			got, dry = u, d
			return 1, nil
		}})
	if _, err := app.EraseUserData(context.Background(), "u1", WithEraseDryRun()); err != nil {
		t.Fatal(err)
	}
	if e, _ := got.Identity(datexport.IdentityEmail); e != "u1@x.test" || !dry {
		t.Fatalf("dry run gave the store %+v (dry %v), want the kept email", got, dry)
	}
	if n := countWhere(t, db, eraseIdentitiesTable, "user_id", "u1"); n != 1 {
		t.Fatalf("a dry run changed the kept rows: %d", n)
	}
}

// A dry run whose holder check cannot answer says its count may be short.
func TestDryRunReportsAWithheldValue(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	registerMagicLinkEraseScenario(t)
	seedRetention(t, db, "", "u1", "s", "u1@x.test", time.Now().Add(time.Hour))
	heldCheckFault = func() error { return errors.New("connection reset") }
	t.Cleanup(func() { heldCheckFault = nil })
	regStore(t, datexport.StoreEraser{Name: "s", Source: "test",
		EraseResolved: func(context.Context, datexport.ErasedUser, bool) (int, error) { return 0, nil }})
	if _, err := app.EraseUserData(context.Background(), "u1", WithEraseDryRun()); err == nil || !strings.Contains(err.Error(), "could not be told") {
		t.Fatalf("dry run with an inconclusive holder check: %v", err)
	}
}

// Rows are keyed by the store's source and name: a store of another
// source under the same name gets nothing kept for the first.
func TestRetentionIsKeyedBySourceAndName(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	registerMagicLinkEraseScenario(t)
	seedRetention(t, db, "", "u1", "s", "u1@x.test", time.Now().Add(time.Hour))
	var got datexport.ErasedUser
	regStore(t, datexport.StoreEraser{Name: "s", Source: "other",
		EraseResolved: func(_ context.Context, u datexport.ErasedUser, _ bool) (int, error) {
			got = u
			return 0, nil
		}})
	if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Identity(datexport.IdentityEmail); ok {
		t.Fatalf("another source's store got the kept email: %+v", got)
	}
	if n := countWhere(t, db, eraseIdentitiesTable, "store", "4:test/s"); n != 1 {
		t.Fatalf("rows for test/s after another store succeeded: %d, want 1", n)
	}
}

// A resolver whose Canonicalize panics on a row gives no answer: the
// value is withheld and its row kept. NULL and empty values are skipped
// as resolution skips them.
func TestCanonicalizerPanicIsNoAnswer(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	datexport.Reset(t)
	t.Cleanup(func() { datexport.Reset(t) })
	datexport.RegisterIdentityResolver(datexport.IdentityEmail, datexport.DataIdentityResolver{
		Table: "auth_users", IDColumn: "id", ValueColumn: "email",
		Canonicalize: func(s string) string {
			if s == "" || strings.HasPrefix(s, "boom") {
				panic("bad address")
			}
			return strings.ToLower(s)
		},
	})
	datexport.RegisterEraser(datexport.DataEraser{Name: "auth_users", Source: "auth", Table: "auth_users", Column: "id", Mode: datexport.EraseDelete})
	if _, err := db.Exec(`INSERT INTO auth_users (id, email) VALUES ('u3', '')`); err != nil {
		t.Fatal(err)
	}
	seedRetention(t, db, "", "u1", "s", "u1@x.test", time.Now().Add(time.Hour))
	var got datexport.ErasedUser
	regStore(t, datexport.StoreEraser{Name: "s", Source: "test",
		EraseResolved: func(_ context.Context, u datexport.ErasedUser, _ bool) (int, error) {
			got = u
			return 0, nil
		}})
	// The empty row is skipped, so the value is handed over.
	if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
		t.Fatalf("an empty resolver value was folded: %v", err)
	}
	if e, _ := got.Identity(datexport.IdentityEmail); e != "u1@x.test" {
		t.Fatalf("store got %+v, want the kept email", got)
	}
	// A panic on a real value is no answer.
	seedRetention(t, db, "", "u1", "s", "u1@x.test", time.Now().Add(time.Hour))
	if _, err := db.Exec(`INSERT INTO auth_users (id, email) VALUES ('u4', 'boom@x.test')`); err != nil {
		t.Fatal(err)
	}
	got = datexport.ErasedUser{}
	if _, err := app.EraseUserData(context.Background(), "u1"); err == nil || !strings.Contains(err.Error(), "could not be told") {
		t.Fatalf("a panicking Canonicalize: %v, want the withheld error", err)
	}
	if _, ok := got.Identity(datexport.IdentityEmail); ok {
		t.Fatalf("the store got a value whose check panicked: %+v", got)
	}
	if n := countWhere(t, db, eraseIdentitiesTable, "store", "4:test/s"); n != 1 {
		t.Fatalf("rows after a panicking check: %d, want 1", n)
	}
}

// Without a Canonicalize, a case variant outside ASCII counts as held.
func TestNonASCIICaseVariantIsHeld(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	registerMagicLinkEraseScenario(t)
	if _, err := db.Exec(`INSERT INTO auth_users (id, email) VALUES ('u2', 'Équipe@x.test')`); err != nil {
		t.Fatal(err)
	}
	seedRetention(t, db, "", "u1", "s", "équipe@x.test", time.Now().Add(time.Hour))
	var got datexport.ErasedUser
	regStore(t, datexport.StoreEraser{Name: "s", Source: "test",
		EraseResolved: func(_ context.Context, u datexport.ErasedUser, _ bool) (int, error) {
			got = u
			return 0, nil
		}})
	if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Identity(datexport.IdentityEmail); ok {
		t.Fatalf("a non-ASCII case variant another account holds reached the store: %+v", got)
	}
}

// A success removes only the rows it read: one a concurrent erasure
// wrote while the store ran stays.
func TestSuccessKeepsRowsWrittenSinceItsRead(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	registerMagicLinkEraseScenario(t)
	seedRetention(t, db, "", "u1", "s", "old@x.test", time.Now().Add(time.Hour))
	regStore(t, datexport.StoreEraser{Name: "s", Source: "test",
		EraseResolved: func(context.Context, datexport.ErasedUser, bool) (int, error) {
			seedRetention(t, db, "", "u1", "s", "new@x.test", time.Now().Add(time.Hour))
			return 0, nil
		}})
	if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
		t.Fatal(err)
	}
	if n := countWhere(t, db, eraseIdentitiesTable, "value", "new@x.test"); n != 1 {
		t.Fatal("a success removed a row a concurrent erasure wrote after its read")
	}
	if n := countWhere(t, db, eraseIdentitiesTable, "value", "old@x.test"); n != 0 {
		t.Fatal("a success left the row it read")
	}
}

// A store that fails again with a new identity keeps it beside the old
// one, without moving the old one's expiry, and the next retry gives the
// store each.
func TestRepeatedFailureKeepsEachIdentity(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	if _, err := db.Exec(`INSERT INTO auth_users (id, email) VALUES ('u1','b@x.test')`); err != nil {
		t.Fatal(err)
	}
	registerMagicLinkEraseScenario(t)
	first := time.Now().Add(time.Hour).Truncate(time.Second)
	seedRetention(t, db, "", "u1", "s", "a@x.test", first)
	fail := true
	var got []string
	regStore(t, datexport.StoreEraser{Name: "s", Source: "test",
		EraseResolved: func(_ context.Context, u datexport.ErasedUser, _ bool) (int, error) {
			e, _ := u.Identity(datexport.IdentityEmail)
			got = append(got, e)
			if fail {
				return 0, errors.New("down")
			}
			return 0, nil
		}})
	if _, err := app.EraseUserData(context.Background(), "u1"); err == nil {
		t.Fatal("a failed store did not fail the erasure")
	}
	var exp int64
	if err := db.QueryRow(`SELECT expires_at FROM ` + eraseIdentitiesTable + ` WHERE value = 'a@x.test'`).Scan(&exp); err != nil || exp != first.Unix() {
		t.Fatalf("the old row's expiry: %d %v, want %d", exp, err, first.Unix())
	}
	if n := countWhere(t, db, eraseIdentitiesTable, "value", "b@x.test"); n != 1 {
		t.Fatal("the new identity the store failed with was not kept")
	}
	fail, got = false, nil
	if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{"a@x.test", "b@x.test"}) {
		t.Fatalf("the retry gave the store %v, want both kept identities", got)
	}
	if n := countWhere(t, db, eraseIdentitiesTable, "store", "4:test/s"); n != 0 {
		t.Fatalf("rows left after the retry succeeded: %d", n)
	}
}

// A kept value already expired at its turn is not looked up at all, so a
// lookup that would fail does not hold its store's rows.
func TestExpiredValueIsNotLookedUp(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	registerMagicLinkEraseScenario(t)
	start := time.Now()
	clock := fakeRetentionClock(t, start)
	seedRetention(t, db, "", "u1", "b", "u1@x.test", start.Add(10*time.Second))
	regStore(t, datexport.StoreEraser{Name: "a", Source: "test",
		EraseResolved: func(context.Context, datexport.ErasedUser, bool) (int, error) {
			*clock = start.Add(20 * time.Second)
			return 0, nil
		}})
	regStore(t, datexport.StoreEraser{Name: "b", Source: "test",
		EraseResolved: func(context.Context, datexport.ErasedUser, bool) (int, error) { return 0, nil }})
	heldCheckFault = func() error { return errors.New("connection reset") }
	t.Cleanup(func() { heldCheckFault = nil })
	if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
		t.Fatalf("an expired value was looked up: %v", err)
	}
}

// One store's success removes only its own rows: another store that
// failed again keeps its email for the next retry.
func TestOneStoresSuccessKeepsAnothersRows(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	if _, err := db.Exec(`INSERT INTO auth_users (id, email) VALUES ('u1','u1@x.test')`); err != nil {
		t.Fatal(err)
	}
	registerMagicLinkEraseScenario(t)
	run := 0
	var bGot []string
	regStore(t, datexport.StoreEraser{Name: "a", Source: "test",
		EraseResolved: func(context.Context, datexport.ErasedUser, bool) (int, error) {
			if run == 0 {
				return 0, errors.New("down")
			}
			return 0, nil
		}})
	regStore(t, datexport.StoreEraser{Name: "b", Source: "test",
		EraseResolved: func(_ context.Context, u datexport.ErasedUser, _ bool) (int, error) {
			e, _ := u.Identity(datexport.IdentityEmail)
			bGot = append(bGot, e)
			if run < 2 {
				return 0, errors.New("down")
			}
			return 0, nil
		}})
	for run = 0; run < 3; run++ {
		_, _ = app.EraseUserData(context.Background(), "u1")
	}
	if len(bGot) != 3 || bGot[2] != "u1@x.test" {
		t.Fatalf("store b got %v, want the email on every run", bGot)
	}
}

// A running App sweeps expired rows at start: Start's hooks wire the
// sweep, not only a direct call.
func TestAppStartSweepsExpiredRetention(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	seedRetention(t, db, "", "u1", "s", "u1@x.test", time.Now().Add(-time.Hour))
	if err := app.runStartHooks(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	deadline := time.Now().Add(5 * time.Second)
	for countWhere(t, db, eraseIdentitiesTable, "user_id", "u1") != 0 {
		if time.Now().After(deadline) {
			t.Fatal("App start left an expired kept email stored")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type retentionSweepProbe struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *retentionSweepProbe) Connect(ctx context.Context) (driver.Conn, error) {
	p.once.Do(func() { close(p.entered) })
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.release:
		return nil, errors.New("no such function: version")
	}
}

func (*retentionSweepProbe) Driver() driver.Driver { return nil }

func TestShutdownJoinsRetentionSweepDuringBoot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		probe := retentionSweepProbe{entered: make(chan struct{}), release: make(chan struct{})}
		db := sql.OpenDB(&probe)
		defer db.Close()
		app := NewApp(WithoutAutoMigrate(), WithoutDefaultMiddleware())
		app.WithSeed(func(context.Context) error {
			app.DB = db
			return nil
		})
		started := make(chan struct{})
		app.OnStart(func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
		stopped := make(chan struct{})
		app.OnStop(func() error {
			close(stopped)
			return db.Close()
		})

		run := make(chan error, 1)
		host := &countingHost{}
		go func() { run <- app.Run(context.Background(), host) }()
		<-started
		<-probe.entered
		stop := make(chan error, 1)
		go func() { stop <- app.Shutdown(context.Background()) }()
		synctest.Wait()
		if got := host.stops.Load(); got != 1 {
			t.Errorf("host Stop ran %d times while the retention sweep was blocked, want 1", got)
		}
		select {
		case <-stopped:
			t.Error("OnStop closed the database while the retention sweep was still opening a connection")
		default:
		}
		close(probe.release)
		if err := <-stop; err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
		if err := <-run; err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
}

func TestShutdownBoundsRetentionSweepWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		probe := retentionSweepProbe{entered: make(chan struct{})}
		db := sql.OpenDB(&probe)
		defer db.Close()
		defer time.Sleep(20 * time.Second)
		app := NewApp(WithoutAutoMigrate(), WithoutDefaultMiddleware())
		app.WithSeed(func(context.Context) error {
			app.DB = db
			return nil
		})

		stopped := false
		app.OnStop(func() error {
			stopped = true
			return nil
		})
		run := make(chan error, 1)
		go func() { run <- app.Run(context.Background(), ServicesHost()) }()
		<-app.ServicesReady()
		<-probe.entered

		const budget = time.Second
		ctx, cancel := context.WithTimeout(context.Background(), budget)
		defer cancel()
		start := time.Now()
		if err := app.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "retention sweep join abandoned after the shutdown deadline") {
			t.Errorf("Shutdown: %v, want retention sweep join abandoned after the shutdown deadline: context deadline exceeded", err)
		}
		if elapsed := time.Since(start); elapsed != budget {
			t.Errorf("Shutdown spent %s waiting on retention sweep, want %s", elapsed, budget)
		}
		if !stopped {
			t.Error("Shutdown skipped OnStop after the retention sweep wait expired")
		}
		if err := <-run; err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
}

func TestShutdownCanceledWithoutRetentionSweep(t *testing.T) {
	for _, withStopError := range []bool{false, true} {
		app := NewApp(WithoutDefaultMiddleware())
		var want error
		if withStopError {
			want = errors.New("stop hook failed")
			app.OnStop(func() error { return want })
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := app.Shutdown(ctx); !errors.Is(err, want) {
			t.Errorf("Shutdown with no sweep: %v, want %v", err, want)
		}
	}
}

func TestShutdownCanceledAfterRetentionSweepFinished(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		probe := retentionSweepProbe{entered: make(chan struct{}), release: make(chan struct{})}
		db := sql.OpenDB(&probe)
		defer db.Close()
		app := NewApp(WithoutDefaultMiddleware())
		app.DB = db

		ctx, cancel := context.WithCancel(context.Background())
		app.startRetentionSweep(ctx)
		<-probe.entered
		cancel()
		close(probe.release)
		synctest.Wait()

		if err := app.Shutdown(ctx); err != nil {
			t.Fatalf("Shutdown after the sweep finished: %v", err)
		}
	})
}

// No two source and name pairs share a retention key.
func TestStoreKeyIsUnambiguous(t *testing.T) {
	a := storeKey(datexport.StoreEraser{Source: "app", Name: "cache/profiles"})
	b := storeKey(datexport.StoreEraser{Source: "app/cache", Name: "profiles"})
	if a == b {
		t.Fatalf("two stores share the key %q", a)
	}
}

// A kept value deferred because its kind resolves to another value now
// stays when the store succeeds with the live value.
func TestDeferredKeptValueSurvivesASuccess(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	if _, err := db.Exec(`INSERT INTO auth_users (id, email) VALUES ('u1','b@x.test')`); err != nil {
		t.Fatal(err)
	}
	registerMagicLinkEraseScenario(t)
	seedRetention(t, db, "", "u1", "s", "a@x.test", time.Now().Add(time.Hour))
	regStore(t, datexport.StoreEraser{Name: "s", Source: "test",
		EraseResolved: func(context.Context, datexport.ErasedUser, bool) (int, error) { return 0, nil }})
	if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
		t.Fatal(err)
	}
	if n := countWhere(t, db, eraseIdentitiesTable, "value", "a@x.test"); n != 1 {
		t.Fatal("a kept value the store never got was removed by its success")
	}
}

// A further run whose kept value expired during an earlier run of the
// same store does not happen.
func TestFurtherRunRechecksExpiry(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	registerMagicLinkEraseScenario(t)
	start := time.Now()
	clock := fakeRetentionClock(t, start)
	seedRetention(t, db, "", "u1", "s", "a@x.test", start.Add(10*time.Second))
	seedRetention(t, db, "", "u1", "s", "b@x.test", start.Add(10*time.Second))
	var got []string
	regStore(t, datexport.StoreEraser{Name: "s", Source: "test",
		EraseResolved: func(_ context.Context, u datexport.ErasedUser, _ bool) (int, error) {
			e, _ := u.Identity(datexport.IdentityEmail)
			got = append(got, e)
			*clock = start.Add(20 * time.Second)
			return 0, nil
		}})
	if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("store runs %v, want one: the second value expired during the first", got)
	}
}

// A dry run counts what the store keys by user id once, however many
// kept values it runs with.
func TestDryRunCountsIDKeyedRecordsOnce(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	registerMagicLinkEraseScenario(t)
	seedRetention(t, db, "", "u1", "s", "a@x.test", time.Now().Add(time.Hour))
	seedRetention(t, db, "", "u1", "s", "b@x.test", time.Now().Add(time.Hour))
	// One record keyed by the user id, one keyed by each email.
	regStore(t, datexport.StoreEraser{Name: "s", Source: "test",
		EraseResolved: func(_ context.Context, u datexport.ErasedUser, _ bool) (int, error) {
			n := 1
			if _, ok := u.Identity(datexport.IdentityEmail); ok {
				n++
			}
			return n, nil
		}})
	rep, err := app.EraseUserData(context.Background(), "u1", WithEraseDryRun())
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range rep.Stores {
		if st.Name == "s" && st.RowsAffected != 3 {
			t.Fatalf("dry-run count %d, want 3 (the id-keyed record once, each email once)", st.RowsAffected)
		}
	}
}
