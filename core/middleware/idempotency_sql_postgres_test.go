package middleware

import (
	"context"
	"database/sql"
	"errors"
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
// binds the key through pgKey. The principal column is stored raw, and
// a principal holding NUL is refused there.

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
}

// TestSQLIdempotencyPostgres_NULPrincipalRefused: the principal column
// used to hold a NUL-bearing principal pgKey-escaped, so "bad<NUL>id"
// was stored as `bad\0id`, the literal id of another user, and erasing
// that user deleted the first one's cached responses too. Begin now
// refuses it before any write; a literal `bad\0id` principal is stored
// untouched.
func TestSQLIdempotencyPostgres_NULPrincipalRefused(t *testing.T) {
	db, table := pgIdemTable(t)
	s, err := NewSQLIdempotencyStore(db, WithSQLIdempotencyTable(table))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx := context.Background()
	if _, ok, err := s.Begin(ctx, idemStoreKey("bad\x00id", "", "", "k"), "fp"); !errors.Is(err, errPrincipalNUL) || ok {
		t.Fatalf("Begin with a NUL-bearing principal: ok=%v err=%v, want errPrincipalNUL", ok, err)
	}
	var n int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("a refused Begin wrote %d row(s)", n)
	}

	if _, ok, err := s.Begin(ctx, idemStoreKey(`bad\0id`, "", "", "k"), "fp"); err != nil || ok {
		t.Fatalf("Begin with the literal principal: ok=%v err=%v", ok, err)
	}
	var principal string
	if err := db.QueryRow("SELECT principal FROM " + table).Scan(&principal); err != nil {
		t.Fatalf("read principal: %v", err)
	}
	if principal != `bad\0id` {
		t.Fatalf("principal column = %q, want the literal principal untouched", principal)
	}
}

// TestSQLIdempotencyPostgres_NULPrincipalFailsClosed: through the
// middleware, the refusal is a store error, so the keyed write answers
// 503 and the handler never runs (FailOpen is off by default).
func TestSQLIdempotencyPostgres_NULPrincipalFailsClosed(t *testing.T) {
	db, table := pgIdemTable(t)
	s, err := NewSQLIdempotencyStore(db, WithSQLIdempotencyTable(table))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	calls := 0
	h := Idempotency(IdempotencyConfig{Store: s, Principal: func(*http.Request) string { return "bad\x00id" }})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusCreated)
	}))
	req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(`{"n":1}`))
	req.Header.Set(IdempotencyKeyHeader, "pg-nul")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || calls != 0 {
		t.Fatalf("NUL principal keyed write: status %d, handler ran %d time(s); want 503 and 0", rec.Code, calls)
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
}

// TestColumnPrincipalNUL needs no database: on postgres a principal
// with NUL is refused and every other one is stored raw; SQLite keeps
// any principal raw. A Begin on the refusal touches nothing: the store
// below has no DB, so a reap or claim would panic.
func TestColumnPrincipalNUL(t *testing.T) {
	pg := &SQLIdempotencyStore{dialect: "postgres"}
	for _, p := range []string{`CORP\alice`, `bad\0id`, "", "alice"} {
		if got, err := pg.columnPrincipal(idemStoreKey(p, "", "", "k")); err != nil || got != p {
			t.Fatalf("postgres principal %q: column %q err %v, want it raw", p, got, err)
		}
	}
	if _, err := pg.columnPrincipal(idemStoreKey("bad\x00id", "", "", "k")); !errors.Is(err, errPrincipalNUL) {
		t.Fatalf("postgres NUL principal: err %v, want errPrincipalNUL", err)
	}
	lite := &SQLIdempotencyStore{dialect: "sqlite"}
	if got, err := lite.columnPrincipal(idemStoreKey("bad\x00id", "", "", "k")); err != nil || got != "bad\x00id" {
		t.Fatalf("sqlite NUL principal: column %q err %v, want it raw", got, err)
	}
	if _, _, err := pg.Begin(context.Background(), idemStoreKey("bad\x00id", "", "", "k"), "fp"); !errors.Is(err, errPrincipalNUL) {
		t.Fatalf("Begin with a NUL principal: err %v, want errPrincipalNUL before any query", err)
	}
}
