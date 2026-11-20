package search

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

// PostgresSearch is a full-text search backend backed by a single
// Postgres table whose tsv column is a TSVECTOR maintained via
// to_tsvector at index time.
type PostgresSearch struct {
	db          *sql.DB
	table       string
	quotedTable string
	quotedIndex string
	prefixIndex string
	prefixFn    string
	prefixTrg   string
	language    string

	weightedFields     map[string]byte
	weightedFieldOrder []string
}

// PostgresConfig configures a PostgresSearch.
type PostgresConfig struct {
	// Table is the destination table.
	Table string

	// Language is the tsvector text-search configuration name.
	Language string

	// WeightedFields maps Document.Fields keys (string values only) to a
	// tsvector weight in the range 'A'..'D'.
	WeightedFields map[string]byte
}

var langRe = regexp.MustCompile(`^[a-z_]+$`)

// NewPostgres constructs a PostgresSearch, validating the table
// identifier (via the same core/query.SafeIdent used across the
// framework), the language allowlist, and every configured weight.
func NewPostgres(db *sql.DB, cfg PostgresConfig) (*PostgresSearch, error) {
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
	lang := cfg.Language
	if lang == "" {
		lang = "english"
	}
	if !langRe.MatchString(lang) {
		return nil, fmt.Errorf("search: invalid language %q (must match ^[a-z_]+$)", lang)
	}
	wf := make(map[string]byte, len(cfg.WeightedFields))
	for k, w := range cfg.WeightedFields {
		if k == "" {
			return nil, errors.New("search: empty weighted field key")
		}
		if w < 'A' || w > 'D' {
			return nil, fmt.Errorf("search: weight for field %q must be 'A'..'D', got %q", k, w)
		}
		wf[k] = w
	}
	order := make([]string, 0, len(wf))
	for k := range wf {
		order = append(order, k)
	}
	sort.Strings(order)
	registerOwnerEraser(safe)
	return &PostgresSearch{
		db:                 db,
		table:              safe,
		quotedTable:        query.QuoteFolded(safe),
		quotedIndex:        query.QuoteIndexName(pgTsvIndexName(safe)),
		prefixIndex:        query.QuoteIndexName(pgRelName(safe, "_tsv_prefix_idx")),
		prefixFn:           pgName(safe + "_tsv_prefix"),
		prefixTrg:          storedTriggerName(pgName(safe + "_tsv_prefix")),
		language:           lang,
		weightedFields:     wf,
		weightedFieldOrder: order,
	}, nil
}

func pgName(s string) string {
	if len(s) > 63 {
		return s[:63]
	}
	return s
}

func storedTriggerName(s string) string {
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		s = s[i+1:]
	}
	return strings.ToLower(s)
}

func pgTsvIndexName(base string) string {
	name := base + "_tsv_idx"
	if len(name) > 63 {
		name = name[:63]
	}
	if name == base {
		return pgRelName(base, "_tsv_idx")
	}
	return name
}

func pgRelName(base, suffix string) string {
	if len(base)+len(suffix) <= 63 {
		return base + suffix
	}
	h := fnv.New32a()
	h.Write([]byte(base))
	tag := fmt.Sprintf("_%08x", h.Sum32())
	return base[:63-len(tag)-len(suffix)] + tag + suffix
}

// EnsureSchema creates the table and its GIN index if they do not already
// exist.
func (p *PostgresSearch) EnsureSchema(ctx context.Context) error {
	if err := query.RefuseCaseKeptTables(ctx, p.db, p.table); err != nil {
		return fmt.Errorf("search: %w", err)
	}
	//gofastr:allow(GOFASTR1415) a search index is derived from records the entity graph already exports, and a restore re-indexes them; registerOwnerEraser covers erasure by Document.Owner
	createTable := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id     TEXT PRIMARY KEY NOT NULL,
		type   TEXT NOT NULL,
		text   TEXT NOT NULL,
		fields JSONB,
		tsv    TSVECTOR NOT NULL,
		tsv_prefix TSVECTOR,
		owner  TEXT NOT NULL DEFAULT ''
	)`, p.quotedTable)
	if _, err := p.db.ExecContext(ctx, createTable); err != nil {
		return fmt.Errorf("search: create table: %w", err)
	}
	if _, err := p.db.ExecContext(ctx, fmt.Sprintf(
		`ALTER TABLE %s ADD COLUMN IF NOT EXISTS owner TEXT NOT NULL DEFAULT ''`, p.quotedTable)); err != nil {
		return fmt.Errorf("search: add owner column: %w", err)
	}
	if err := p.ensurePrefixColumn(ctx); err != nil {
		return err
	}
	for _, idx := range [][2]string{{p.quotedIndex, "tsv"}, {p.prefixIndex, "tsv_prefix"}} {
		createIndex := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s USING GIN (%s)`,
			idx[0], p.quotedTable, idx[1])
		if _, err := p.db.ExecContext(ctx, createIndex); err != nil {
			return fmt.Errorf("search: create index: %w", err)
		}
	}
	return nil
}

