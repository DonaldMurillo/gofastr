package embed

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// burnStoreContract is the behaviour every BurnStore must have. Both
// implementations run it, so the SQL store cannot quietly diverge from the
// memory store the rest of the tests use.
func burnStoreContract(t *testing.T, name string, mk func(t *testing.T) BurnStore) {
	ctx := context.Background()

	t.Run(name+"/first caller wins", func(t *testing.T) {
		s := mk(t)
		got, replay, err := s.Burn(ctx, "n1", "grant-1", time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("Burn: %v", err)
		}
		if replay || got != "grant-1" {
			t.Fatalf("first burn: got=%q replay=%v", got, replay)
		}
	})

	t.Run(name+"/replay returns the original grant", func(t *testing.T) {
		s := mk(t)
		if _, _, err := s.Burn(ctx, "n1", "grant-1", time.Now().Add(time.Hour)); err != nil {
			t.Fatalf("Burn: %v", err)
		}
		got, replay, err := s.Burn(ctx, "n1", "grant-2", time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("Burn: %v", err)
		}
		if !replay {
			t.Error("second burn of the same nonce did not report a replay")
		}
		if got != "grant-1" {
			t.Errorf("replay returned %q — a second grant was issued for one nonce", got)
		}
	})

	t.Run(name+"/spent past the window", func(t *testing.T) {
		s := mk(t)
		if _, _, err := s.Burn(ctx, "n1", "grant-1", time.Now().Add(-time.Second)); err != nil {
			t.Fatalf("Burn: %v", err)
		}
		got, replay, err := s.Burn(ctx, "n1", "grant-2", time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("Burn: %v", err)
		}
		if !replay || got != "" {
			t.Errorf("an expired burn must be spent-and-closed, got=%q replay=%v", got, replay)
		}
	})

	t.Run(name+"/distinct nonces are independent", func(t *testing.T) {
		s := mk(t)
		for i := 0; i < 5; i++ {
			id := fmt.Sprintf("n%d", i)
			got, replay, err := s.Burn(ctx, id, "grant-"+id, time.Now().Add(time.Hour))
			if err != nil || replay || got != "grant-"+id {
				t.Fatalf("%s: got=%q replay=%v err=%v", id, got, replay, err)
			}
		}
	})

	// Contention: many callers claim ONE nonce at once and exactly one may
	// win. Against the SQL store this exercises the unique constraint, which
	// is the mechanism that also holds across replicas — the property a
	// single-process race detector cannot see.
	t.Run(name+"/one winner under contention", func(t *testing.T) {
		s := mk(t)
		const racers = 32
		wins := make([]bool, racers)
		grants := make([]string, racers)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < racers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				got, replay, err := s.Burn(ctx, "contended", fmt.Sprintf("grant-%d", i), time.Now().Add(time.Hour))
				if err != nil {
					t.Errorf("racer %d: %v", i, err)
					return
				}
				wins[i] = !replay
				grants[i] = got
			}(i)
		}
		close(start)
		wg.Wait()

		winners := 0
		for _, w := range wins {
			if w {
				winners++
			}
		}
		if winners != 1 {
			t.Fatalf("%d racers produced %d winners, want exactly 1 — the claim is not atomic", racers, winners)
		}
		distinct := map[string]struct{}{}
		for _, g := range grants {
			distinct[g] = struct{}{}
		}
		if len(distinct) != 1 {
			t.Fatalf("racers received %d distinct grants, want 1", len(distinct))
		}
	})
}

func TestMemoryBurnStoreContract(t *testing.T) {
	burnStoreContract(t, "memory", func(t *testing.T) BurnStore { return NewMemoryBurnStore() })
}

func TestSQLBurnStoreContract(t *testing.T) {
	burnStoreContract(t, "sqlite", func(t *testing.T) BurnStore {
		t.Helper()
		return newSQLiteBurnStore(t)
	})
}

