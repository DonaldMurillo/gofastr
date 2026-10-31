package semantic

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// modelTable records which embedding model each pgvector chunk table
// holds, one row per table. A pgvector column fixes the dimension and
// nothing else, so two models of one dimension would otherwise mix in
// one table and rank as one space.
const modelTable = "semantic_models"

// bindTimeout bounds Open's model check; Open takes no context.
const bindTimeout = 10 * time.Second

// modelBinder is a store that records the embedding model its vectors
// came from and refuses another (PgVectorStore). Open calls it with the
// embedder's Name.
type modelBinder interface {
	bindModel(ctx context.Context, model string) error
}

// ensureModelTable creates the model table; EnsureSchema calls it.
func (s *PgVectorStore) ensureModelTable(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS `+query.QuoteFolded(modelTable)+` (
		table_name TEXT PRIMARY KEY,
		model      TEXT NOT NULL,
		dim        INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("semantic: create %s table: %w", modelTable, err)
	}
	return nil
}

// bindModel records model and the store's dimension for this table, or
// checks them against the recorded pair. A table that holds no rows
// takes the new pair: there are no vectors to mix, and emptying the
// table is how a host re-indexes after a model change. The model row is
// locked for the check: a write in flight (checkModel holds the row
// shared) finishes before the emptiness check, which then sees its rows.
func (s *PgVectorStore) bindModel(ctx context.Context, model string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("semantic: check embedding model: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	quoted := query.QuoteFolded(modelTable)
	key := foldedTable(s.table)
	if _, err := tx.ExecContext(ctx, `INSERT INTO `+quoted+` (table_name, model, dim) VALUES ($1, $2, $3)
		ON CONFLICT (table_name) DO NOTHING`, key, model, s.dim); err != nil {
		// 42P01 = undefined_table.
		if pqCode(err) == "42P01" {
			return fmt.Errorf("semantic: the %s table is missing: call PgVectorStore.EnsureSchema before Open", modelTable)
		}
		return fmt.Errorf("semantic: check embedding model: %w", err)
	}
	var gotModel string
	var gotDim int
	if err := tx.QueryRowContext(ctx, `SELECT model, dim FROM `+quoted+` WHERE table_name = $1 FOR UPDATE`, key).
		Scan(&gotModel, &gotDim); err != nil {
		return fmt.Errorf("semantic: check embedding model: %w", err)
	}
	if gotModel != model || gotDim != s.dim {
		var held bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM `+s.quotedTable+`)`).Scan(&held); err != nil {
			return fmt.Errorf("semantic: check embedding model: %w", err)
		}
		if held {
			return &ModelMismatchError{SnapshotModel: gotModel, SnapshotDim: gotDim, StoreModel: model, StoreDim: s.dim}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE `+quoted+` SET model = $2, dim = $3 WHERE table_name = $1`,
			key, model, s.dim); err != nil {
			return fmt.Errorf("semantic: check embedding model: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("semantic: check embedding model: %w", err)
	}
	return nil
}

// checkModel refuses a write of chunks embedded with model while the
// table is bound to another: a replica still running the old model after
// another emptied the table and opened with a new one. It holds the model
// row shared until the write commits, so a bindModel rebinding the table
// waits for the write and then finds its rows.
func (s *PgVectorStore) checkModel(ctx context.Context, tx *sql.Tx, model string) error {
	var gotModel string
	var gotDim int
	err := tx.QueryRowContext(ctx, `SELECT model, dim FROM `+query.QuoteFolded(modelTable)+` WHERE table_name = $1 FOR SHARE`,
		foldedTable(s.table)).Scan(&gotModel, &gotDim)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("semantic: no embedding model is recorded for table %s: open the store through semantic.Open", s.table)
	case pqCode(err) == "42P01":
		return fmt.Errorf("semantic: the %s table is missing: call PgVectorStore.EnsureSchema before Open", modelTable)
	case err != nil:
		return fmt.Errorf("semantic: check embedding model: %w", err)
	}
	if gotModel != model || gotDim != s.dim {
		return &ModelMismatchError{SnapshotModel: gotModel, SnapshotDim: gotDim, StoreModel: model, StoreDim: s.dim}
	}
	return nil
}

// foldedTable is the table name as Postgres stores it: QuoteFolded
// lowercases it, so Embed_Chunks and embed_chunks name one table and
// share one model row. NewPgVector validated the name, so it is ASCII.
func foldedTable(table string) string {
	b := []byte(table)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

var _ modelBinder = (*PgVectorStore)(nil)
