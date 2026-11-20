package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// SQLiteFTS is a full-text search backend backed by a single SQLite FTS5
// virtual table.
type SQLiteFTS struct {
	db          *sql.DB
	table       string
	quotedTable string

	prefixTable  string
	quotedPrefix string
	quotedVocab  string

	matchTable  string
	matchPrefix string
}

// SQLiteFTSConfig configures a SQLiteFTS.
type SQLiteFTSConfig struct {
	// Table is the destination FTS5 virtual table name.
	Table string
}

const fts5MissingSubstr = "no such module"

// NewSQLiteFTS constructs a SQLiteFTS, validating the table identifier
// via the same core/query.SafeIdent used across the framework.
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
	registerOwnerEraser(safe, safe+"_prefix")
	_, bare := sqliteSplit(safe)
	return &SQLiteFTS{
		matchTable:   query.QuoteFolded(bare),
		matchPrefix:  query.QuoteFolded(bare + "_prefix"),
		db:           db,
		table:        safe,
		quotedTable:  query.QuoteFolded(safe),
		prefixTable:  safe + "_prefix",
		quotedPrefix: query.QuoteFolded(safe + "_prefix"),
		quotedVocab:  query.QuoteFolded(safe + "_prefix_vocab"),
	}, nil
}

// EnsureSchema creates the FTS5 virtual table if it does not already
// exist.
func (s *SQLiteFTS) EnsureSchema(ctx context.Context) error {
	createTable := fmt.Sprintf(
		`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(%s, tokenize='porter unicode61')`,
		s.quotedTable, sqliteMainColumns,
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
	if err := s.ensureOwnerColumn(ctx); err != nil {
		return err
	}
	return s.ensurePrefixSchema(ctx)
}

const sqliteMainColumns = `id UNINDEXED, type UNINDEXED, fields UNINDEXED, owner UNINDEXED, text`

func sqliteSplit(table string) (schema, name string) {
	if i := strings.LastIndexByte(table, '.'); i >= 0 {
		return table[:i], table[i+1:]
	}
	return "", table
}

func sqliteCatalog(table string) string {
	if schema, _ := sqliteSplit(table); schema != "" {
		return query.QuoteFolded(schema) + ".sqlite_master"
	}
	return "sqlite_master"
}

func hasColumn(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, table, col string) (bool, error) {
	var n int
	var err error
	if schema, name := sqliteSplit(table); schema != "" {
		err = q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info(?, ?) WHERE name = ?`, name, schema, col).Scan(&n)
	} else {
		err = q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info(?) WHERE name = ?`, table, col).Scan(&n)
	}
	return n > 0, err
}

