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

// PostgresSearch is a full-text search backend backed by a single Postgres
// table whose tsv column is a TSVECTOR maintained via to_tsvector at index
// time. It is the production counterpart to Memory: zero extra infrastructure
// for a Postgres-first app, and ranked results straight out of the database.
//
// The text body (Document.Text) is always indexed at weight 'A'. Structured
// fields may be promoted to weights 'B'..'D' via PostgresConfig.WeightedFields
// so a hit in, say, a document's title outranks one in its body. Structured
// fields are also stored as JSONB, which powers Query.FieldEquals containment
// filtering, the tenant/owner/permission scoping hook.
//
// Prefix matching is built in: the final query term is suffixed with ":*" so
// command-palette / search-as-you-type queries ("pagin" → "pagination") work
// without a second round trip. The language config stems the prefix too
// ("paginat:*" → 'paginat':*, while the index holds 'pagin'), so a second
// tsvector column, tsv_prefix, holds the same text under the unstemmed
// 'simple' config and the last term also prefix-matches it. A trigger
// computes tsv_prefix on every write, so a writer that never names the
// column (the previous release, during a rolling upgrade) still keeps it
// current.
//
// This backend deliberately avoids pg_trgm, which requires CREATE EXTENSION
// (a superuser privilege many managed-DB roles lack). Fuzzy/trigram search is
// left to a future backend that can opt into the extension explicitly.
type PostgresSearch struct {
	db          *sql.DB
	table       string // validated configured table name
	quotedTable string // validated + quoted table identifier
	quotedIndex string // validated + quoted GIN index name
	prefixIndex string // quoted GIN index name for tsv_prefix
	prefixFn    string // raw name of the trigger function filling tsv_prefix
	prefixTrg   string // stored name of the trigger that calls it: folded, unqualified
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
	// "english". Validated at construction against ^[a-z_]+$, only lowercase
	// letters and underscores survive, so a caller cannot smuggle SQL here.
	Language string

	// WeightedFields maps Document.Fields keys (string values only) to a
	// tsvector weight in the range 'A'..'D'. The matched field's string value
	// is indexed at that weight; Document.Text is always weight 'A'. A field
	// whose value is not a string is skipped, even if configured here.
	WeightedFields map[string]byte
}

// langRe bounds the tsvector configuration name to a safe allowlist. Only
// lowercase ASCII letters and underscores are accepted, no quotes, spaces,
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

// pgName cuts an identifier to the 63 bytes Postgres keeps, so a catalog
// lookup by name finds what the DDL created. The name is validated
// ASCII, so no rune is split.
func pgName(s string) string {
	if len(s) > 63 {
		return s[:63]
	}
	return s
}

// storedTriggerName is the tgname Postgres keeps for a trigger created
// as query.QuoteIndexName(s): the part after the last dot, folded.
func storedTriggerName(s string) string {
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		s = s[i+1:]
	}
	return strings.ToLower(s)
}

// pgTsvIndexName names the tsv index the way released builds did:
// base+"_tsv_idx" as Postgres cuts it at 63 bytes, so a table those
// builds set up keeps its one index. Only a 63-byte base, where that cut
// is the table's own name and the index was never built, takes
// pgRelName's hashed form.
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

