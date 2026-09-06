package desktop

import (
	"context"
	"path/filepath"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Group 5: grant stores. The SQLite store runs on a temp FILE database
// (not :memory:, with a pool larger than one, each connection would
// get its own empty in-memory database).

func TestSQLGrantStoreRoundTrip(t *testing.T) {
	db, err := openAppDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := newSQLGrantStore(db)
	if err := s.ensureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, found, err := s.Get(context.Background(), "clipboard", "clipboard:read"); err != nil || found {
		t.Fatalf("initial Get = (found %v, err %v), want miss", found, err)
	}
	if err := s.Set(context.Background(), "clipboard", "clipboard:read", grantAllow); err != nil {
		t.Fatal(err)
	}
	dec, found, err := s.Get(context.Background(), "clipboard", "clipboard:read")
	if err != nil || !found || dec != grantAllow {
		t.Fatalf("Get after Set = (%q, %v, %v)", dec, found, err)
	}
	// Upsert replaces.
	if err := s.Set(context.Background(), "clipboard", "clipboard:read", grantDeny); err != nil {
		t.Fatal(err)
	}
	dec, _, _ = s.Get(context.Background(), "clipboard", "clipboard:read")
	if dec != grantDeny {
		t.Fatalf("decision after overwrite = %q, want deny", dec)
	}
	// Reset clears everything.
	if err := s.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, found, err := s.Get(context.Background(), "clipboard", "clipboard:read"); err != nil || found {
		t.Fatalf("Get after Reset = (found %v, err %v), want miss", found, err)
	}
	// Bad decisions are refused.
	if err := s.Set(context.Background(), "x", "x:y", "maybe"); err == nil {
		t.Fatal("garbage decision accepted")
	}
}

// TestSQLGrantStoreSurvivesReopen proves persistence across processes:
// a fresh store on the same file sees the earlier grant.
func TestSQLGrantStoreSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	db1, err := openAppDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	s1 := newSQLGrantStore(db1)
	if err := s1.ensureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s1.Set(context.Background(), "fs", "fs:read", grantAllow); err != nil {
		t.Fatal(err)
	}
	db1.Close()

	db2, err := openAppDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db2.Close() })
	s2 := newSQLGrantStore(db2)
	dec, found, err := s2.Get(context.Background(), "fs", "fs:read")
	if err != nil || !found || dec != grantAllow {
		t.Fatalf("reopened Get = (%q, %v, %v), want persisted allow", dec, found, err)
	}
	_ = filepath.Join // silence unused-import churn if the fixture moves
}

func TestMemGrantStore(t *testing.T) {
	s := newMemGrantStore()
	ctx := context.Background()
	if _, found, err := s.Get(ctx, "c", "p"); err != nil || found {
		t.Fatalf("initial = (%v, %v)", found, err)
	}
	if err := s.Set(ctx, "c", "p", grantDeny); err != nil {
		t.Fatal(err)
	}
	if dec, found, _ := s.Get(ctx, "c", "p"); !found || dec != grantDeny {
		t.Fatalf("after Set = (%q, %v)", dec, found)
	}
	if err := s.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := s.Get(ctx, "c", "p"); found {
		t.Fatal("Reset did not clear")
	}
	if err := s.Set(ctx, "c", "p", "sometimes"); err == nil {
		t.Fatal("garbage decision accepted")
	}
}
