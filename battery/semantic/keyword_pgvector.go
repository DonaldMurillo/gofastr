package semantic

import (
	"context"
	"fmt"
	"strings"
)

// Keyword returns a KeywordBackend over this store's table with Postgres
// full-text search, shared by every replica.
func (s *PgVectorStore) Keyword() KeywordBackend { return pgKeyword{s: s} }

type pgKeyword struct{ s *PgVectorStore }

func (pgKeyword) Index(context.Context, string, string) error { return nil }

func (pgKeyword) Delete(context.Context, string) error { return nil }

func (k pgKeyword) Search(ctx context.Context, text string, top int) ([]KeywordHit, error) {
	top = defaultTop(top)
	q := tsqueryAny(keywordTokens(text))
	if q == "" {
		return nil, nil
	}
	rows, err := k.s.db.QueryContext(ctx, fmt.Sprintf(
		`SELECT id, ts_rank(to_tsvector('simple', text), q) AS score
		FROM %s, CAST($1 AS tsquery) AS q
		WHERE to_tsvector('simple', text) @@ q
		ORDER BY score DESC, id
		LIMIT $2`, k.s.quotedTable), q, top)
	if err != nil {
		return nil, fmt.Errorf("semantic: pgvector keyword search: %w", err)
	}
	defer rows.Close()
	var out []KeywordHit
	for rows.Next() {
		var h KeywordHit
		if err := rows.Scan(&h.ChunkID, &h.Score); err != nil {
			return nil, fmt.Errorf("semantic: pgvector keyword search: %w", err)
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("semantic: pgvector keyword search: %w", err)
	}
	return out, nil
}

func tsqueryAny(terms []string) string {
	var b strings.Builder
	for _, t := range terms {
		if b.Len() > 0 {
			b.WriteString(" | ")
		}
		b.WriteString("'" + strings.ReplaceAll(t, "'", "''") + "'")
	}
	return b.String()
}

func (s *PgVectorStore) sharedAcrossProcesses() {}

var _ sharedStore = (*PgVectorStore)(nil)