// pgRelName names a relation derived from base (an index on the table
// base names). base+suffix is kept when it fits Postgres's 63 bytes, so
// an existing index is found again. A longer one is base cut short, an
// 8-hex-digit hash of the whole base, then suffix: Postgres would cut it
// at 63 bytes itself, and two indexes on a table of 58 bytes or more
// would get one name, or at 63 bytes the table's own name, and CREATE
// INDEX IF NOT EXISTS would skip them.
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
// exist. Idempotent, safe to call on every boot.
func (p *PostgresSearch) EnsureSchema(ctx context.Context) error {
	if err := query.RefuseCaseKeptTables(ctx, p.db, p.table); err != nil {
		return fmt.Errorf("search: %w", err)
	}
	createTable := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id     TEXT PRIMARY KEY,
		type   TEXT NOT NULL,
		text   TEXT NOT NULL,
		fields JSONB,
		tsv    TSVECTOR NOT NULL,
		tsv_prefix TSVECTOR
	)`, p.quotedTable)
	if _, err := p.db.ExecContext(ctx, createTable); err != nil {
		return fmt.Errorf("search: create table: %w", err)
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

// afterPrefixBatch is a test hook run after each backfill batch with
// the rows it filled.
var afterPrefixBatch func(filled int64)

// prefixBackfillBatch is how many rows one backfill statement fills.
var prefixBackfillBatch = 1000

// prefixFilledComment marks a tsv_prefix column every row of which has
// been filled, so a later boot skips the backfill.
const prefixFilledComment = "gofastr search: tsv_prefix filled"

// prefixTriggerBody computes tsv_prefix for the row being written: the
// text at weight A, then each weighted field the trigger names as a
// (key, weight) argument pair, all under the unstemmed 'simple' config.
// It is the one definition of the column; Index leaves it to the trigger.
const prefixTriggerBody = `
BEGIN
	NEW.tsv_prefix := setweight(to_tsvector('simple'::regconfig, NEW.text), 'A');
	FOR i IN 0 .. TG_NARGS - 2 BY 2 LOOP
		NEW.tsv_prefix := NEW.tsv_prefix || setweight(to_tsvector('simple'::regconfig, coalesce(NEW.fields->>TG_ARGV[i], '')), TG_ARGV[i + 1]::"char");
	END LOOP;
	RETURN NEW;
