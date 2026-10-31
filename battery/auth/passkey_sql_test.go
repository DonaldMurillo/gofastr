package auth

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

// passkeyStoreContract is the behaviour every PasskeyStore owes the
// plugin: duplicate ids refused, the use counter a compare-and-swap,
// deletes scoped to the owner.
func passkeyStoreContract(t *testing.T, s PasskeyStore) {
	t.Helper()
	ctx := context.Background()
	at := time.Unix(1_700_000_000, 0).UTC()
	a := PasskeyCredential{ID: []byte{1, 2, 3}, UserID: "u1", PublicKey: []byte{9, 9}, Algorithm: coseAlgES256,
		SignCount: 4, Name: "Laptop", BackupEligible: true, CreatedAt: at}
	b := PasskeyCredential{ID: []byte{4, 5}, UserID: "u1", PublicKey: []byte{8}, Algorithm: coseAlgEdDSA, CreatedAt: at.Add(time.Second)}
	c := PasskeyCredential{ID: []byte{6}, UserID: "u2", PublicKey: []byte{7}, Algorithm: coseAlgRS256, CreatedAt: at}
	for _, p := range []PasskeyCredential{a, b, c} {
		if err := s.AddPasskey(ctx, p, 20); err != nil {
			t.Fatalf("add %x: %v", p.ID, err)
		}
	}
	if err := s.AddPasskey(ctx, PasskeyCredential{ID: a.ID, UserID: "u2", PublicKey: []byte{1}, CreatedAt: at}, 20); !errors.Is(err, ErrPasskeyExists) {
		t.Fatalf("duplicate id: %v", err)
	}
	got, err := s.GetPasskey(ctx, a.ID)
	if err != nil || got.UserID != "u1" || got.SignCount != 4 || !got.BackupEligible || got.Name != "Laptop" ||
		!got.CreatedAt.Equal(at) || !got.LastUsedAt.IsZero() || string(got.PublicKey) != string(a.PublicKey) {
		t.Fatalf("get: %+v %v", got, err)
	}
	if _, err := s.GetPasskey(ctx, []byte{0xee}); !errors.Is(err, ErrPasskeyNotFound) {
		t.Fatalf("get missing: %v", err)
	}
	list, err := s.ListPasskeys(ctx, "u1")
	if err != nil || len(list) != 2 || string(list[0].ID) != string(a.ID) || string(list[1].ID) != string(b.ID) {
		t.Fatalf("list: %+v %v", list, err)
	}
	if ok, err := s.RecordPasskeyUse(ctx, a.ID, 3, 9, true, at); ok || err != nil {
		t.Fatalf("stale CAS wrote: %v %v", ok, err)
	}
	if ok, err := s.RecordPasskeyUse(ctx, a.ID, 4, 9, true, at.Add(time.Hour)); !ok || err != nil {
		t.Fatalf("CAS: %v %v", ok, err)
	}
	if got, _ := s.GetPasskey(ctx, a.ID); got.SignCount != 9 || !got.BackedUp || !got.LastUsedAt.Equal(at.Add(time.Hour)) {
		t.Fatalf("after use: %+v", got)
	}
	if ok, _ := s.DeletePasskey(ctx, "u2", a.ID); ok {
		t.Fatal("u2 deleted u1's passkey")
	}
	if ok, err := s.DeletePasskey(ctx, "u1", a.ID); !ok || err != nil {
		t.Fatalf("delete: %v %v", ok, err)
	}
	if n, err := s.DeletePasskeysForUser(ctx, "u1"); n != 1 || err != nil {
		t.Fatalf("delete for user: %d %v", n, err)
	}
	if list, _ := s.ListPasskeys(ctx, "u2"); len(list) != 1 {
		t.Fatalf("u2 lost passkeys: %+v", list)
	}
}

func TestMemoryPasskeyStoreContract(t *testing.T) {
	passkeyStoreContract(t, NewMemoryPasskeyStore())
}

func sqlPasskeyStore(t *testing.T, db *sql.DB) *SQLPasskeyStore {
	t.Helper()
	s, err := NewSQLPasskeyStore(db)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 { // idempotent
		if err := s.EnsureSchema(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestSQLitePasskeyStoreContract(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	passkeyStoreContract(t, sqlPasskeyStore(t, db))
}

func TestPostgresPasskeyStoreContract(t *testing.T) {
	passkeyStoreContract(t, sqlPasskeyStore(t, openPGForBattery(t)))
}

func TestSQLPasskeyStoreRefusesBadTable(t *testing.T) {
	if _, err := NewSQLPasskeyStore(nil, `x"; DROP TABLE y; --`); err == nil {
		t.Fatal("unsafe table name accepted")
	}
}
