package embed

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

func TestSweepEmbedSurfaceNameIsBounded(t *testing.T) {
	mk := func(name string) error {
		_, err := New(Config{
			Surfaces:  []Surface{{Name: name, Screen: testScreen{"/d"}, Origins: []string{"https://acme.com"}}},
			BurnStore: NewMemoryBurnStore(),
		})
		return err
	}
	if err := mk(strings.Repeat("a", 64)); err != nil {
		t.Fatalf("a 64-byte name: %v", err)
	}
	if err := mk(strings.Repeat("a", 65)); err == nil {
		t.Fatal("a 65-byte surface name booted")
	}
}

type countingStore struct {
	BurnStore
	burns atomic.Int32
}

func (s *countingStore) Burn(ctx context.Context, id, grant string, expires time.Time) (string, bool, error) {
	s.burns.Add(1)
	return s.BurnStore.Burn(ctx, id, grant, expires)
}

func TestSweepEmbedUnverifiedNonceNeverReachesTheStore(t *testing.T) {
	store := &countingStore{BurnStore: NewMemoryBurnStore()}
	h := testHost(t, func(c *Config) { c.BurnStore = store })
	forged, err := MintNonce([]byte("not-the-host-key-not-the-host-k"), "dashboard", "u-1", "https://acme.com", nil, time.Minute, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{"", "garbage", NoncePrefix + "e30.AAAA", forged} {
		if _, err := h.Exchange(context.Background(), tok, "https://acme.com"); err == nil {
			t.Fatalf("Exchange(%q) succeeded", tok)
		}
	}
	if n := store.burns.Load(); n != 0 {
		t.Fatalf("%d unverified nonce(s) reached the burn store", n)
	}
}

func TestSweepEmbedMiddlewareWithoutKeysAnswers503(t *testing.T) {
	h, err := New(Config{
		Surfaces:  []Surface{{Name: "dashboard", Screen: testScreen{"/embed/dashboard"}, Origins: []string{"https://acme.com"}}},
		BurnStore: NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	p := &probe{}
	req := httptest.NewRequest(http.MethodGet, "/embed/dashboard", nil)
	req.Header.Set(GrantHeader, "emg_anything.x")
	rec := httptest.NewRecorder()
	h.Middleware()(p.handler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || p.reached {
		t.Fatalf("keyless host answered %d (reached=%v), want 503", rec.Code, p.reached)
	}
}

func TestSweepEmbedWithGrantHoldsOnlyItsScopes(t *testing.T) {
	held, ok := access.HeldScopes(WithGrant(context.Background(), Grant{Surface: "s", Origin: "https://a.com", Scopes: []string{"read"}}))
	if !ok || len(held) != 1 || string(held[0]) != "read" {
		t.Fatalf("held scopes = %v, %v; want [read]", held, ok)
	}
	if _, ok := access.HeldScopes(WithGrant(context.Background(), Grant{Surface: "s", Origin: "https://a.com"})); !ok {
		t.Fatal("a grant with no scopes left the request unrestricted")
	}
}

func eachBurnStore(t *testing.T, fn func(t *testing.T, s BurnStore)) {
	for _, c := range []struct {
		name string
		mk   func(*testing.T) BurnStore
	}{
		{"memory", func(*testing.T) BurnStore { return NewMemoryBurnStore() }},
		{"sqlite", func(t *testing.T) BurnStore { return newSQLiteBurnStore(t) }},
	} {
		t.Run(c.name, func(t *testing.T) { fn(t, c.mk(t)) })
	}
}

func TestSweepEmbedIdenticalGrantReplayIsAReplay(t *testing.T) {
	eachBurnStore(t, func(t *testing.T, s BurnStore) {
		ctx := context.Background()
		exp := time.Now().Add(time.Hour)
		if _, replay, err := s.Burn(ctx, "n1", "same", exp); err != nil || replay {
			t.Fatalf("first burn: replay=%v err=%v", replay, err)
		}
		got, replay, err := s.Burn(ctx, "n1", "same", exp)
		if err != nil || !replay || got != "same" {
			t.Fatalf("second burn of a byte-identical grant = %q, replay=%v, err=%v; want the stored grant as a replay", got, replay, err)
		}
	})
}

// vanishingExec runs the claim, then deletes the row and hides RowsAffected.
type vanishingExec struct {
	*sql.DB
	table  string
	vanish bool
}

type noRowsAffected struct{ sql.Result }

func (noRowsAffected) RowsAffected() (int64, error) { return 0, errors.New("unsupported") }

func (e *vanishingExec) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	res, err := e.DB.ExecContext(ctx, q, args...)
	if err != nil || !strings.HasPrefix(q, "INSERT") {
		return res, err
	}
	if e.vanish {
		if _, err := e.DB.ExecContext(ctx, `DELETE FROM `+e.table); err != nil {
			return nil, err
		}
	}
	return noRowsAffected{res}, nil
}

func TestSweepEmbedSQLBurnWithoutRowsAffected(t *testing.T) {
	base := newSQLiteBurnStore(t)
	raw := base.db.(*sql.DB)
	ctx := context.Background()

	keep := &SQLBurnStore{db: &vanishingExec{DB: raw, table: base.table}, table: base.table}
	got, _, err := keep.Burn(ctx, "n1", "grant-1", time.Now().Add(time.Hour))
	if err != nil || got != "grant-1" {
		t.Fatalf("claim on a driver without RowsAffected = %q, %v; want the stored grant", got, err)
	}
	if got, replay, _ := keep.Burn(ctx, "n1", "grant-2", time.Now().Add(time.Hour)); got != "grant-1" || !replay {
		t.Fatalf("replay = %q, %v; want grant-1 as a replay", got, replay)
	}

	gone := &SQLBurnStore{db: &vanishingExec{DB: raw, table: base.table, vanish: true}, table: base.table}
	got, replay, err := gone.Burn(ctx, "n2", "grant-3", time.Now().Add(time.Hour))
	if err != nil || got != "" || !replay {
		t.Fatalf("burn whose row vanished = %q, replay=%v, err=%v; want spent", got, replay, err)
	}
}
