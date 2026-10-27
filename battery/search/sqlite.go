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
// queries ("pagin" → "pagination") work. FTS5 runs the porter stemmer over
// a prefix token too, so "paginat*" looks for stems starting "paginat"
// while the index holds "pagin": a companion unstemmed index (Table +
// "_prefix", contentless) takes the same prefix scan unstemmed, and a row
// matches when either does (the other terms still match stemmed).
//
// FTS5 is an SQLite compile-time option. The mattn/go-sqlite3 driver
// bundles FTS5 only when built with the -tags sqlite_fts5 build tag.
// EnsureSchema translates the resulting "no such module: fts5" error into
// an actionable message naming the tag.
type SQLiteFTS struct {
	db          *sql.DB
	quotedTable string // validated + quoted FTS5 table identifier

	prefixTable  string // unquoted name of the unstemmed companion table
	quotedPrefix string // quoted prefixTable
	quotedVocab  string // quoted vocabulary view earlier releases made; EnsureSchema drops it
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
// survive, no quotes, dots, semicolons, or anything that could break out
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
		db:           db,
		quotedTable:  query.QuoteFolded(safe),
		prefixTable:  safe + "_prefix",
		quotedPrefix: query.QuoteFolded(safe + "_prefix"),
		quotedVocab:  query.QuoteFolded(safe + "_prefix_vocab"),
	}, nil
}

// EnsureSchema creates the FTS5 virtual table if it does not already exist.
// Idempotent: safe to call on every boot.
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
				"search: FTS5 is not available in this SQLite build: "+
					"rebuild with the build tag 'sqlite_fts5' "+
					"(e.g. go build -tags sqlite_fts5): %w", err,
			)
		}
		return fmt.Errorf("search: create fts5 table: %w", err)
	}
	return s.ensurePrefixSchema(ctx)
}

// ensurePrefixSchema creates the unstemmed companion index and its
// vocabulary view. The companion is contentless (it stores no text, only
// the index) and shares the main table's rowids. A table that predates
// it is backfilled once, in the transaction that creates it.
func (s *SQLiteFTS) ensurePrefixSchema(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("search: begin prefix schema: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck — safe after Commit
	// SQLite names have no case: Docs opens the docs_prefix an earlier
	// boot made, and a case-sensitive probe would backfill it again.
	var existed int
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE name = ? COLLATE NOCASE`, s.prefixTable,
	).Scan(&existed); err != nil {
		return fmt.Errorf("search: probe prefix table: %w", err)
	}
	// remove_diacritics 0 keeps the index in the spelling the sanitized
	// query uses ("café", not "cafe"). The vocabulary view earlier
	// releases created over the companion is no longer read; drop it.
	stmts := []string{
		fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(text, content='', contentless_delete=1, tokenize='unicode61 remove_diacritics 0')`, s.quotedPrefix),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s`, s.quotedVocab),
	}
	if existed == 0 {
		stmts = append(stmts, fmt.Sprintf(`INSERT INTO %s (rowid, text) SELECT rowid, text FROM %s`, s.quotedPrefix, s.quotedTable))
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("search: create prefix index: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("search: commit prefix schema: %w", err)
	}
	return nil
}

// Index inserts or replaces a document. FTS5 has no upsert, so Index
// DELETEs the existing row by id and INSERTs the new one in a single
// transaction, re-indexing the same id replaces in place with no duplicate
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

	if err := s.deleteTx(ctx, tx, doc.ID); err != nil {
		return fmt.Errorf("search: delete old %q: %w", doc.ID, err)
	}

	insStmt := fmt.Sprintf(`INSERT INTO %s (id, type, fields, text) VALUES (?, ?, ?, ?)`, s.quotedTable)
	res, err := tx.ExecContext(ctx, insStmt, doc.ID, doc.Type, fieldsArg, doc.Text)
	if err != nil {
		return fmt.Errorf("search: index %q: %w", doc.ID, err)
	}
	rowid, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("search: index %q: %w", doc.ID, err)
	}
	prefixStmt := fmt.Sprintf(`INSERT INTO %s (rowid, text) VALUES (?, ?)`, s.quotedPrefix)
	if _, err := tx.ExecContext(ctx, prefixStmt, rowid, doc.Text); err != nil {
		return fmt.Errorf("search: prefix index %q: %w", doc.ID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("search: commit index %q: %w", doc.ID, err)
	}
	return nil
}

// Delete removes a document by ID.
func (s *SQLiteFTS) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("search: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck — safe after Commit
	if err := s.deleteTx(ctx, tx, id); err != nil {
		return fmt.Errorf("search: delete %q: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("search: commit delete %q: %w", id, err)
	}
	return nil
}

// deleteTx removes id from the companion prefix index, by the main
// table's rowids, and then from the main table.
func (s *SQLiteFTS) deleteTx(ctx context.Context, tx *sql.Tx, id string) error {
	prefixStmt := fmt.Sprintf(`DELETE FROM %s WHERE rowid IN (SELECT rowid FROM %s WHERE id = ?)`, s.quotedPrefix, s.quotedTable)
	if _, err := tx.ExecContext(ctx, prefixStmt, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id = ?`, s.quotedTable), id)
	return err
}

