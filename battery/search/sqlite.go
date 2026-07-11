package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// SQLiteFTS is a full-text search backend backed by a single SQLite FTS5
// virtual table. It is the durable counterpart to Memory for SQLite-first
// apps: ranked BM25 full-text search straight out of the database with zero
// extra infrastructure.
//
// Only Document.Text is tokenised; id, type, and fields are UNINDEXED FTS5
// columns stored alongside the index for fast reconstruction and
// Query.FieldEquals filtering. FieldEquals is evaluated via
// json_extract on the stored fields JSON, mirroring the string-only
// containment contract the Postgres backend encodes as JSONB @>.
//
// Prefix matching is built in: the final FTS5 query term is suffixed with
// "*" (outside the double-quote) so command-palette / search-as-you-type
// queries ("pagin" → "pagination") work without a second round trip.
//
// FTS5 is an SQLite compile-time option. The mattn/go-sqlite3 driver
// bundles FTS5 only when built with the -tags sqlite_fts5 build tag.
// EnsureSchema translates the resulting "no such module: fts5" error into
// an actionable message naming the tag.
type SQLiteFTS struct {
	db          *sql.DB
	quotedTable string // validated + quoted FTS5 table identifier
}

// SQLiteFTSConfig configures a SQLiteFTS. The zero value is usable after
// NewSQLiteFTS applies defaults.
type SQLiteFTSConfig struct {
	// Table is the destination FTS5 virtual table name. Defaults to
	// "search_documents".
	Table string
}

// fieldKeyRe bounds a FieldEquals key before it is interpolated into a
// json_extract JSON path. Only ASCII letters, digits, and underscore
// survive — no quotes, dots, semicolons, or anything that could break out
// of the JSON path. The value itself is always parameterised.
var fieldKeyRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// fts5MissingSubstr is the substring of the SQLite error when the driver was
// compiled without FTS5 support (mattn/go-sqlite3 without -tags sqlite_fts5).
const fts5MissingSubstr = "no such module"

// NewSQLiteFTS constructs a SQLiteFTS, validating the table identifier via
// the same core/query.SafeIdent used across the framework.
func NewSQLiteFTS(db *sql.DB, cfg SQLiteFTSConfig) (*SQLiteFTS, error) {
	if db == nil {
		return nil, errors.New("search: nil db")
	}
	table := cfg.Table
	if table == "" {
		table = "search_documents"
	}
	safe, err := query.SafeIdent(table)
	if err != nil {
		return nil, fmt.Errorf("search: invalid table name %q: %w", table, err)
	}
	return &SQLiteFTS{
		db:          db,
		quotedTable: query.QuoteIdent(safe),
	}, nil
}

// EnsureSchema creates the FTS5 virtual table if it does not already exist.
// Idempotent — safe to call on every boot.
//
// If the SQLite driver lacks FTS5 support (mattn/go-sqlite3 compiled
// without the sqlite_fts5 build tag), the error is translated into an
// actionable message naming the tag.
func (s *SQLiteFTS) EnsureSchema(ctx context.Context) error {
	createTable := fmt.Sprintf(
		`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(id UNINDEXED, type UNINDEXED, fields UNINDEXED, text, tokenize='porter unicode61')`,
		s.quotedTable,
	)
	if _, err := s.db.ExecContext(ctx, createTable); err != nil {
		if strings.Contains(err.Error(), fts5MissingSubstr) {
			return fmt.Errorf(
				"search: FTS5 is not available in this SQLite build — "+
					"rebuild with the build tag 'sqlite_fts5' "+
					"(e.g. go build -tags sqlite_fts5): %w", err,
			)
		}
		return fmt.Errorf("search: create fts5 table: %w", err)
	}
	return nil
}

