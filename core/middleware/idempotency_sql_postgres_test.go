package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// SQLIdempotencyStore on Postgres. Two defects kept it from working at
// all there although its doc said "sqlite and postgres": the DDL
// declared `body BLOB`, a type Postgres does not have, so the store
// failed at construction; and the middleware's store key joins its
// parts with NUL bytes, which Postgres TEXT refuses, so every keyed
// write would have answered 503. The store now declares BYTEA there and
// binds the key through pgKey.

// pgIdemTable returns a Postgres connection scoped to a fresh schema
// (pgtest: TEST_POSTGRES_DSN, skipped locally when unset) and a table
// name for the store.
func pgIdemTable(t *testing.T) (*sql.DB, string) {
	t.Helper()
	return pgtest.DB(t), "idempotency_keys"
}

func TestSQLIdempotencyPostgres_CreatesTable(t *testing.T) {
	db, table := pgIdemTable(t)
	if _, err := NewSQLIdempotencyStore(db, WithSQLIdempotencyTable(table)); err != nil {
		t.Fatalf("NewSQLIdempotencyStore on Postgres: %v", err)
	}
	var typ string
	if err := db.QueryRow(`SELECT data_type FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 AND column_name = 'body'`, table).Scan(&typ); err != nil {
		t.Fatalf("read body column type: %v", err)
	}
	if typ != "bytea" {
		t.Fatalf("body column type = %q, want bytea", typ)
	}
}

// TestSQLIdempotencyPostgres_StoreKeyRoundTrip drives the store with the
// exact keys the middleware builds, NUL separators and all, and checks
// that two keys the encoding could fold together stay apart.
func TestSQLIdempotencyPostgres_StoreKeyRoundTrip(t *testing.T) {
	db, table := pgIdemTable(t)
	s, err := NewSQLIdempotencyStore(db, WithSQLIdempotencyTable(table))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx := context.Background()
	key := idemStoreKey(`CORP\alice`, "t1", "", "order-1")
	if _, ok, err := s.Begin(ctx, key, "fp"); err != nil || ok {
		t.Fatalf("Begin with the middleware's store key: ok=%v err=%v", ok, err)
	}
	if err := s.Finish(ctx, key, "fp", &IdempotentResponse{Status: 201, Header: http.Header{"X-A": {"1"}}, Body: []byte("ok\x00bin")}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	replay, ok, err := s.Begin(ctx, key, "fp")
	if err != nil || !ok || replay == nil || string(replay.Body) != "ok\x00bin" || replay.Status != 201 {
		t.Fatalf("replay: ok=%v err=%v replay=%+v", ok, err, replay)
	}
	// The principal column holds the raw principal, so App.EraseUserData,
	// which binds the raw user id, still reaches the row.
	var principal string
	if err := db.QueryRow("SELECT principal FROM " + table).Scan(&principal); err != nil {
		t.Fatalf("read principal: %v", err)
	}
	if principal != `CORP\alice` {
		t.Fatalf("principal column = %q, want the raw principal", principal)
	}

	// "a<NUL>b" and the literal `a\0b` must not share a row: the second
	// is a fresh claim, not a fingerprint mismatch against the first.
	if _, _, err := s.Begin(ctx, "a\x00b", "fp-1"); err != nil {
		t.Fatalf("Begin NUL key: %v", err)
	}
	if _, ok, err := s.Begin(ctx, `a\0b`, "fp-2"); err != nil || ok {
		t.Fatalf("Begin of a key the NUL one encodes next to: ok=%v err=%v (the encoding folded two keys onto one row)", ok, err)
	}
	// Release works on the encoded key too.
	if err := s.Finish(ctx, "a\x00b", "fp-1", nil); err != nil {
		t.Fatalf("Finish(nil): %v", err)
	}
	if _, ok, err := s.Begin(ctx, "a\x00b", "fp-3"); err != nil || ok {
		t.Fatalf("Begin after release: ok=%v err=%v, want a fresh claim", ok, err)
	}

	// A principal holding NUL still stores.
	if _, _, err := s.Begin(ctx, idemStoreKey("bad\x00id", "", "", "k"), "fp"); err != nil {
		t.Fatalf("Begin with a NUL-bearing principal: %v", err)
	}
}

// TestSQLIdempotencyPostgres_Middleware replays a keyed write end to end.
func TestSQLIdempotencyPostgres_Middleware(t *testing.T) {
	db, table := pgIdemTable(t)
	s, err := NewSQLIdempotencyStore(db, WithSQLIdempotencyTable(table))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	calls := 0
	h := Idempotency(IdempotencyConfig{Store: s, Principal: testPrincipal})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("made"))
	}))
	for i := range 2 {
		req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(`{"n":1}`))
		req.Header.Set(IdempotencyKeyHeader, "pg-key")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("request %d: status %d body %q", i, rec.Code, rec.Body.String())
		}
	}
	if calls != 1 {
		t.Fatalf("handler ran %d times, want 1 (the retry replays)", calls)
	}
}

// TestPgKeyInjective needs no database: the encoding leaves no NUL,
// never maps two keys to one, and SQLite keeps its keys unchanged so
// rows an earlier release wrote still match.
func TestPgKeyInjective(t *testing.T) {
	keys := []string{"", "a", "a\x00b", `a\0b`, `a\\0b`, "a\\\x00b", `\`, "\x00", `\\`, "\\0"}
	seen := map[string]string{}
	for _, k := range keys {
		enc := pgKey(k)
		if strings.IndexByte(enc, 0) >= 0 {
			t.Fatalf("pgKey(%q) = %q still holds NUL", k, enc)
		}
		if prev, dup := seen[enc]; dup {
			t.Fatalf("pgKey(%q) == pgKey(%q) == %q", k, prev, enc)
		}
		seen[enc] = k
	}
	lite := &SQLIdempotencyStore{dialect: "sqlite"}
	if k := idemStoreKey("p", "", "", "k"); lite.columnKey(k) != k {
		t.Fatalf("sqlite columnKey changed the key: %q", lite.columnKey(k))
	}
	pg := &SQLIdempotencyStore{dialect: "postgres"}
	if got := pg.columnPrincipal(idemStoreKey(`CORP\alice`, "", "", "k")); got != `CORP\alice` {
		t.Fatalf("postgres principal column = %q, want the raw principal", got)
	}
}
