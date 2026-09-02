package auth

// EntityUserStore concurrency and fail-closed edges.
//
// Property 1: the unique-email identity holds under concurrent creation —
// N racing CreateUser calls for one address yield exactly ONE account,
// with every loser receiving ErrEmailTaken (never a raw driver error,
// which callers would surface as a 500 and operators as an incident).
//
// Property 2: the password-state setters fail closed for an unknown user
// — ErrUserNotFound, never a silent success and never (true, nil), which
// AccountsPlugin would read as "this user has a password".

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func newEntitySecurityStore(t *testing.T) (*sql.DB, *EntityUserStore) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// ":memory:" is per pool connection (see the oauth-link fixture): pin
	// the pool so every goroutine shares one schema. database/sql then
	// serializes the statements themselves, which is exactly the
	// count-then-insert interleaving the unique constraint must survive.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	store := NewEntityUserStore(db, "users")
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return db, store
}

func TestConcurrentCreateSameEmailOneWinner(t *testing.T) {
	db, store := newEntitySecurityStore(t)
	const N = 16

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		won      int
		takenErr int
		otherErr []error
	)
	start := make(chan struct{})
	wg.Add(N)
	for range N {
		go func() {
			defer wg.Done()
			<-start
			_, err := store.CreateUser(context.Background(), "race@example.com", "hash", []string{"user"})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				won++
			case errors.Is(err, ErrEmailTaken):
				takenErr++
			default:
				otherErr = append(otherErr, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	for _, err := range otherErr {
		t.Errorf("concurrent duplicate create surfaced a raw driver error instead of ErrEmailTaken: %v", err)
	}
	if won != 1 || takenErr != N-1 {
		t.Errorf("email uniqueness under contention: %d winners, %d ErrEmailTaken, want exactly 1 and %d", won, takenErr, N-1)
	}
	var rows int
	if err := db.QueryRow("SELECT COUNT(*) FROM users WHERE email = 'race@example.com'").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("racing creates produced %d rows for one address, want 1", rows)
	}
}

func TestConcurrentCreateDistinctEmailsAllPersist(t *testing.T) {
	db, store := newEntitySecurityStore(t)
	const N = 24

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, N)
	wg.Add(N)
	for i := range N {
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = store.CreateUser(context.Background(),
				"user"+string(rune('a'+i))+"@example.com", "hash", []string{"user"})
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("create %d failed under contention: %v", i, err)
		}
	}
	var rows int
	if err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != N {
		t.Errorf("concurrent creates lost rows: %d persist, want %d", rows, N)
	}
}

// Surfaces: SetPassword and HasPassword on an unknown user. Both must
// answer ErrEmailTaken's user-level sibling ErrUserNotFound — never a
// silent no-op success, and never a positive HasPassword.
func TestPasswordSettersUnknownUserFailClosed(t *testing.T) {
	_, store := newEntitySecurityStore(t)
	ctx := context.Background()

	if err := store.SetPassword(ctx, "ghost", "newhash"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("SetPassword(unknown) = %v, want ErrUserNotFound — a silent success would let a caller believe a credential it cannot see was rotated", err)
	}
	has, err := store.HasPassword(ctx, "ghost")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("HasPassword(unknown) = (%v, %v), want (false, ErrUserNotFound)", has, err)
	}
	if err == nil && has {
		t.Errorf("SECURITY: [entity-haspassword] HasPassword(unknown) = (true, nil) — AccountsPlugin would treat a nonexistent account as password-backed")
	}
}
