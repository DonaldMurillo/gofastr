package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// passkeyCapContract: the per-user cap holds when registrations race.
// The handler counted and then stored, so two registrations with one
// slot left both counted under the cap and both stored.
func passkeyCapContract(t *testing.T, s PasskeyStore, strict bool) {
	t.Helper()
	ctx := context.Background()
	const max = 2
	at := time.Unix(1_700_000_000, 0).UTC()
	cred := func(id string) PasskeyCredential {
		return PasskeyCredential{ID: []byte(id), UserID: "capped", PublicKey: []byte{1}, Algorithm: coseAlgES256, CreatedAt: at}
	}
	if err := s.AddPasskey(ctx, cred("first"), max); err != nil {
		t.Fatalf("first: %v", err)
	}
	const racers = 8
	errs := make([]error, racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range racers {
		wg.Go(func() {
			<-start
			errs[i] = s.AddPasskey(ctx, cred("race-"+strconv.Itoa(i)), max)
		})
	}
	close(start)
	wg.Wait()
	stored := 0
	for _, err := range errs {
		switch {
		case err == nil:
			stored++
		case strict && !errors.Is(err, ErrPasskeyLimit):
			t.Errorf("a refused racer got %v, want ErrPasskeyLimit", err)
		}
	}
	list, err := s.ListPasskeys(ctx, "capped")
	if err != nil {
		t.Fatal(err)
	}
	if stored != 1 || len(list) != max {
		t.Fatalf("%d racers stored and the user holds %d passkeys, want 1 and %d", stored, len(list), max)
	}
	if err := s.AddPasskey(ctx, cred("late"), max); !errors.Is(err, ErrPasskeyLimit) {
		t.Fatalf("add at the cap: %v, want ErrPasskeyLimit", err)
	}
	// The cap is per user.
	other := cred("other")
	other.UserID = "uncapped"
	if err := s.AddPasskey(ctx, other, max); err != nil {
		t.Fatalf("another user's add: %v", err)
	}
}

func TestMemoryPasskeyCapUnderRace(t *testing.T) {
	passkeyCapContract(t, NewMemoryPasskeyStore(), true)
}

// A file database with several connections, so the racers really do
// overlap. SQLite lets one writer through at a time; a racer that lost
// the write lock may answer busy rather than the limit, and none stores.
func TestSQLitePasskeyCapUnderRace(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "pk.db")+"?_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	passkeyCapContract(t, sqlPasskeyStore(t, db), false)
}

// A pool of its own: openPGForBattery pins one connection, where the
// racers would never overlap.
func TestPostgresPasskeyCapUnderRace(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open pg: %v", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(20)
	t.Cleanup(func() { _ = db.Close() })
	// Dial the racers' connections first: on a cold pool the first racer
	// commits while the others are still connecting, and none overlap.
	var conns []*sql.Conn
	for range 10 {
		c, err := db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	for _, c := range conns {
		_ = c.Close()
	}
	table := fmt.Sprintf("pk_cap_%d", time.Now().UnixNano())
	s, err := NewSQLPasskeyStore(db, table)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS " + table) })
	passkeyCapContract(t, s, true)
}