var afterPrefixBatch func(filled int64)

var prefixBackfillBatch = 1000

const prefixFilledComment = "gofastr search: tsv_prefix filled"

const prefixTriggerBody = `
BEGIN
	NEW.tsv_prefix := setweight(to_tsvector('simple'::regconfig, NEW.text), 'A');
	FOR i IN 0 .. TG_NARGS - 2 BY 2 LOOP
		NEW.tsv_prefix := NEW.tsv_prefix || setweight(to_tsvector('simple'::regconfig, coalesce(NEW.fields->>TG_ARGV[i], '')), TG_ARGV[i + 1]::"char");
	END LOOP;
	RETURN NEW;
END
`

func (p *PostgresSearch) ensurePrefixColumn(ctx context.Context) error {
	var has bool
	var comment sql.NullString
	if err := p.db.QueryRowContext(ctx, `SELECT
		EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = to_regclass($1) AND attname = 'tsv_prefix' AND NOT attisdropped),
		(SELECT col_description(attrelid, attnum) FROM pg_attribute WHERE attrelid = to_regclass($1) AND attname = 'tsv_prefix' AND NOT attisdropped)`,
		p.quotedTable).Scan(&has, &comment); err != nil {
		return fmt.Errorf("search: probe tsv_prefix: %w", err)
	}
	if !has {
		if _, err := p.db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS tsv_prefix TSVECTOR`, p.quotedTable)); err != nil {
			return fmt.Errorf("search: add tsv_prefix: %w", err)
		}
	}
	if err := p.ensurePrefixTrigger(ctx); err != nil {
		return err
	}
	if has && comment.String == prefixFilledComment {
		return nil
	}
	backfill := fmt.Sprintf(`UPDATE %[1]s SET text = text
		WHERE tsv_prefix IS NULL AND id IN (SELECT id FROM %[1]s WHERE tsv_prefix IS NULL LIMIT $1)`,
		p.quotedTable)
	remaining := fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE tsv_prefix IS NULL)`, p.quotedTable)
	for {
		res, err := p.db.ExecContext(ctx, backfill, prefixBackfillBatch)
		if err != nil {
			return fmt.Errorf("search: backfill tsv_prefix: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("search: backfill tsv_prefix: %w", err)
		}
		if afterPrefixBatch != nil {
			afterPrefixBatch(n)
		}
		if n > 0 {
			continue
		}
		var left bool
		if err := p.db.QueryRowContext(ctx, remaining).Scan(&left); err != nil {
			return fmt.Errorf("search: backfill tsv_prefix: %w", err)
		}
		if !left {
			break
		}
	}
	if _, err := p.db.ExecContext(ctx, fmt.Sprintf(`COMMENT ON COLUMN %s.tsv_prefix IS '%s'`, p.quotedTable, prefixFilledComment)); err != nil {
		return fmt.Errorf("search: mark tsv_prefix filled: %w", err)
	}
	return nil
}

func (p *PostgresSearch) ensurePrefixTrigger(ctx context.Context) error {
	var trgArgs []string
	var wantArgs []byte
	for _, key := range p.weightedFieldOrder {
		for _, a := range []string{key, string(p.weightedFields[key])} {
			trgArgs = append(trgArgs, a)
			wantArgs = append(append(wantArgs, a...), 0)
		}
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("search: tsv_prefix trigger: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "gofastr search: "+p.quotedTable); err != nil {
		return fmt.Errorf("search: tsv_prefix trigger lock: %w", err)
	}
	fn := query.QuoteFolded(p.prefixFn)
	var src sql.NullString
	var gotArgs []byte
	var hasTrg bool
	if err := tx.QueryRowContext(ctx, `SELECT
		(SELECT prosrc FROM pg_proc WHERE oid = to_regproc($1)),
		(SELECT tgargs FROM pg_trigger WHERE tgrelid = to_regclass($2) AND tgname = $3),
		EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid = to_regclass($2) AND tgname = $3)`,
		fn, p.quotedTable, p.prefixTrg).Scan(&src, &gotArgs, &hasTrg); err != nil {
		return fmt.Errorf("search: probe tsv_prefix trigger: %w", err)
	}
	if src.String != prefixTriggerBody {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`CREATE OR REPLACE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $gofastr$%s$gofastr$`,
			fn, prefixTriggerBody)); err != nil {
			return fmt.Errorf("search: create tsv_prefix function: %w", err)
		}
	}
	if !hasTrg || !bytes.Equal(gotArgs, wantArgs) {
		quoted := make([]string, len(trgArgs))
		for j, a := range trgArgs {
			//gofastr:allow(GOFASTR1502) one quote per configured field key, only when the trigger is (re)created at boot
			if err := tx.QueryRowContext(ctx, `SELECT quote_literal($1)`, a).Scan(&quoted[j]); err != nil {
				return fmt.Errorf("search: quote tsv_prefix trigger argument: %w", err)
			}
		}
		trg := query.QuoteIndexName(p.prefixTrg)
		for _, stmt := range []string{
			fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON %s`, trg, p.quotedTable),
			fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT OR UPDATE OF text, fields ON %s FOR EACH ROW EXECUTE FUNCTION %s(%s)`,
				trg, p.quotedTable, fn, strings.Join(quoted, ", ")),
		} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("search: create tsv_prefix trigger: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("search: tsv_prefix trigger: %w", err)
	}
	return nil
}

