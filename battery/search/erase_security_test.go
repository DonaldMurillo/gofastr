package search

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// indexOwned indexes one document per owner (the empty owner names
// nobody) under the same searchable word. v1 is indexed twice, so the
// re-index has to move its owner.
func indexOwned(t *testing.T, b Backend) {
	t.Helper()
	for _, d := range []Document{
		{ID: "v1", Type: "note", Text: "diary draft", Owner: "earlier"},
		{ID: "v1", Type: "note", Text: "diary entry", Owner: "victim"},
		{ID: "b1", Type: "note", Text: "diary entry", Owner: "bystander"},
		{ID: "n1", Type: "page", Text: "diary help page"},
	} {
		if err := b.Index(context.Background(), d); err != nil {
			t.Fatalf("Index %s: %v", d.ID, err)
		}
	}
}

// searchOwners runs q and returns "id:owner" for each hit, sorted.
func searchOwners(t *testing.T, b Backend, text string) []string {
	t.Helper()
	res, err := b.Search(context.Background(), Query{Text: text})
	if err != nil {
		t.Fatalf("Search %q: %v", text, err)
	}
	var got []string
	for _, r := range res {
		got = append(got, r.Document.ID+":"+r.Document.Owner)
	}
	slices.Sort(got)
	return got
}

// eraseVictim runs App.EraseUserData for "victim" over db and checks
// the index kept only the other documents, whole words and prefixes.
func eraseVictim(t *testing.T, db *sql.DB, b Backend) {
	t.Helper()
	want := []string{"b1:bystander", "n1:", "v1:victim"}
	if got := searchOwners(t, b, "diary"); !slices.Equal(got, want) {
		t.Fatalf("before erase: hits %v, want %v", got, want)
	}
	if _, err := framework.NewApp(framework.WithDB(db)).EraseUserData(context.Background(), "victim"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	want = []string{"b1:bystander", "n1:"}
	for _, q := range []string{"diary", "dia"} {
		if got := searchOwners(t, b, q); !slices.Equal(got, want) {
			t.Errorf("after erase, %q hits %v, want %v", q, got, want)
		}
	}
}

// Erasing a user removes the documents indexed for them and keeps
// everyone else's (S23). On SQLite the prefix companion loses the
// victim's rows too, so no token of the erased text survives there.
func TestEraseRemovesSQLiteDocuments(t *testing.T) {
	db := openSQLiteDB(t)
	if err := sqliteFTS5Available(db); err != nil {
		t.Skipf("FTS5 unavailable: %v", err)
	}
	s, err := NewSQLiteFTS(db, SQLiteFTSConfig{Table: "notes_idx"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	indexOwned(t, s)
	eraseVictim(t, db, s)
	var main, prefix int
	if err := db.QueryRow(`SELECT count(*) FROM notes_idx`).Scan(&main); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM notes_idx_prefix`).Scan(&prefix); err != nil {
		t.Fatal(err)
	}
	if main != 2 || prefix != 2 {
		t.Errorf("rows after erase: main %d, prefix %d; want 2 and 2", main, prefix)
	}
}

// The Postgres twin of TestEraseRemovesSQLiteDocuments.
func TestEraseRemovesPostgresDocuments(t *testing.T) {
	db := openSearchPG(t)
	p, err := NewPostgres(db, PostgresConfig{Table: "notes_idx"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	indexOwned(t, p)
	eraseVictim(t, db, p)
}

// A SQLite index an earlier release created gains the owner column on
// EnsureSchema and keeps its documents, prefixes included.
func TestSQLiteIndexAddsOwnerColumn(t *testing.T) {
	db := openSQLiteDB(t)
	if err := sqliteFTS5Available(db); err != nil {
		t.Skipf("FTS5 unavailable: %v", err)
	}
	for _, stmt := range []string{
		`CREATE VIRTUAL TABLE notes_idx USING fts5(id UNINDEXED, type UNINDEXED, fields UNINDEXED, text, tokenize='porter unicode61')`,
		`CREATE VIRTUAL TABLE notes_idx_prefix USING fts5(text, content='', contentless_delete=1, tokenize='unicode61 remove_diacritics 0')`,
		`INSERT INTO notes_idx (id, type, fields, text) VALUES ('old', 'note', '{}', 'diary from before')`,
		`INSERT INTO notes_idx_prefix (rowid, text) SELECT rowid, text FROM notes_idx`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	s, err := NewSQLiteFTS(db, SQLiteFTSConfig{Table: "notes_idx"})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.EnsureSchema(context.Background()); err != nil {
			t.Fatalf("EnsureSchema over an old index: %v", err)
		}
	}
	if err := s.Index(context.Background(), Document{ID: "new", Type: "note", Text: "diary today", Owner: "victim"}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"diary", "dia"} {
		if got, want := searchOwners(t, s, q), []string{"new:victim", "old:"}; !slices.Equal(got, want) {
			t.Errorf("%q hits %v, want %v", q, got, want)
		}
	}
}

// The Postgres twin of TestSQLiteIndexAddsOwnerColumn.
func TestPostgresIndexAddsOwnerColumn(t *testing.T) {
	db := openSearchPG(t)
	if _, err := db.Exec(`CREATE TABLE notes_idx (id TEXT PRIMARY KEY, type TEXT NOT NULL, text TEXT NOT NULL,
		fields JSONB, tsv TSVECTOR NOT NULL, tsv_prefix TSVECTOR)`); err != nil {
		t.Fatal(err)
	}
	p, err := NewPostgres(db, PostgresConfig{Table: "notes_idx"})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := p.EnsureSchema(context.Background()); err != nil {
			t.Fatalf("EnsureSchema over an old table: %v", err)
		}
	}
	if err := p.Index(context.Background(), Document{ID: "new", Type: "note", Text: "diary today", Owner: "victim"}); err != nil {
		t.Fatal(err)
	}
	if got, want := searchOwners(t, p, "diary"), []string{"new:victim"}; !slices.Equal(got, want) {
		t.Errorf("hits %v, want %v", got, want)
	}
}