END
`

// ensurePrefixColumn adds tsv_prefix to a table created before it existed,
// installs the trigger that keeps it current, and fills the rows written
// before the trigger existed. The column is added in a statement of its
// own, which holds the table only for the catalog change; the rows are
// then filled in committed batches, so other replicas keep searching and
// indexing throughout (a row not yet filled still matches through tsv,
// it only misses prefixes past its stem). A replica that finds a
// backfill already under way fills the rows left and waits behind the
// other's batches. When no row is left the column is marked. The
// trigger goes in before the backfill, so once the mark is set no
// writer, whatever release it runs, can leave a row empty or stale.
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
	// SET text = text fires the trigger, which fills the row. The outer
	// tsv_prefix IS NULL is rechecked after a lock wait, so a row another
	// replica filled meanwhile is not filled twice.
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
		// A batch that filled nothing either finished the table or lost
		// its rows to another replica's batch; only an empty probe ends it.
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

// ensurePrefixTrigger installs the function and trigger that compute
// tsv_prefix on every insert and on every update of text or fields. A
// boot that finds both current changes nothing; a changed WeightedFields
// replaces the trigger (its arguments carry the fields). Replicas booting
// together take turns on an advisory lock: two concurrent CREATE OR
// REPLACE FUNCTION calls fail with "tuple concurrently updated".
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
		// The field keys are host configuration of any spelling; the
		// server quotes each one as a literal.
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
	// tsv_prefix is the trigger's to fill (see ensurePrefixTrigger).
	var tsv strings.Builder
	fmt.Fprintf(&tsv, "setweight(to_tsvector($%d::regconfig, $%d), 'A')", langIdx, textIdx)
	for _, key := range p.weightedFieldOrder {
		v, present := doc.Fields[key]
		if !present {
			continue
		}
		val, ok := v.(string)
		if !ok {
			// A configured weighted field present with a non-string
			// value is a data mismatch, not an absent field: silently
			// skipping it leaves the value unsearchable with no signal
			// to the indexer (the laxcoerce shape).
			return fmt.Errorf("search: index %q: weighted field %q must be a string, got %T", doc.ID, key, v)
		}
		args = append(args, val)
		fmt.Fprintf(&tsv, " || setweight(to_tsvector($%d::regconfig, $%d), '%c')",
			langIdx, len(args), p.weightedFields[key])
	}

	// The target is aliased so a table named excluded does not collide
	// with the EXCLUDED pseudo-row.
	stmt := fmt.Sprintf(`INSERT INTO %s AS t (id, type, text, fields, tsv)
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
	// punctuation) matches nothing, the same split Memory gets from
	// normalizeTerms + its score==0 skip.
	if len(strings.Fields(q.Text)) == 0 {
		return p.searchAll(ctx, q)
	}
	terms := queryTerms(q.Text)
	if len(terms) == 0 {
		// Terms existed but none survived sanitization: nothing can match.
		return []Result{}, nil
	}
	rest, last := terms[:len(terms)-1], terms[len(terms)-1]+":*"

	// The tsquery must be parsed with the SAME text-search configuration the
	// tsv column was built with, to_tsquery($1) alone would use the
	// database's default config and silently mis-stem every non-default
	// language (index "corriendo" under spanish → lexeme 'corr'; an
	// english-parsed query for "corría" never matches it).
	//
	// A row matches the stemmed query q, or the last term's unstemmed
	// prefix qp against tsv_prefix plus the other terms stemmed (qn; a
	// stopword-only qn has no nodes and constrains nothing, as it does in
	// q).
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

	// Clamp pagination exactly like Memory: negative offset becomes 0, and a
	// non-positive limit means "no limit". OFFSET/LIMIT are bound as params.
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

	stmt := fmt.Sprintf(`SELECT id, type, text, fields,
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

// searchAll is the empty-query path: every document (score 0) that passes
// the Type/FieldEquals filters, ordered by id for a stable page walk,
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

// buildTsQuery turns a raw query string into a to_tsquery operand. It is the
// single chokepoint where untrusted query text is sanitized before reaching
// the database, so it is pure (no DB dependency) and unit-testable directly.
//
// Rules:
//   - Split at every rune that is not a letter, digit, underscore, or
//     hyphen ([\pL\pN_-]): whitespace, an apostrophe ("won't" → "won" "t"),
//     and every to_tsquery operator, '&', '|', '!', ':', '(', ')', and SQL
//     metacharacter, none of which reaches a term.
//   - Lowercase and trim leading/trailing '-'/'_' so a term can never be
//     pure punctuation.
//   - Drop empties.
//   - Dedupe (case-insensitive after lowercasing) and cap at maxQueryTerms so
//     an attacker-controlled query cannot amplify cost, the same bound
//     Memory applies.
//   - If nothing survives, return "" (caller short-circuits to empty results).
//   - Join the survivors with " & " (AND semantics, matching Memory) and
//     suffix the LAST term with ":*" for prefix matching.
//
// The result is passed strictly as a parameter to to_tsquery($n): query text
// is never interpolated into SQL.
func buildTsQuery(text string) string {
	terms := queryTerms(text)
	if len(terms) == 0 {
		return ""
	}
	return tsJoin(terms[:len(terms)-1], terms[len(terms)-1]+":*")
}

// tsJoin AND-joins tsquery operands, skipping an empty one.
func tsJoin(terms []string, last string) string {
	out := make([]string, 0, len(terms)+1)
	out = append(out, terms...)
	if last != "" {
		out = append(out, last)
	}
	return strings.Join(out, " & ")
}

// queryTerms is the sanitized term list both database backends query
// with: split at every rune a term cannot hold, sanitized (see
// sanitizeTsTerm), deduped, and capped at maxQueryTerms. Splitting rather
// than deleting keeps the query's words where the tokenizers put the
// document's: unicode61 and the Postgres parser index "won't" as "won"
// and "t", and a glued "wont" matched neither.
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

// sanitizeTsTerm lowercases s and retains only [\pL\pN_-], then trims
// leading/trailing '-'/'_'. The trim guarantees no term is pure punctuation,
// which keeps the emitted to_tsquery syntactically valid.
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

// termRune reports whether r can appear in a query term: [\pL\pN_-]. \pN
// is every number category, not only decimal digits: unicode61 keeps
// H₂O's ₂ inside one token, and the Postgres parser splits the term
// itself.
func termRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' || r == '-'
}

// Compile-time interface check.
var _ Backend = (*PostgresSearch)(nil)