// Index inserts or replaces a document (UPSERT on id).
func (p *PostgresSearch) Index(ctx context.Context, doc Document) error {
	var fieldsArg any
	if len(doc.Fields) == 0 {
		fieldsArg = []byte("{}")
	} else {
		b, err := json.Marshal(doc.Fields)
		if err != nil {
			return fmt.Errorf("search: marshal fields: %w", err)
		}
		fieldsArg = b
	}

	args := []any{doc.ID, doc.Type, doc.Text, fieldsArg, p.language, doc.Owner}
	const textIdx, langIdx = 3, 5

	var tsv strings.Builder
	fmt.Fprintf(&tsv, "setweight(to_tsvector($%d::regconfig, $%d), 'A')", langIdx, textIdx)
	for _, key := range p.weightedFieldOrder {
		v, present := doc.Fields[key]
		if !present {
			continue
		}
		val, ok := v.(string)
		if !ok {
			return fmt.Errorf("search: index %q: weighted field %q must be a string, got %T", doc.ID, key, v)
		}
		args = append(args, val)
		fmt.Fprintf(&tsv, " || setweight(to_tsvector($%d::regconfig, $%d), '%c')",
			langIdx, len(args), p.weightedFields[key])
	}

	stmt := fmt.Sprintf(`INSERT INTO %s AS t (id, type, text, fields, owner, tsv)
VALUES ($1, $2, $3, $4, $6, %s)
ON CONFLICT (id) DO UPDATE SET
	type   = EXCLUDED.type,
	text   = EXCLUDED.text,
	fields = EXCLUDED.fields,
	owner  = EXCLUDED.owner,
	tsv    = EXCLUDED.tsv`,
		p.quotedTable, tsv.String())
	if _, err := p.db.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("search: index %q: %w", doc.ID, err)
	}
	return nil
}

// Delete removes a document by ID.
func (p *PostgresSearch) Delete(ctx context.Context, id string) error {
	stmt := fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, p.quotedTable)
	if _, err := p.db.ExecContext(ctx, stmt, id); err != nil {
		return fmt.Errorf("search: delete %q: %w", id, err)
	}
	return nil
}