// Index inserts or replaces a document. FTS5 has no upsert, so Index
// DELETEs the existing row by id and INSERTs the new one in a single
// transaction — re-indexing the same id replaces in place with no duplicate
// rows.
func (s *SQLiteFTS) Index(ctx context.Context, doc Document) error {
	// Fields are stored as a JSON text object. An empty/nil field map is
	// stored as "{}" (never SQL NULL) so json_extract behaves predictably
	// and FieldEquals never matches a missing field.
	var fieldsArg any
	if len(doc.Fields) == 0 {
		fieldsArg = "{}"
	} else {
		b, err := json.Marshal(doc.Fields)
		if err != nil {
			return fmt.Errorf("search: marshal fields: %w", err)
		}
		fieldsArg = string(b)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("search: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck — safe after Commit

	delStmt := fmt.Sprintf(`DELETE FROM %s WHERE id = ?`, s.quotedTable)
	if _, err := tx.ExecContext(ctx, delStmt, doc.ID); err != nil {
		return fmt.Errorf("search: delete old %q: %w", doc.ID, err)
	}

	insStmt := fmt.Sprintf(`INSERT INTO %s (id, type, fields, text) VALUES (?, ?, ?, ?)`, s.quotedTable)
	if _, err := tx.ExecContext(ctx, insStmt, doc.ID, doc.Type, fieldsArg, doc.Text); err != nil {
		return fmt.Errorf("search: index %q: %w", doc.ID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("search: commit index %q: %w", doc.ID, err)
	}
	return nil
}

// Delete removes a document by ID.
func (s *SQLiteFTS) Delete(ctx context.Context, id string) error {
	stmt := fmt.Sprintf(`DELETE FROM %s WHERE id = ?`, s.quotedTable)
	if _, err := s.db.ExecContext(ctx, stmt, id); err != nil {
		return fmt.Errorf("search: delete %q: %w", id, err)
	}
	return nil
}

// Search runs a ranked full-text query. See buildFts5Query for the query-text
// rules and Query.FieldEquals for the JSON scoping hook.
func (s *SQLiteFTS) Search(ctx context.Context, q Query) ([]Result, error) {
	// Mirror Memory's two edge behaviors exactly: an empty / whitespace-only
	// query matches every document (score 0, Type/FieldEquals filters still
	// applied), while a query whose terms all sanitize away (pure
	// punctuation) matches nothing — the same split Memory gets from
	// normalizeTerms + its score==0 skip.
	if len(strings.Fields(q.Text)) == 0 {
		return s.searchAll(ctx, q)
	}
	ftsQuery := buildFts5Query(q.Text)
	if ftsQuery == "" {
		// Terms existed but none survived sanitization: nothing can match.
		return []Result{}, nil
	}

	args := []any{ftsQuery}
	var where strings.Builder
	fmt.Fprintf(&where, "%s MATCH ?", s.quotedTable)
	extra, err := s.appendFilters(&where, args, q)
	if err != nil {
		return nil, err
	}
	args = extra

	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	var paging strings.Builder
	if q.Limit > 0 {
		args = append(args, q.Limit)
		paging.WriteString(" LIMIT ?")
	} else if offset > 0 {
		// SQLite requires LIMIT before OFFSET; -1 means "no limit".
		paging.WriteString(" LIMIT -1")
	}
	if offset > 0 {
		args = append(args, offset)
		paging.WriteString(" OFFSET ?")
	}

	// bm25 returns negative values (more negative = better match), so
	// ORDER BY bm25(...) ASC puts best matches first. Score = -bm25 so
	// callers sort descending consistently with the other backends.
	stmt := fmt.Sprintf(`SELECT id, type, text, fields, -bm25(%s) AS score
FROM %s
WHERE %s
ORDER BY bm25(%s) ASC, id ASC%s`,
		s.quotedTable, s.quotedTable, where.String(), s.quotedTable, paging.String())

	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("search: query: %w", err)
	}
	return scanSearchRows(rows)
}

// searchAll is the empty-query path: every document (score 0) that passes
// the Type/FieldEquals filters, ordered by id for a stable page walk —
// the same result Memory produces when normalizeTerms yields no terms.
func (s *SQLiteFTS) searchAll(ctx context.Context, q Query) ([]Result, error) {
	args := []any{}
	var where strings.Builder
	where.WriteString("TRUE")
	extra, err := s.appendFilters(&where, args, q)
	if err != nil {
		return nil, err
	}
	args = extra

	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	var paging strings.Builder
	if q.Limit > 0 {
		args = append(args, q.Limit)
		paging.WriteString(" LIMIT ?")
	} else if offset > 0 {
		// SQLite requires LIMIT before OFFSET; -1 means "no limit".
		paging.WriteString(" LIMIT -1")
	}
	if offset > 0 {
		args = append(args, offset)
		paging.WriteString(" OFFSET ?")
	}

	stmt := fmt.Sprintf(`SELECT id, type, text, fields, 0 AS score
FROM %s
WHERE %s
ORDER BY id ASC%s`,
		s.quotedTable, where.String(), paging.String())

	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("search: query: %w", err)
	}
	return scanSearchRows(rows)
}

// appendFilters adds Type and FieldEquals predicates to where and appends
// their parameters to args, returning the extended slice. Each FieldEquals
// key is validated against fieldKeyRe BEFORE its JSON path is interpolated;
// the lookup value is always parameterised.
func (s *SQLiteFTS) appendFilters(where *strings.Builder, args []any, q Query) ([]any, error) {
	if q.Type != "" {
		args = append(args, q.Type)
		where.WriteString(" AND type = ?")
	}
	if len(q.FieldEquals) > 0 {
		// Sort keys for deterministic SQL regardless of map iteration order.
		keys := make([]string, 0, len(q.FieldEquals))
		for k := range q.FieldEquals {
			if !fieldKeyRe.MatchString(k) {
				return nil, fmt.Errorf("search: invalid FieldEquals key %q (must match ^[A-Za-z0-9_]+$)", k)
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(where, " AND json_extract(fields, '$.%s') = ?", `"`+k+`"`)
			args = append(args, q.FieldEquals[k])
		}
	}
	return args, nil
}

// buildFts5Query turns a raw query string into an FTS5 MATCH operand. It is
// the single chokepoint where untrusted query text is sanitized before
// reaching the database, so it is pure (no DB dependency) and unit-testable
// directly.
//
// Rules (mirrors buildTsQuery's sanitizer character class exactly):
//   - Split on whitespace.
//   - From each term, keep only letters, digits, underscore, and hyphen
//     ([\pL\pN_-]); everything else is dropped (this strips every FTS5
//     operator — AND, OR, NOT, NEAR — column filters, parens, quotes, and
//     SQL metacharacters).
//   - Lowercase and trim leading/trailing '-'/'_' so a term can never be
//     pure punctuation.
//   - Drop empties.
//   - Dedupe (case-insensitive after lowercasing) and cap at maxQueryTerms so
//     an attacker-controlled query cannot amplify cost — the same bound
//     Memory and Postgres apply.
//   - If nothing survives, return "" (caller short-circuits to empty results).
//   - Double-quote each survivor (neutralises any residual FTS5 operators).
//   - Suffix the LAST term with "*" (outside the quote) for FTS5 prefix
//     matching.
//
// The result is passed strictly as a parameter to the MATCH operator: query
// text is never interpolated into SQL.
func buildFts5Query(text string) string {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(parts))
	terms := make([]string, 0, len(parts))
	for _, part := range parts {
		t := sanitizeTsTerm(part) // reuse the exact same sanitizer as Postgres
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		terms = append(terms, t)
		if len(terms) >= maxQueryTerms {
			break
		}
	}
	if len(terms) == 0 {
		return ""
	}
	// Double-quote each term so FTS5 operators, column filters, and parens
	// in the sanitized text are treated as literal phrase content, never
	// interpreted as query syntax.
	for i, t := range terms {
		terms[i] = `"` + t + `"`
	}
	// Prefix token: the * goes OUTSIDE the closing quote.
	terms[len(terms)-1] += "*"
	return strings.Join(terms, " ")
}

// Compile-time interface check.
var _ Backend = (*SQLiteFTS)(nil)
