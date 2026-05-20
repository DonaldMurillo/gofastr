package featureflag

import (
	"context"
	"database/sql"
	"reflect"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func openSQLStore(t *testing.T) (*sql.DB, *SQLStore) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	s, err := NewSQLStore(db)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return db, s
}

func TestSQLStore_RoundTrip(t *testing.T) {
	_, s := openSQLStore(t)
	in := Flag{
		Key:     "new-checkout",
		Enabled: true,
		Rollout: 25,
		Users:   []string{"alice", "bob"},
		Tenants: []string{"acme"},
	}
	if err := s.Set(in); err != nil {
		t.Fatalf("set: %v", err)
	}
	out, err := s.Get(context.Background(), "new-checkout")
	if err != nil || out == nil {
		t.Fatalf("get: %v %v", out, err)
	}
	if out.Key != in.Key || out.Enabled != in.Enabled || out.Rollout != in.Rollout {
		t.Fatalf("scalar mismatch: %+v vs %+v", out, in)
	}
	if !reflect.DeepEqual(out.Users, in.Users) {
		t.Fatalf("users: got %v want %v", out.Users, in.Users)
	}
	if !reflect.DeepEqual(out.Tenants, in.Tenants) {
		t.Fatalf("tenants: got %v want %v", out.Tenants, in.Tenants)
	}
}

func TestSQLStore_GetMissingNil(t *testing.T) {
	_, s := openSQLStore(t)
	got, err := s.Get(context.Background(), "nope")
	if err != nil || got != nil {
		t.Fatalf("missing key: got (%v, %v), want (nil, nil)", got, err)
	}
}

func TestSQLStore_UpsertReplacesExisting(t *testing.T) {
	_, s := openSQLStore(t)
	if err := s.Set(Flag{Key: "x", Enabled: false, Rollout: 0}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(Flag{Key: "x", Enabled: true, Rollout: 100, Users: []string{"u"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(context.Background(), "x")
	if !got.Enabled || got.Rollout != 100 || len(got.Users) != 1 {
		t.Fatalf("upsert: got %+v", got)
	}
}

func TestSQLStore_Delete(t *testing.T) {
	_, s := openSQLStore(t)
	_ = s.Set(Flag{Key: "x", Enabled: true})
	if err := s.Delete("x"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(context.Background(), "x")
	if got != nil {
		t.Fatalf("expected delete to remove row, got %+v", got)
	}
}

func TestSQLStore_All(t *testing.T) {
	_, s := openSQLStore(t)
	_ = s.Set(Flag{Key: "b", Enabled: true})
	_ = s.Set(Flag{Key: "a", Enabled: false})
	all, err := s.All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Key != "a" || all[1].Key != "b" {
		t.Fatalf("All sorted by key: got %+v", all)
	}
}

func TestSQLStore_DriveEvaluator(t *testing.T) {
	_, s := openSQLStore(t)
	_ = s.Set(Flag{Key: "x", Enabled: true, Rollout: 100})
	e := NewEvaluator(s)
	if !e.Bool(context.Background(), "x") {
		t.Fatalf("evaluator should see persisted rollout=100 flag as on")
	}
}

func TestSQLStore_UnsafeTableRejected(t *testing.T) {
	db, _ := openSQLStore(t)
	if _, err := NewSQLStore(db, WithSQLTable("not safe")); err == nil {
		t.Fatal("expected unsafe table name to be rejected")
	}
}