func newSQLiteBurnStore(t *testing.T) *SQLBurnStore {
	t.Helper()
	// A file-backed DB in a temp dir, not ":memory:": an in-memory sqlite DSN
	// gives every pooled connection its OWN empty database, so the contention
	// subtest would silently exercise 32 separate tables.
	dsn := "file:" + t.TempDir() + "/burn.db?_busy_timeout=5000"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s, err := NewSQLBurnStore(db)
	if err != nil {
		t.Fatalf("NewSQLBurnStore: %v", err)
	}
	return s
}

func TestSQLBurnStorePrunes(t *testing.T) {
	s := newSQLiteBurnStore(t)
	ctx := context.Background()
	now := time.Now()
	if _, _, err := s.Burn(ctx, "live", "g1", now.Add(time.Hour)); err != nil {
		t.Fatalf("Burn: %v", err)
	}
	if _, _, err := s.Burn(ctx, "dead", "g2", now.Add(-time.Hour)); err != nil {
		t.Fatalf("Burn: %v", err)
	}
	if err := s.Prune(ctx, now); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if _, replay, _ := s.Burn(ctx, "live", "g3", now.Add(time.Hour)); !replay {
		t.Error("Prune removed a live burn — the nonce became reusable")
	}
	if _, replay, _ := s.Burn(ctx, "dead", "g4", now.Add(time.Hour)); replay {
		t.Error("Prune left an expired burn behind")
	}
}

func TestSQLBurnStoreRejectsBadTable(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:"+t.TempDir()+"/burn.db")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := NewSQLBurnStore(db, WithBurnTable("bad name; DROP TABLE users")); err == nil {
		t.Error("an unsafe table identifier was accepted")
	}
	if _, err := NewSQLBurnStore(nil); err == nil {
		t.Error("a nil database was accepted")
	}
}

// The exchange path end-to-end against the durable store: what an app running
// more than one replica actually gets.
func TestExchangeAgainstSQLBurnStore(t *testing.T) {
	store := newSQLiteBurnStore(t)
	h := testHost(t, func(c *Config) { c.BurnStore = store })
	ctx := context.Background()
	tok, err := h.MintNonce(context.Background(), "dashboard", "u-1", "https://acme.com", nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	first, err := h.Exchange(ctx, tok, "https://acme.com")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	second, err := h.Exchange(ctx, tok, "https://acme.com")
	if err != nil {
		t.Fatalf("second Exchange: %v", err)
	}
	if !second.Replay || second.Grant != first.Grant {
		t.Fatalf("durable store did not make the exchange idempotent: %+v vs %+v", second, first)
	}
}

// Verification happens before the burn and is not atomic with it. A request
// that verified a nonce and then stalled past its retention deadline must not
// mint a second grant when it finally lands — and the row it writes must still
// be a tombstone, or the nonce is simply unburnt and the next caller takes it.
func TestStalledClaimGetsNoGrantButStillBurns(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		mk   func(t *testing.T) BurnStore
	}{
		{"memory", func(*testing.T) BurnStore { return NewMemoryBurnStore() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.mk(t)

			// The stalled request: deadline already passed by the time it lands.
			got, replay, err := s.Burn(ctx, "n1", "grant-late", time.Now().Add(-time.Second))
			if err != nil {
				t.Fatalf("Burn: %v", err)
			}
			if got != "" || !replay {
				t.Fatalf("a claim landing past its deadline minted %q (replay=%v) — "+
					"one nonce produced a second grant", got, replay)
			}

			// And the nonce is still burnt: a later caller gets nothing.
			got2, _, err := s.Burn(ctx, "n1", "grant-next", time.Now().Add(time.Hour))
			if err != nil {
				t.Fatalf("Burn: %v", err)
			}
			if got2 != "" {
				t.Errorf("the nonce was left unburnt — a later caller minted %q", got2)
			}
		})
	}
}
