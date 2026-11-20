package semantic

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
)

const modelTable = "semantic_models"

const bindTimeout = 10 * time.Second

type modelBinder interface {
	bindModel(ctx context.Context, model string) error
}

func (s *PgVectorStore) ensureModelTable(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS `+query.QuoteFolded(modelTable)+` (
		table_name TEXT PRIMARY KEY NOT NULL,
		model      TEXT NOT NULL,
		dim        INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("semantic: create %s table: %w", modelTable, err)
	}
	return nil
}

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
