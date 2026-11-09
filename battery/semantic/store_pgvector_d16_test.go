package semantic

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// D-16 (#669): the keyword search filtered on to_tsvector('simple',
// text) with no index — a full table scan recomputing the tsvector for
// every row on every hybrid query — and ChunkIDsForDoc folded a
// database failure into an empty list, which the keyword purge read as
// "nothing to clean" and left entries pointing at a removed doc.
func TestPgVectorKeywordSearchUsesGINIndex(t *testing.T) {
	s := newPgStore(t, 3)
	ctx := context.Background()
	// Enough rows that the planner prefers the index when sequential
	// scans are disabled; the point is the index exists and matches the
	// search's predicate expression exactly.
	chunks := make([]Chunk, 0, 64)
	for i := range 64 {
		chunks = append(chunks, Chunk{
			ID:    fmt.Sprintf("k%03d", i),
			DocID: "d",
			Text:  fmt.Sprintf("uncommon token %d filler words", i),
			Vec:   vec3(1, 0, 0),
		})
	}
	if err := s.Add(ctx, chunks); err != nil {
		t.Fatalf("Add: %v", err)
	}

	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_indexes WHERE schemaname = current_schema() AND indexname = $1`,
		keywordIndexName(s.table)).Scan(&count); err != nil {
		t.Fatalf("read pg_indexes: %v", err)
	}
	if count != 1 {
		t.Fatalf("keyword GIN index %s not present on table %s", keywordIndexName(s.table), s.table)
	}

	// With sequential scans off, the planner must answer the search's
	// own predicate through the expression index: the same
	// to_tsvector('simple', text) the Search method filters on.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatalf("disable seqscan: %v", err)
	}
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(
		`EXPLAIN SELECT id FROM %s WHERE to_tsvector('simple', text) @@ CAST($1 AS tsquery)`,
		s.quotedTable), "'token'")
	if err != nil {
		t.Fatalf("explain keyword search: %v", err)
	}
	defer rows.Close()
	var planLines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain: %v", err)
		}
		planLines = append(planLines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("explain rows: %v", err)
	}
	plan := strings.Join(planLines, "\n")
	if !strings.Contains(plan, "Bitmap Index Scan") && !strings.Contains(plan, "Index Scan") {
		t.Fatalf("keyword search plan does not use the GIN index:\n%s", plan)
	}
	if !strings.Contains(plan, keywordIndexName(s.table)) {
		t.Fatalf("keyword search plan scans a different index:\n%s", plan)
	}
}

// ChunkIDsForDoc must fail, not fold the failure into an empty list.
func TestPgVectorChunkIDsForDocReturnsError(t *testing.T) {
	s := newPgStore(t, 3)
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if _, err := s.ChunkIDsForDoc("d1"); err != nil {
		t.Fatalf("listing on a healthy table: %v", err)
	}
	if _, err := s.db.Exec(`DROP TABLE ` + s.quotedTable); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	ids, err := s.ChunkIDsForDoc("d1")
	if err == nil {
		t.Fatalf("ChunkIDsForDoc over a dropped table returned %v, want an error", ids)
	}
	if len(ids) != 0 {
		t.Fatalf("a failed listing returned %v too", ids)
	}
}

// A schema-qualified table must not truncate its qualifier into the
// index name: two indexes of one table would share a name and CREATE
// INDEX IF NOT EXISTS would skip the second (the review gate's
// finding on the first cut).
func TestIndexNamesQualifiedTableDistinct(t *testing.T) {
	qualified := strings.Repeat("s", 60) + ".t"
	if got, want := docIndexName(qualified), "t_doc_id_idx"; got != want {
		t.Fatalf("doc index for %s = %q, want %q", qualified, got, want)
	}
	if got, want := keywordIndexName(qualified), "t_kw_fts_idx"; got != want {
		t.Fatalf("keyword index for %s = %q, want %q", qualified, got, want)
	}
	if docIndexName(qualified) == keywordIndexName(qualified) {
		t.Fatal("doc and keyword index names collide for a schema-qualified table")
	}
}

// Both index suffixes must survive PostgreSQL's identifier limit.
func TestIndexNamesLongTableDistinct(t *testing.T) {
	for _, n := range []int{61, 62, 63} {
		table := strings.Repeat("t", n)
		doc, kw := docIndexName(table), keywordIndexName(table)
		if doc == kw || len(doc) > 63 || len(kw) > 63 {
			t.Fatalf("length %d: colliding or oversized index names %q / %q", n, doc, kw)
		}
	}
}

func TestPgVectorLongTableHasBothIndexes(t *testing.T) {
	for _, n := range []int{62, 63} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			s, err := NewPgVector(openPgVector(t), PgVectorConfig{Table: strings.Repeat("t", n), Dim: 3, Model: "test"})
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := s.EnsureSchema(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			var count int
			err = s.db.QueryRow(`SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND tablename=$1 AND indexname IN ($2,$3)`, s.table, docIndexName(s.table), keywordIndexName(s.table)).Scan(&count)
			if err != nil || count != 2 {
				t.Fatalf("table length %d: found %d document/keyword indexes, error %v", n, count, err)
			}
		})
	}
}