// Search runs a ranked full-text query. See buildFts5Query for the query-text
// rules and Query.FieldEquals for the JSON scoping hook.
func (s *SQLiteFTS) Search(ctx context.Context, q Query) ([]Result, error) {
	// Mirror Memory's two edge behaviors exactly: an empty / whitespace-only
	// query matches every document (score 0, Type/FieldEquals filters still
	// applied), while a query whose terms all sanitize away (pure
	// punctuation) matches nothing, the same split Memory gets from
	// normalizeTerms + its score==0 skip.
	if len(strings.Fields(q.Text)) == 0 {
		return s.searchAll(ctx, q)
	}
	terms := queryTerms(q.Text)
	if len(terms) == 0 {
		// Terms existed but none survived sanitization: nothing can match.
		return []Result{}, nil
	}
	// A row matches the stemmed query, or the last term as an unstemmed
	// prefix in the companion table with the other terms stemmed in the
	// main one: the shape Postgres gives tsv and tsv_prefix. The prefix
	// leg runs the FTS5 prefix scan itself, so every word the prefix
	// starts counts, however many there are.
	rest, last := terms[:len(terms)-1], terms[len(terms)-1]
	args := []any{fts5Match(terms), fts5Match([]string{last})}
	prefixLeg := fmt.Sprintf(`SELECT rowid AS rid, -bm25(%[1]s) AS score FROM %[1]s WHERE %[1]s MATCH ?`, s.quotedPrefix)
	if len(rest) > 0 {
		args = append(args, quoteTerms(rest))
		prefixLeg += fmt.Sprintf(` AND rowid IN (SELECT rowid FROM %[1]s WHERE %[1]s MATCH ?)`, s.quotedTable)
	}
	var where strings.Builder
	where.WriteString("TRUE")
	args, err := s.appendFilters(&where, args, q)
	if err != nil {
		return nil, err
	}

	offset := max(q.Offset, 0)
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
	// each leg scores -bm25 and a row both legs match sums the two, as
	// Postgres sums ts_rank over tsv and tsv_prefix. Callers sort
	// descending consistently with the other backends.
	stmt := fmt.Sprintf(`SELECT m.id, m.type, m.text, m.fields, h.score
FROM (SELECT rid, SUM(score) AS score FROM (
	SELECT rowid AS rid, -bm25(%[1]s) AS score FROM %[1]s WHERE %[1]s MATCH ?
	UNION ALL
	%[2]s
) GROUP BY rid) AS h
JOIN %[1]s AS m ON m.rowid = h.rid
WHERE %[3]s
ORDER BY h.score DESC, m.id ASC%[4]s`,
		s.quotedTable, prefixLeg, where.String(), paging.String())

	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("search: query: %w", err)
	}
	return scanSearchRows(rows)
}

// searchAll is the empty-query path: every document (score 0) that passes
// the Type/FieldEquals filters, ordered by id for a stable page walk,
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

	offset := max(q.Offset, 0)
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
			// json_extract returns an array or object as its JSON text, so
			// the type check is what keeps tenant=["acme"] from equalling
			// the filter value `["acme"]`: only a JSON string matches.
			path := `'$."` + k + `"'`
			fmt.Fprintf(where, " AND json_type(fields, %s) = 'text' AND json_extract(fields, %s) = ?", path, path)
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
//   - Split at every rune that is not a letter, digit, underscore, or
//     hyphen ([\pL\pN_-]): whitespace, an apostrophe ("won't" → "won" "t",
//     the tokens unicode61 indexed), column-filter colons, parens, quotes,
//     and SQL metacharacters, none of which reaches a term.
//   - Lowercase and trim leading/trailing '-'/'_' so a term can never be
//     pure punctuation.
//   - Drop empties.
//   - Dedupe (case-insensitive after lowercasing) and cap at maxQueryTerms so
//     an attacker-controlled query cannot amplify cost, the same bound
//     Memory and Postgres apply.
//   - If nothing survives, return "" (caller short-circuits to empty results).
//   - Double-quote each survivor (neutralises any residual FTS5 operators).
//   - Suffix the LAST term with "*" (outside the quote) for FTS5 prefix
//     matching.
//
// The result is passed strictly as a parameter to the MATCH operator: query
// text is never interpolated into SQL.
func buildFts5Query(text string) string {
	return fts5Match(queryTerms(text)) // the same terms Postgres gets
}

// fts5Match renders sanitized terms as an FTS5 MATCH operand: each term
// quoted (see quoteTerms) and the last carrying a "*" outside its quote.
// A hyphenated last term ("load-bal") is a phrase whose final token is
// the prefix, the way unicode61 split the indexed words.
func fts5Match(terms []string) string {
	if len(terms) == 0 {
		return ""
	}
	return quoteTerms(terms) + "*"
}

// quoteTerms double-quotes each sanitized term so FTS5 operators, column
// filters, and parens in it are literal phrase content, never query
// syntax, and joins them with spaces (an implicit AND).
func quoteTerms(terms []string) string {
	quoted := make([]string, len(terms))
	for i, t := range terms {
		quoted[i] = `"` + t + `"`
	}
	return strings.Join(quoted, " ")
}

// Compile-time interface check.
var _ Backend = (*SQLiteFTS)(nil)
