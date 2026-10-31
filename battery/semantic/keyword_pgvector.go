package semantic

import (
	"context"
	"fmt"
	"strings"
)

// Keyword returns a [KeywordBackend] that searches this store's table
// with Postgres full-text search, so every replica's hybrid keyword leg
// reads the rows the vector leg does. Pass it as [Options.Keyword] with
// this store; [Open] refuses a [MemoryKeyword] here, which would hold
// only the writes of the process that made it.
//
// Index and Delete do nothing: the table already holds each chunk's
// text, written and removed in the store's own transactions. Search
// matches any query term against to_tsvector('simple', text), the
// language-neutral configuration with no stemming, and ranks by
// ts_rank. Terms are split the way [MemoryKeyword] splits them (runs of
// letters and digits, lowercased), so a term the Postgres parser keeps
// whole (an email address, a URL, a decimal number) is not matched by
// its parts.
func (s *PgVectorStore) Keyword() KeywordBackend { return pgKeyword{s: s} }

type pgKeyword struct{ s *PgVectorStore }

func (pgKeyword) Index(context.Context, string, string) error { return nil }

func (pgKeyword) Delete(context.Context, string) error { return nil }

func (k pgKeyword) Search(ctx context.Context, text string, top int) ([]KeywordHit, error) {
	if top <= 0 {
		top = 10
	}
	q := tsqueryAny(keywordTokens(text))
	// No terms: an empty tsquery matches nothing, so skip the round trip.
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

// tsqueryAny spells terms as a tsquery matching any of them, each a
// quoted lexeme. keywordTokens yields only letters and digits, so no
// term holds a quote or an operator; the quotes are doubled anyway so
// the literal stays one lexeme if the tokenizer ever lets one through.
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

// sharedAcrossProcesses marks PgVectorStore as a store other processes
// write to; see [sharedStore].
func (s *PgVectorStore) sharedAcrossProcesses() {}

var _ sharedStore = (*PgVectorStore)(nil)