// Search runs a ranked full-text query.
func (p *PostgresSearch) Search(ctx context.Context, q Query) ([]Result, error) {
	if len(strings.Fields(q.Text)) == 0 {
		return p.searchAll(ctx, q)
	}
	terms := queryTerms(q.Text)
	if len(terms) == 0 {
		return []Result{}, nil
	}
	rest, last := terms[:len(terms)-1], terms[len(terms)-1]+":*"

	args := []any{tsJoin(rest, last), p.language, last}
	from := "CROSS JOIN to_tsquery($2::regconfig, $1) AS q CROSS JOIN to_tsquery('simple'::regconfig, $3) AS qp"
	var where strings.Builder
	if len(rest) > 0 {
		args = append(args, tsJoin(rest, ""))
		from += fmt.Sprintf(" CROSS JOIN to_tsquery($2::regconfig, $%d) AS qn", len(args))
		where.WriteString("(tsv @@ q OR (tsv_prefix @@ qp AND (numnode(qn) = 0 OR tsv @@ qn)))")
	} else {
		where.WriteString("(tsv @@ q OR tsv_prefix @@ qp)")
	}
	if q.Type != "" {
		args = append(args, q.Type)
		fmt.Fprintf(&where, " AND type = $%d", len(args))
	}
	if len(q.FieldEquals) > 0 {
		b, err := json.Marshal(q.FieldEquals)
		if err != nil {
			return nil, fmt.Errorf("search: marshal field filter: %w", err)
		}
		args = append(args, b)
		fmt.Fprintf(&where, " AND fields @> $%d::jsonb", len(args))
	}

	offset := max(q.Offset, 0)
	limit := q.Limit

	var paging strings.Builder
	if limit > 0 {
		args = append(args, limit)
		fmt.Fprintf(&paging, " LIMIT $%d", len(args))
	}
	if offset > 0 {
		args = append(args, offset)
		fmt.Fprintf(&paging, " OFFSET $%d", len(args))
	}

	stmt := fmt.Sprintf(`SELECT id, type, text, fields, owner,
	ts_rank(tsv, q) + COALESCE(ts_rank(tsv_prefix, qp), 0) AS score
FROM %s %s
WHERE %s
ORDER BY score DESC, id ASC%s`,
		p.quotedTable, from, where.String(), paging.String())

	rows, err := p.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("search: query: %w", err)
	}
	return scanSearchRows(rows)
}

func (p *PostgresSearch) searchAll(ctx context.Context, q Query) ([]Result, error) {
	args := []any{}
	var where strings.Builder
	where.WriteString("TRUE")
	if q.Type != "" {
		args = append(args, q.Type)
		fmt.Fprintf(&where, " AND type = $%d", len(args))
	}
	if len(q.FieldEquals) > 0 {
		b, err := json.Marshal(q.FieldEquals)
		if err != nil {
			return nil, fmt.Errorf("search: marshal field filter: %w", err)
		}
		args = append(args, b)
		fmt.Fprintf(&where, " AND fields @> $%d::jsonb", len(args))
	}

	offset := max(q.Offset, 0)

	var paging strings.Builder
	if q.Limit > 0 {
		args = append(args, q.Limit)
		fmt.Fprintf(&paging, " LIMIT $%d", len(args))
	}
	if offset > 0 {
		args = append(args, offset)
		fmt.Fprintf(&paging, " OFFSET $%d", len(args))
	}

	stmt := fmt.Sprintf(`SELECT id, type, text, fields, owner, 0::float8 AS score
FROM %s
WHERE %s
ORDER BY id ASC%s`,
		p.quotedTable, where.String(), paging.String())

	rows, err := p.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("search: query: %w", err)
	}
	return scanSearchRows(rows)
}

func scanSearchRows(rows *sql.Rows) ([]Result, error) {
	defer rows.Close()
	var results []Result
	for rows.Next() {
		var (
			id, docType, text, owner string
			fieldsBytes              []byte
			score                    float64
		)
		if err := rows.Scan(&id, &docType, &text, &fieldsBytes, &owner, &score); err != nil {
			return nil, fmt.Errorf("search: scan: %w", err)
		}
		doc := Document{ID: id, Type: docType, Text: text, Owner: owner}
		if len(fieldsBytes) > 0 {
			var fields map[string]any
			if err := jsonexact.Unmarshal(fieldsBytes, &fields); err != nil {
				return nil, fmt.Errorf("search: unmarshal fields: %w", err)
			}
			doc.Fields = fields
		}
		results = append(results, Result{Document: doc, Score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search: rows: %w", err)
	}
	return results, nil
}

func buildTsQuery(text string) string {
	terms := queryTerms(text)
	if len(terms) == 0 {
		return ""
	}
	return tsJoin(terms[:len(terms)-1], terms[len(terms)-1]+":*")
}

func tsJoin(terms []string, last string) string {
	out := make([]string, 0, len(terms)+1)
	out = append(out, terms...)
	if last != "" {
		out = append(out, last)
	}
	return strings.Join(out, " & ")
}

func queryTerms(text string) []string {
	parts := strings.FieldsFunc(text, func(r rune) bool { return !termRune(r) })
	seen := make(map[string]struct{}, len(parts))
	terms := make([]string, 0, len(parts))
	for _, part := range parts {
		t := sanitizeTsTerm(part)
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
	return terms
}

func sanitizeTsTerm(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if termRune(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return strings.Trim(b.String(), "-_")
}

func termRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' || r == '-'
}

var _ Backend = (*PostgresSearch)(nil)
