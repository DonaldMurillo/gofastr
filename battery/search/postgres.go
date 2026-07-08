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
	"unicode"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// PostgresSearch is a full-text search backend backed by a single Postgres
// table whose tsv column is a TSVECTOR maintained via to_tsvector at index
// time. It is the production counterpart to Memory: zero extra infrastructure
// for a Postgres-first app, and ranked results straight out of the database.
//
// The text body (Document.Text) is always indexed at weight 'A'. Structured
// fields may be promoted to weights 'B'..'D' via PostgresConfig.WeightedFields
// so a hit in, say, a document's title outranks one in its body. Structured
// fields are also stored as JSONB, which powers Query.FieldEquals containment
// filtering — the tenant/owner/permission scoping hook.
//
// Prefix matching is built in: the final query term is suffixed with ":*" so
// command-palette / search-as-you-type queries ("pagin" → "pagination") work
// without a second round trip.
//
// This backend deliberately avoids pg_trgm, which requires CREATE EXTENSION
// (a superuser privilege many managed-DB roles lack). Fuzzy/trigram search is
// left to a future backend that can opt into the extension explicitly.
type PostgresSearch struct {
	db          *sql.DB
	quotedTable string // validated + quoted table identifier
	quotedIndex string // validated + quoted GIN index name
	language    string // validated tsvector config name (e.g. "english")

	weightedFields     map[string]byte // field key -> weight 'A'..'D'
	weightedFieldOrder []string        // sorted keys -> deterministic SQL
}

// PostgresConfig configures a PostgresSearch. The zero value is usable after
// NewPostgres applies defaults.
type PostgresConfig struct {
	// Table is the destination table. Defaults to "search_documents".
	Table string

	// Language is the tsvector text-search configuration name. Defaults to
	// "english". Validated at construction against ^[a-z_]+$ — only lowercase
	// letters and underscores survive, so a caller cannot smuggle SQL here.
	Language string

	// WeightedFields maps Document.Fields keys (string values only) to a
	// tsvector weight in the range 'A'..'D'. The matched field's string value
	// is indexed at that weight; Document.Text is always weight 'A'. A field
	// whose value is not a string is skipped, even if configured here.
	WeightedFields map[string]byte
}

// langRe bounds the tsvector configuration name to a safe allowlist. Only
// lowercase ASCII letters and underscores are accepted — no quotes, spaces,
// semicolons, or anything that could break out of the regconfig cast.
var langRe = regexp.MustCompile(`^[a-z_]+$`)

// NewPostgres constructs a PostgresSearch, validating the table identifier
// (via the same core/query.SafeIdent used across the framework), the language
// allowlist, and every configured weight.
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
	return &PostgresSearch{
		db:                 db,
		quotedTable:        query.QuoteIdent(safe),
		quotedIndex:        query.QuoteIdent(safe + "_tsv_idx"),
		language:           lang,
		weightedFields:     wf,
		weightedFieldOrder: order,
	}, nil
}

// EnsureSchema creates the table and its GIN index if they do not already
// exist. Idempotent — safe to call on every boot.
func (p *PostgresSearch) EnsureSchema(ctx context.Context) error {
	createTable := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id     TEXT PRIMARY KEY,
		type   TEXT NOT NULL,
		text   TEXT NOT NULL,
		fields JSONB,
		tsv    TSVECTOR NOT NULL
	)`, p.quotedTable)
	if _, err := p.db.ExecContext(ctx, createTable); err != nil {
		return fmt.Errorf("search: create table: %w", err)
	}
	createIndex := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s USING GIN (tsv)`,
		p.quotedIndex, p.quotedTable)
	if _, err := p.db.ExecContext(ctx, createIndex); err != nil {
		return fmt.Errorf("search: create index: %w", err)
	}
	return nil
}

