package search

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// prefixCorpus is two documents whose words stem shorter than they are
// spelled: porter and the english config store "pagination" as "pagin"
// and "running" as "run".
var prefixCorpus = []Document{
	{ID: "1", Text: "pagination helpers"},
	{ID: "2", Text: "the cats are running"},
}

// assertEveryPrefixMatches types each word one letter at a time, the way
// a search-as-you-type box does, and wants its document at every step.
func assertEveryPrefixMatches(t *testing.T, b Backend) {
	t.Helper()
	ctx := context.Background()
	for _, d := range prefixCorpus {
		if err := b.Index(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ word, id string }{{"pagination", "1"}, {"running", "2"}} {
		for n := 3; n <= len(tc.word); n++ {
			q := tc.word[:n]
			res, err := b.Search(ctx, Query{Text: q})
			if err != nil {
				t.Fatal(err)
			}
			if len(res) != 1 || res[0].Document.ID != tc.id {
				t.Errorf("q=%q: got %d hits, want doc %s", q, len(res), tc.id)
			}
		}
	}
}

// TestSQLiteEveryPrefixMatches: FTS5 stems a prefix token before the
// prefix scan, so "paginat" became "paginat*" against the stored
// "pagin" and type-ahead lost the hit between "pagin" and the full word.
func TestSQLiteEveryPrefixMatches(t *testing.T) {
	assertEveryPrefixMatches(t, newSQLiteFTS(t))
}

// TestPostgresEveryPrefixMatches: to_tsquery('english','paginat:*')
// stems to 'paginat':*, which no stored lexeme ('pagin') starts with.
func TestPostgresEveryPrefixMatches(t *testing.T) {
	assertEveryPrefixMatches(t, newSearchIndex(t, nil))
}

// assertPrefixWithOtherTerms: the prefix leg still ANDs the other terms,
// and a hyphenated last term completes on its tail.
func assertPrefixWithOtherTerms(t *testing.T, b Backend) {
	t.Helper()
	ctx := context.Background()
	for _, d := range []Document{
		{ID: "1", Text: "pagination helpers"},
		{ID: "2", Text: "pagination internals"},
		{ID: "3", Text: "the load-balancer restarted"},
	} {
		if err := b.Index(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	for q, want := range map[string]string{"helpers paginat": "1", "load-balance": "3"} {
		res, err := b.Search(ctx, Query{Text: q})
		if err != nil {
			t.Fatal(err)
		}
		if len(res) != 1 || res[0].Document.ID != want {
			t.Errorf("q=%q: got %+v, want only doc %s", q, res, want)
		}
	}
}

func TestSQLitePrefixWithOtherTerms(t *testing.T) {
	assertPrefixWithOtherTerms(t, newSQLiteFTS(t))
}

func TestPostgresPrefixWithOtherTerms(t *testing.T) {
	assertPrefixWithOtherTerms(t, newSearchIndex(t, nil))
}

// assertPrefixPastTermCap: a prefix with more completions than the query
// term cap still finds the word its stem cuts short. runnin000..runnin063
// sort before "running", whose stem "run" the stemmed leg cannot reach.
func assertPrefixPastTermCap(t *testing.T, b Backend) {
	t.Helper()
	ctx := context.Background()
	for i := range maxQueryTerms {
		d := Document{ID: fmt.Sprintf("n%03d", i), Type: "noise", Text: fmt.Sprintf("runnin%03d", i)}
		if err := b.Index(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Index(ctx, Document{ID: "hit", Type: "doc", Text: "the cats are running"}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []Query{{Text: "runnin"}, {Text: "runnin", Type: "doc"}} {
		res, err := b.Search(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, r := range res {
			found = found || r.Document.ID == "hit"
		}
		if !found {
			t.Errorf("q=%q type=%q: %d hits, none of them doc hit", q.Text, q.Type, len(res))
		}
		if q.Type != "" && len(res) != 1 {
			t.Errorf("q=%q type=%q: %d hits, want 1", q.Text, q.Type, len(res))
		}
	}
}

func TestSQLitePrefixPastTermCap(t *testing.T) {
	assertPrefixPastTermCap(t, newSQLiteFTS(t))
}

func TestPostgresPrefixPastTermCap(t *testing.T) {
	assertPrefixPastTermCap(t, newSearchIndex(t, nil))
}

// TestSQLitePrefixIndexFollowsDeletes: re-indexing and deleting a
// document takes its words out of the unstemmed companion index too.
func TestSQLitePrefixIndexFollowsDeletes(t *testing.T) {
	ctx := context.Background()
	idx := newSQLiteFTS(t)
	_ = idx.Index(ctx, Document{ID: "1", Text: "pagination helpers"})
	_ = idx.Index(ctx, Document{ID: "1", Text: "other words"})
	_ = idx.Index(ctx, Document{ID: "2", Text: "paginated list"})
	if err := idx.Delete(ctx, "2"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := idx.db.QueryRow(`SELECT count(*) FROM ` + idx.quotedPrefix + ` WHERE ` + idx.quotedPrefix + ` MATCH '"pag"*'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("companion index kept %d rows after re-index and delete", n)
	}
}

// TestSQLiteDropsOldPrefixVocab: the vocabulary view an earlier release
// built over the companion index goes on the next EnsureSchema.
func TestSQLiteDropsOldPrefixVocab(t *testing.T) {
	ctx := context.Background()
	idx := newSQLiteFTS(t)
	if _, err := idx.db.Exec(`CREATE VIRTUAL TABLE ` + idx.quotedVocab + ` USING fts5vocab(` + idx.quotedPrefix + `, 'row')`); err != nil {
		t.Fatal(err)
	}
	if err := idx.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := idx.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'search_documents_prefix_vocab'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("old vocabulary view survived EnsureSchema")
	}
}

// TestSQLitePrefixBackfillsOldTable: a table created before the
// unstemmed companion existed gets it filled on the next EnsureSchema.
func TestSQLitePrefixBackfillsOldTable(t *testing.T) {
	ctx := context.Background()
	idx := newSQLiteFTS(t)
	if _, err := idx.db.Exec(`DROP TABLE ` + idx.quotedPrefix); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.db.Exec(`INSERT INTO `+idx.quotedTable+` (id, type, fields, text) VALUES ('1', '', '{}', ?)`, "pagination helpers"); err != nil {
		t.Fatal(err)
	}
	if err := idx.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	res, err := idx.Search(ctx, Query{Text: "paginat"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("old row not backfilled: got %d hits", len(res))
	}
}

// TestPostgresPrefixBackfillsOldTable: a table created before tsv_prefix
// existed gains the column, filled, on the next EnsureSchema.
func TestPostgresPrefixBackfillsOldTable(t *testing.T) {
	ctx := context.Background()
	db := openSearchPG(t)
	if _, err := db.Exec(`CREATE TABLE search_documents (
		id TEXT PRIMARY KEY, type TEXT NOT NULL, text TEXT NOT NULL,
		fields JSONB, tsv TSVECTOR NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO search_documents (id, type, text, fields, tsv)
		VALUES ('1', '', 'helpers', '{"title":"pagination"}', to_tsvector('english', 'helpers pagination'))`); err != nil {
		t.Fatal(err)
	}
	idx, err := NewPostgres(db, PostgresConfig{WeightedFields: map[string]byte{"title": 'B'}})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	res, err := idx.Search(ctx, Query{Text: "paginat"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("old row not backfilled: got %d hits", len(res))
	}
}

// The backfill on an upgraded table does not hold the table: another
// replica's search runs between batches.
func TestPostgresBackfillLeavesTableOpen(t *testing.T) {
	ctx := context.Background()
	db := openSearchPG(t)
	if _, err := db.Exec(`CREATE TABLE search_documents (
		id TEXT PRIMARY KEY, type TEXT NOT NULL, text TEXT NOT NULL,
		fields JSONB, tsv TSVECTOR NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"1", "2", "3"} {
		if _, err := db.Exec(`INSERT INTO search_documents (id, type, text, fields, tsv)
			VALUES ($1, '', 'pagination helpers', '{}', to_tsvector('english', 'pagination helpers'))`, id); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := NewPostgres(db, PostgresConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer func(n int) { prefixBackfillBatch = n }(prefixBackfillBatch)
	prefixBackfillBatch = 1
	batches := 0
	afterPrefixBatch = func(int64) {
		batches++
		qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if _, err := idx.Search(qctx, Query{Text: "pagination"}); err != nil {
			t.Errorf("search during the backfill: %v", err)
		}
	}
	defer func() { afterPrefixBatch = nil }()
	if err := idx.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if batches < 3 {
		t.Errorf("backfill ran %d batches, want one per row at batch size 1", batches)
	}
	batches = 0
	if err := idx.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if batches != 0 {
		t.Errorf("a second boot ran %d backfill batches over a filled column, want 0", batches)
	}
	res, err := idx.Search(ctx, Query{Text: "paginat"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 3 {
		t.Fatalf("prefix hits after the backfill = %d, want 3", len(res))
	}
}

// otherReplicaDB opens a second pool on the schema db is scoped to, the
// way another replica reaches the same table.
func otherReplicaDB(t *testing.T, db *sql.DB) *sql.DB {
	t.Helper()
	var schema string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(pgtest.BaseDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("options", "-c search_path="+schema)
	u.RawQuery = q.Encode()
	other, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	return other
}

// Another replica fills a row this backfill's batch had picked: the batch
// fills nothing, and the backfill still fills the rows left before it
// marks the column. After the first batch, the other replica takes the
// row the next batch will pick and commits only once that batch waits on
// it, so the batch's recheck finds the row filled and returns zero.
func TestPostgresBackfillOutlastsLostBatch(t *testing.T) {
	ctx := context.Background()
	db := openSearchPG(t)
	replica := otherReplicaDB(t, db)
	if _, err := db.Exec(`CREATE TABLE search_documents (
		id TEXT PRIMARY KEY, type TEXT NOT NULL, text TEXT NOT NULL,
		fields JSONB, tsv TSVECTOR NOT NULL, tsv_prefix TSVECTOR)`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"1", "2", "3"} {
		if _, err := db.Exec(`INSERT INTO search_documents (id, type, text, fields, tsv)
			VALUES ($1, '', 'pagination helpers', '{}', to_tsvector('english', 'pagination helpers'))`, id); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := NewPostgres(db, PostgresConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer func(n int) { prefixBackfillBatch = n }(prefixBackfillBatch)
	prefixBackfillBatch = 1
	var filled []int64
	var commit sync.WaitGroup
	afterPrefixBatch = func(n int64) {
		filled = append(filled, n)
		if len(filled) != 1 {
			return
		}
		// The row the next batch's subquery will pick, by the same scan.
		var next string
		if err := db.QueryRow(`SELECT id FROM search_documents WHERE tsv_prefix IS NULL LIMIT 1`).Scan(&next); err != nil {
			t.Error(err)
			return
		}
		other, err := replica.Begin()
		if err != nil {
			t.Error(err)
			return
		}
		if _, err := other.Exec(`UPDATE search_documents SET tsv_prefix = to_tsvector('simple', text) WHERE id = $1`, next); err != nil {
			_ = other.Rollback()
			t.Error(err)
			return
		}
		commit.Go(func() {
			time.Sleep(300 * time.Millisecond)
			_ = other.Commit()
		})
	}
	defer func() { afterPrefixBatch = nil }()
	if err := idx.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	commit.Wait()
	var unfilled int
	if err := db.QueryRow(`SELECT COUNT(*) FROM search_documents WHERE tsv_prefix IS NULL`).Scan(&unfilled); err != nil {
		t.Fatal(err)
	}
	if unfilled != 0 {
		t.Errorf("%d rows left unfilled after the backfill (batches filled %v)", unfilled, filled)
	}
	// The race was forced: a batch filled nothing while rows were left,
	// and a later batch filled one.
	if lost := slices.Index(filled, 0); lost < 0 || !slices.ContainsFunc(filled[lost:], func(n int64) bool { return n > 0 }) {
		t.Errorf("batches filled %v: no lost batch with rows left, the race was not forced", filled)
	}
}

// oldReleaseUpsert is the previous release's Index statement, which
// knows nothing of tsv_prefix: a replica still running it during a
// rolling upgrade writes rows the new one searches.
const oldReleaseUpsert = `INSERT INTO search_documents (id, type, text, fields, tsv)
VALUES ($1, '', $2, '{}', to_tsvector('english', $2))
ON CONFLICT (id) DO UPDATE SET
	type   = EXCLUDED.type,
	text   = EXCLUDED.text,
	fields = EXCLUDED.fields,
	tsv    = EXCLUDED.tsv`

// TestPostgresPrefixTracksWeightedFields: the trigger's arguments carry
// WeightedFields, so a boot with a new field list replaces it and the
// field's words prefix-match from the next write on.
func TestPostgresPrefixTracksWeightedFields(t *testing.T) {
	ctx := context.Background()
	first := newSearchIndex(t, nil)
	idx, err := NewPostgres(first.db, PostgresConfig{WeightedFields: map[string]byte{"title": 'B'}})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	doc := Document{ID: "1", Text: "helpers", Fields: map[string]any{"title": "pagination"}}
	if err := idx.Index(ctx, doc); err != nil {
		t.Fatal(err)
	}
	res, err := idx.Search(ctx, Query{Text: "paginat"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("weighted field prefix hits = %d, want 1", len(res))
	}
}

// TestPostgresConcurrentTriggerSwap: replicas booting together all find
// the function and trigger missing; the advisory lock lets one create
// them and the rest see them current. Unserialized, the CREATE OR
// REPLACE FUNCTION calls collide in pg_proc.
func TestPostgresConcurrentTriggerSwap(t *testing.T) {
	ctx := context.Background()
	first := newSearchIndex(t, nil)
	if _, err := first.db.Exec(`DROP FUNCTION search_documents_tsv_prefix() CASCADE`); err != nil {
		t.Fatal(err)
	}
	// One session per replica (pgtest pins one; its search_path rides
	// the DSN, so every connection sees the test schema).
	first.db.SetMaxOpenConns(8)
	first.db.SetMaxIdleConns(8)
	// Open the sessions up front, so the replicas start together rather
	// than one connection dial apart.
	var warm sync.WaitGroup
	for range 6 {
		warm.Go(func() { _, _ = first.db.Exec(`SELECT pg_sleep(0.05)`) })
	}
	warm.Wait()
	start := make(chan struct{})
	errs := make(chan error, 6)
	for range cap(errs) {
		go func() {
			<-start
			idx, err := NewPostgres(first.db, PostgresConfig{WeightedFields: map[string]byte{"title": 'B'}})
			if err == nil {
				err = idx.ensurePrefixTrigger(ctx)
			}
			errs <- err
		}()
	}
	close(start)
	for range cap(errs) {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}

// TestPostgresPrefixFollowsOldWriters: after the column is marked
// filled, a row the old release inserts must still prefix-match, and a
// row it rewrites must prefix-match its new text, not its old.
func TestPostgresPrefixFollowsOldWriters(t *testing.T) {
	ctx := context.Background()
	idx := newSearchIndex(t, nil)
	if err := idx.Index(ctx, Document{ID: "1", Text: "helpers"}); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.db.Exec(oldReleaseUpsert, "2", "pagination helpers"); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.db.Exec(oldReleaseUpsert, "1", "the cats are running"); err != nil {
		t.Fatal(err)
	}
	// A later boot of the new release must not need to repair anything.
	if err := idx.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ q, want string }{{"paginat", "2"}, {"helpe", "2"}, {"runnin", "1"}} {
		res, err := idx.Search(ctx, Query{Text: tc.q})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, r := range res {
			got = append(got, r.Document.ID)
		}
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("q=%q: got %v, want [%s]", tc.q, got, tc.want)
		}
	}
}