func (s *SQLiteFTS) ensureOwnerColumn(ctx context.Context) error {
	has, err := hasColumn(ctx, s.db, s.table, "owner")
	if err != nil {
		return fmt.Errorf("search: probe owner column: %w", err)
	}
	if has {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("search: begin owner migration: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck — safe after Commit
	tmp := query.QuoteFolded(s.table + "_owner_tmp")
	_, bare := sqliteSplit(s.table)
	for _, stmt := range []string{
		fmt.Sprintf(`CREATE VIRTUAL TABLE %s USING fts5(%s, tokenize='porter unicode61')`, tmp, sqliteMainColumns),
		fmt.Sprintf(`INSERT INTO %s (rowid, id, type, fields, owner, text) SELECT rowid, id, type, fields, '', text FROM %s`, tmp, s.quotedTable),
		fmt.Sprintf(`DROP TABLE %s`, s.quotedTable),
		fmt.Sprintf(`ALTER TABLE %s RENAME TO %s`, tmp, query.QuoteFolded(bare)),
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("search: add owner column: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("search: commit owner migration: %w", err)
	}
	return nil
}

func (s *SQLiteFTS) ensurePrefixSchema(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("search: begin prefix schema: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck — safe after Commit
	var existed int
	_, barePrefix := sqliteSplit(s.prefixTable)
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM `+sqliteCatalog(s.prefixTable)+` WHERE name = ? COLLATE NOCASE`, barePrefix,
	).Scan(&existed); err != nil {
		return fmt.Errorf("search: probe prefix table: %w", err)
	}
	stale := false
	if existed > 0 {
		has, err := hasColumn(ctx, tx, s.prefixTable, "owner")
		if err != nil {
			return fmt.Errorf("search: probe prefix owner column: %w", err)
		}
		stale = !has
	}
	var stmts []string
	if stale {
		stmts = append(stmts, fmt.Sprintf(`DROP TABLE %s`, s.quotedPrefix))
	}
	if existed == 0 || stale {
		stmts = append(stmts, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, query.QuoteFolded(s.prefixTable+"_content")))
	}
	stmts = append(stmts,
		//gofastr:allow(GOFASTR1415) the prefix companion is the main index's token twin; NewSQLiteFTS registers its owner eraser beside the main table's, and like the main index it is rebuilt, not exported
		fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(owner UNINDEXED, text, content='', contentless_delete=1, contentless_unindexed=1, tokenize='unicode61 remove_diacritics 0')`, s.quotedPrefix),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s`, s.quotedVocab),
	)
	if existed == 0 || stale {
		stmts = append(stmts, fmt.Sprintf(`INSERT INTO %s (rowid, owner, text) SELECT rowid, owner, text FROM %s`, s.quotedPrefix, s.quotedTable))
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

// Index inserts or replaces a document.
func (s *SQLiteFTS) Index(ctx context.Context, doc Document) error {
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

	insStmt := fmt.Sprintf(`INSERT INTO %s (id, type, fields, owner, text) VALUES (?, ?, ?, ?, ?)`, s.quotedTable)
	res, err := tx.ExecContext(ctx, insStmt, doc.ID, doc.Type, fieldsArg, doc.Owner, doc.Text)
	if err != nil {
		return fmt.Errorf("search: index %q: %w", doc.ID, err)
	}
	rowid, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("search: index %q: %w", doc.ID, err)
	}
	prefixStmt := fmt.Sprintf(`INSERT INTO %s (rowid, owner, text) VALUES (?, ?, ?)`, s.quotedPrefix)
	if _, err := tx.ExecContext(ctx, prefixStmt, rowid, doc.Owner, doc.Text); err != nil {
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

func (s *SQLiteFTS) deleteTx(ctx context.Context, tx *sql.Tx, id string) error {
	prefixStmt := fmt.Sprintf(`DELETE FROM %s WHERE rowid IN (SELECT rowid FROM %s WHERE id = ?)`, s.quotedPrefix, s.quotedTable)
	if _, err := tx.ExecContext(ctx, prefixStmt, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id = ?`, s.quotedTable), id)
	return err
}

// Search runs a ranked full-text query.
func (s *SQLiteFTS) Search(ctx context.Context, q Query) ([]Result, error) {
	if len(strings.Fields(q.Text)) == 0 {
		return s.searchAll(ctx, q)
	}
	terms := queryTerms(q.Text)
	if len(terms) == 0 {
		return []Result{}, nil
	}
	rest, last := terms[:len(terms)-1], terms[len(terms)-1]
	args := []any{fts5Match(terms), fts5Match([]string{last})}
	prefixLeg := fmt.Sprintf(`SELECT rowid AS rid, -bm25(%[2]s) AS score FROM %[1]s WHERE %[2]s MATCH ?`, s.quotedPrefix, s.matchPrefix)
	if len(rest) > 0 {
		args = append(args, quoteTerms(rest))
		prefixLeg += fmt.Sprintf(` AND rowid IN (SELECT rowid FROM %[1]s WHERE %[2]s MATCH ?)`, s.quotedTable, s.matchTable)
	}
	var where strings.Builder
	where.WriteString("TRUE")
	args = s.appendFilters(&where, args, q)

	offset := max(q.Offset, 0)
	var paging strings.Builder
	if q.Limit > 0 {
		args = append(args, q.Limit)
		paging.WriteString(" LIMIT ?")
	} else if offset > 0 {
		paging.WriteString(" LIMIT -1")
	}
	if offset > 0 {
		args = append(args, offset)
		paging.WriteString(" OFFSET ?")
	}

	stmt := fmt.Sprintf(`SELECT m.id, m.type, m.text, m.fields, COALESCE(m.owner, ''), h.score
FROM (SELECT rid, SUM(score) AS score FROM (
	SELECT rowid AS rid, -bm25(%[5]s) AS score FROM %[1]s WHERE %[5]s MATCH ?
	UNION ALL
	%[2]s
) GROUP BY rid) AS h
JOIN %[1]s AS m ON m.rowid = h.rid
WHERE %[3]s
ORDER BY h.score DESC, m.id ASC%[4]s`,
		s.quotedTable, prefixLeg, where.String(), paging.String(), s.matchTable)

	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("search: query: %w", err)
	}
	return scanSearchRows(rows)
}

func (s *SQLiteFTS) searchAll(ctx context.Context, q Query) ([]Result, error) {
	args := []any{}
	var where strings.Builder
	where.WriteString("TRUE")
	args = s.appendFilters(&where, args, q)

	offset := max(q.Offset, 0)
	var paging strings.Builder
	if q.Limit > 0 {
		args = append(args, q.Limit)
		paging.WriteString(" LIMIT ?")
	} else if offset > 0 {
		paging.WriteString(" LIMIT -1")
	}
	if offset > 0 {
		args = append(args, offset)
		paging.WriteString(" OFFSET ?")
	}

	stmt := fmt.Sprintf(`SELECT id, type, text, fields, COALESCE(owner, ''), 0 AS score
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

func (s *SQLiteFTS) appendFilters(where *strings.Builder, args []any, q Query) []any {
	if q.Type != "" {
		args = append(args, q.Type)
		where.WriteString(" AND type = ?")
	}
	if len(q.FieldEquals) > 0 {
		keys := make([]string, 0, len(q.FieldEquals))
		for k := range q.FieldEquals {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			where.WriteString(" AND EXISTS (SELECT 1 FROM json_each(fields) WHERE key = ? AND type = 'text' AND value = ?)")
			args = append(args, k, q.FieldEquals[k])
		}
	}
	return args
}

func buildFts5Query(text string) string {
	return fts5Match(queryTerms(text))
}

func fts5Match(terms []string) string {
	if len(terms) == 0 {
		return ""
	}
	return quoteTerms(terms) + "*"
}

func quoteTerms(terms []string) string {
	quoted := make([]string, len(terms))
	for i, t := range terms {
		quoted[i] = `"` + t + `"`
	}
	return strings.Join(quoted, " ")
}

var _ Backend = (*SQLiteFTS)(nil)