// Index inserts or replaces a document (UPSERT on id). The tsv column is
// rebuilt in SQL from the text body and any configured weighted fields whose
// value is a string, so re-indexing the same id updates in place with no
// duplicate rows.
func (p *PostgresSearch) Index(ctx context.Context, doc Document) error {
	// Fields are stored as a JSONB object. An empty/nil field map is stored
	// as "{}" (never SQL NULL) so the @> containment operator behaves
	// predictably and FieldEquals never matches a missing field.
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

	// Argument layout: $1=id $2=type $3=text $4=fields(JSONB) $5=language.
	args := []any{doc.ID, doc.Type, doc.Text, fieldsArg, p.language}
	const textIdx, langIdx = 3, 5

	// Text body is always weight 'A'. Each present weighted string field
	// appends another setweight(...) term; all values are parameterized and
	// only the validated weight chars are interpolated as literals.
	var tsv strings.Builder
	fmt.Fprintf(&tsv, "setweight(to_tsvector($%d::regconfig, $%d), 'A')", langIdx, textIdx)
	for _, key := range p.weightedFieldOrder {
		val, ok := doc.Fields[key].(string)
		if !ok {
			continue
		}
		args = append(args, val)
		fmt.Fprintf(&tsv, " || setweight(to_tsvector($%d::regconfig, $%d), '%c')",
			langIdx, len(args), p.weightedFields[key])
	}

	stmt := fmt.Sprintf(`INSERT INTO %s (id, type, text, fields, tsv)
VALUES ($1, $2, $3, $4, %s)
ON CONFLICT (id) DO UPDATE SET
	type   = EXCLUDED.type,
	text   = EXCLUDED.text,
	fields = EXCLUDED.fields,
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

// Search runs a ranked full-text query. See buildTsQuery for the query-text
// rules and Query.FieldEquals for the JSONB scoping hook.
func (p *PostgresSearch) Search(ctx context.Context, q Query) ([]Result, error) {
	// Mirror Memory's two edge behaviors exactly: an empty / whitespace-only
	// query matches every document (score 0, Type/FieldEquals filters still
	// applied), while a query whose terms all sanitize away (pure
	// punctuation) matches nothing — the same split Memory gets from
	// normalizeTerms + its score==0 skip.
	if len(strings.Fields(q.Text)) == 0 {
		return p.searchAll(ctx, q)
	}
	tsquery := buildTsQuery(q.Text)
	if tsquery == "" {
		// Terms existed but none survived sanitization: nothing can match.
		return []Result{}, nil
	}

	// The tsquery must be parsed with the SAME text-search configuration the
	// tsv column was built with — to_tsquery($1) alone would use the
	// database's default config and silently mis-stem every non-default
	// language (index "corriendo" under spanish → lexeme 'corr'; an
	// english-parsed query for "corría" never matches it).
	args := []any{tsquery, p.language}
	var where strings.Builder
	where.WriteString("tsv @@ q")
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

	// Clamp pagination exactly like Memory: negative offset becomes 0, and a
	// non-positive limit means "no limit". OFFSET/LIMIT are bound as params.
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
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

	stmt := fmt.Sprintf(`SELECT id, type, text, fields, ts_rank(tsv, q) AS score
FROM %s CROSS JOIN to_tsquery($2::regconfig, $1) AS q
WHERE %s
ORDER BY ts_rank(tsv, q) DESC, id ASC%s`,
		p.quotedTable, where.String(), paging.String())

	rows, err := p.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("search: query: %w", err)
	}
	return scanSearchRows(rows)
}

// searchAll is the empty-query path: every document (score 0) that passes
// the Type/FieldEquals filters, ordered by id for a stable page walk —
// the same result Memory produces when normalizeTerms yields no terms.
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

	offset := q.Offset
	if offset < 0 {
		offset = 0
	}

	var paging strings.Builder
	if q.Limit > 0 {
		args = append(args, q.Limit)
		fmt.Fprintf(&paging, " LIMIT $%d", len(args))
	}
	if offset > 0 {
		args = append(args, offset)
		fmt.Fprintf(&paging, " OFFSET $%d", len(args))
	}

	stmt := fmt.Sprintf(`SELECT id, type, text, fields, 0::float8 AS score
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

// scanSearchRows drains a search result set into []Result, reconstructing
// each Document (including its JSONB fields). It closes rows.
func scanSearchRows(rows *sql.Rows) ([]Result, error) {
	defer rows.Close()
	var results []Result
	for rows.Next() {
		var (
			id, docType, text string
			fieldsBytes       []byte
			score             float64
		)
		if err := rows.Scan(&id, &docType, &text, &fieldsBytes, &score); err != nil {
			return nil, fmt.Errorf("search: scan: %w", err)
		}
		doc := Document{ID: id, Type: docType, Text: text}
		if len(fieldsBytes) > 0 {
			var fields map[string]any
			if err := json.Unmarshal(fieldsBytes, &fields); err != nil {
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

// buildTsQuery turns a raw query string into a to_tsquery operand. It is the
// single chokepoint where untrusted query text is sanitized before reaching
// the database, so it is pure (no DB dependency) and unit-testable directly.
//
// Rules:
//   - Split on whitespace.
//   - From each term, keep only letters, digits, underscore, and hyphen
//     ([\pL\pN_-]); everything else is dropped (this strips every to_tsquery
//     operator — '&', '|', '!', ':', '(', ')' — and SQL metacharacters).
//   - Lowercase and trim leading/trailing '-'/'_' so a term can never be
//     pure punctuation.
//   - Drop empties.
//   - Dedupe (case-insensitive after lowercasing) and cap at maxQueryTerms so
//     an attacker-controlled query cannot amplify cost — the same bound
//     Memory applies.
//   - If nothing survives, return "" (caller short-circuits to empty results).
//   - Join the survivors with " & " (AND semantics, matching Memory) and
//     suffix the LAST term with ":*" for prefix matching.
//
// The result is passed strictly as a parameter to to_tsquery($n): query text
// is never interpolated into SQL.
func buildTsQuery(text string) string {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return ""
	}
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
	if len(terms) == 0 {
		return ""
	}
	terms[len(terms)-1] += ":*"
	return strings.Join(terms, " & ")
}

// sanitizeTsTerm lowercases s and retains only [\pL\pN_-], then trims
// leading/trailing '-'/'_'. The trim guarantees no term is pure punctuation,
// which keeps the emitted to_tsquery syntactically valid.
func sanitizeTsTerm(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return strings.Trim(b.String(), "-_")
}

// Compile-time interface check.
var _ Backend = (*PostgresSearch)(nil)
