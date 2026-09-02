package search

import (
	"database/sql"
	"testing"

	_ "github.com/lib/pq"
)

// Property: everything NewPostgres interpolates into SQL later (the
// table identifier, the tsvector regconfig name, the weight letters)
// is bounded at construction, so a hostile value can never reach
// to_tsvector/to_tsquery SQL even though the query TEXT itself is
// parameterized. This pins the construction-time gate; the query-text
// sanitizer is pinned by tsquery_test.go and TestPostgresAdversarialQuery.
//
// Surface: NewPostgres's config validation (language via langRe, table
// via core/query.SafeIdent, weights via the 'A'..'D' range check).
// The db handle is never touched during validation, so a lazily-opened
// *sql.DB suffices — no live Postgres needed.
func newLazyPG(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("postgres", "host=127.0.0.1 port=1 dbname=unused connect_timeout=1 sslmode=disable")
	if err != nil {
		t.Fatalf("open lazy db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestPostgresHostileConfigRejected(t *testing.T) {
	db := newLazyPG(t)

	hostileLangs := []string{
		// break out of the regconfig cast / splice SQL
		"english'::regconfig; --",
		`english"; DROP TABLE search_documents; --`,
		"english; SELECT 1",
		"english --",
		// case tricks and unicode homoglyphs that survive visual review
		"English",
		"еnglish", // Cyrillic е
		// structural junk
		"e n g", "*simple", "simple*", "a.b",
	}
	for _, lang := range hostileLangs {
		if _, err := NewPostgres(db, PostgresConfig{Language: lang}); err == nil {
			t.Errorf("SECURITY: [search] NewPostgres accepted hostile Language %q: it flows into to_tsvector/to_tsquery SQL, it must be rejected by the ^[a-z_]+$ allowlist", lang)
		}
	}

	// (Dotted names are deliberately NOT here: SafeIdent allows
	// schema.table by contract, and QuoteIdent renders them inert.)
	hostileTables := []string{
		`docs; DROP TABLE users; --`,
		`"quoted"`,
		"doc tbl",
	}
	for _, table := range hostileTables {
		if _, err := NewPostgres(db, PostgresConfig{Table: table}); err == nil {
			t.Errorf("SECURITY: [search] NewPostgres accepted hostile Table %q: the identifier is quoted into every statement, it must be rejected by SafeIdent", table)
		}
	}

	badWeights := []struct {
		field string
		w     byte
	}{
		{"title", 'a'}, // lowercase: outside 'A'..'D'
		{"title", 'E'}, // past the range
	}
	for _, bw := range badWeights {
		if _, err := NewPostgres(db, PostgresConfig{WeightedFields: map[string]byte{bw.field: bw.w}}); err == nil {
			t.Errorf("SECURITY: [search] NewPostgres accepted weight %q for field %q: weights are interpolated into setweight(), only 'A'..'D' may pass", string(bw.w), bw.field)
		}
	}

	if _, err := NewPostgres(nil, PostgresConfig{}); err == nil {
		t.Errorf("SECURITY: [search] NewPostgres(nil) must be rejected")
	}

	// Control: the plain allowlisted shapes still construct.
	for _, lang := range []string{"english", "simple", "german", "pg_catalog_english"} {
		if _, err := NewPostgres(db, PostgresConfig{Language: lang}); err != nil {
			t.Errorf("NewPostgres rejected legitimate language %q: %v", lang, err)
		}
	}
	if _, err := NewPostgres(db, PostgresConfig{WeightedFields: map[string]byte{"title": 'B'}}); err != nil {
		t.Errorf("NewPostgres rejected legitimate weight 'B': %v", err)
	}
}
