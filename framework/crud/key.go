package crud

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// keyCols returns the handler's primary-key columns in declared order. A
// handler built by hand with no PrimaryKey reads its entity's key.
func (ch *CrudHandler) keyCols() []string {
	if len(ch.PrimaryKey) == 0 {
		return ch.Entity.KeyColumns()
	}
	return ch.PrimaryKey
}

// refuseBlankKeyParts refuses a write that would store "" in a key
// column: the row could never be named again by get, update or delete.
// On a key spanning several columns entity.SplitKey refuses an empty
// part; on a one-column key the record routes answer 400 for an empty
// {id}. A generated column holds its generated value by now, so only a
// caller-chosen key (or a hook) can reach here with "".
func (ch *CrudHandler) refuseBlankKeyParts(body map[string]any) error {
	cols := ch.keyCols()
	var fields map[string][]schema.Violation
	for _, c := range cols {
		if v, ok := body[c]; ok && v == "" {
			if fields == nil {
				fields = map[string][]schema.Violation{}
			}
			fields[c] = []schema.Violation{{Reason: schema.ReasonRequired}}
		}
	}
	if fields != nil {
		return &ValidationError{fields: fields}
	}
	return nil
}

// withKeyTiebreak returns sorts followed by every key column it does not
// already sort on, ascending. An offset page is only well defined over a
// total order: sorted on a non-unique column (or not sorted at all) the
// database may hand back tied rows in a different order for each page, and
// Postgres does, so a walk with ?sort=status&page=N skipped some rows and
// repeated others. The key makes the order total on every dialect.
//
// With no sort at all, an entity with a framework-written created_at
// lists oldest first: ids are random UUIDs, so key order alone would
// shuffle what used to read as insertion order.
func (ch *CrudHandler) withKeyTiebreak(sorts []filter.ParsedSort) []filter.ParsedSort {
	out := slices.Clone(sorts)
	if len(sorts) == 0 && ch.Entity != nil {
		for _, f := range ch.Entity.GetFields() {
			if f.Name == "created_at" && f.Type == schema.Timestamp && f.AutoGenerate != schema.AutoNone {
				out = append(out, filter.ParsedSort{Field: f.Name})
				break
			}
		}
	}
	for _, k := range ch.keyCols() {
		if !slices.ContainsFunc(sorts, func(s filter.ParsedSort) bool { return s.Field == k }) {
			out = append(out, filter.ParsedSort{Field: k})
		}
	}
	return out
}

// keyColumn is the handler's one key column, the column a relation from
// its entity joins on. A handler keyed on several columns has none; see
// entity.Entity.KeyColumn.
func (ch *CrudHandler) keyColumn() (string, error) {
	cols := ch.keyCols()
	if len(cols) != 1 {
		name := ""
		if ch.Entity != nil {
			name = ch.Entity.GetName()
		}
		return "", fmt.Errorf("%w: %s is keyed on (%s)", entity.ErrCompositeKey, name, strings.Join(cols, ", "))
	}
	return cols[0], nil
}

// isKeyCol reports whether col is one of the primary-key columns.
func (ch *CrudHandler) isKeyCol(col string) bool {
	for _, c := range ch.keyCols() {
		if c == col {
			return true
		}
	}
	return false
}

// keyCond builds the WHERE condition naming one record from its key
// values, one per key column: "a = $1 AND b = $2". The builders renumber
// each condition's placeholders, so it composes with any other Where.
// Values that do not match the key column for column (none, say) name
// no record, and the condition matches nothing.
func (ch *CrudHandler) keyCond(vals []any) (string, []any) {
	cols := ch.keyCols()
	if len(cols) == 0 || len(vals) != len(cols) {
		return "1 = 0", nil
	}
	conds := make([]string, len(cols))
	for i, c := range cols {
		conds[i] = c + " = $" + strconv.Itoa(i+1)
	}
	return strings.Join(conds, " AND "), vals
}

// keyWhere is the WHERE condition matching the record id names by
// equality on each key column: keyCond over the values entity.KeyValues
// reads from id. An id that does not split into the key columns names no
// record: the condition matches nothing, so every caller reaches its own
// not-found answer.
//
// A record id names a row only when it is the row's own id, the one
// entity.RowKey renders from the row. On a managed entity equality says
// that already: its key columns are types the framework created (TEXT,
// INTEGER), and KeyValues binds an integer only in canonical form. On an
// Unmanaged entity the database chose the types (a view's SELECT, a
// Postgres UUID, CITEXT or CHAR(n), a case-folding collation), and
// equality can match a second spelling: a request for the uppercase id
// would reach the row a Decider, asked about the id string, knows by its
// lowercase one. There each statement reads the key columns back
// (keyedCols) and keeps a row only when ownRow finds its own id is id.
// The check reads the row the statement itself reached, so no write
// between a check and the operation can swap in another.
func (ch *CrudHandler) keyWhere(id string) (string, []any) {
	vals, ok := ch.idVals(id)
	if !ok {
		return "1 = 0", nil
	}
	return ch.keyCond(vals)
}

// keyHasSpellings reports whether the key columns' equality may match a
// value under more than one spelling: the database, not the framework,
// typed an Unmanaged entity's columns.
func (ch *CrudHandler) keyHasSpellings() bool {
	return ch.Entity != nil && ch.Entity.Config.Unmanaged
}

// keyedCols is cols plus the key columns it lacks, when keyHasSpellings:
// the columns a statement reads so ownRow can see the row's own id.
func (ch *CrudHandler) keyedCols(cols []string) []string {
	if !ch.keyHasSpellings() {
		return cols
	}
	out := slices.Clip(cols)
	for _, k := range ch.keyCols() {
		if !slices.Contains(cols, k) {
			out = append(out, k)
		}
	}
	return out
}

// ownRow reports whether row, read with keyedCols(cols), is the record id
// names (see keyWhere), and removes the key columns keyedCols added, so a
// key column cols left out never reaches the caller.
func (ch *CrudHandler) ownRow(id string, row map[string]any, cols []string) bool {
	if !ch.keyHasSpellings() {
		return true
	}
	own, ok := ch.rowID(row)
	for _, k := range ch.keyCols() {
		if !slices.Contains(cols, k) {
			delete(row, ch.convertKey(k))
		}
	}
	return ok && own == id
}

// queryKeyed runs a statement reading keyedCols(cols) and scans every row
// it returns: equality may match more than one spelling, and a SQLite
// key may be declared under a stricter collation than its column.
func (ch *CrudHandler) queryKeyed(ctx context.Context, cols []string, sqlStr string, args []any) ([]map[string]any, error) {
	rows, err := ch.conn(ctx).QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ch.scanMany(rows, ch.keyedCols(cols))
}

// scanOwn runs a SELECT of keyedCols(cols) naming id's record and returns
// the row whose own id is id, or sql.ErrNoRows.
func (ch *CrudHandler) scanOwn(ctx context.Context, id string, cols []string, sqlStr string, args []any) (map[string]any, error) {
	if !ch.keyHasSpellings() {
		return ch.scanOne(ch.conn(ctx).QueryRowContext(ctx, sqlStr, args...), cols)
	}
	found, err := ch.queryKeyed(ctx, cols, sqlStr, args)
	if err != nil {
		return nil, err
	}
	for _, row := range found {
		if ch.ownRow(id, row, cols) {
			return row, nil
		}
	}
	return nil, sql.ErrNoRows
}

// writeOwn runs an UPDATE naming id's record, RETURNING keyedCols(cols),
// and returns the row it changed, or sql.ErrNoRows when it changed none.
// Every changed row must be id's own: a statement that also changed
// another fails with errNotFound, which rolls it back (inTx), so a key
// equality matches under two spellings is refused rather than half
// written.
func (ch *CrudHandler) writeOwn(ctx context.Context, id string, cols []string, sqlStr string, args []any) (map[string]any, error) {
	if !ch.keyHasSpellings() {
		return ch.scanOne(ch.conn(ctx).QueryRowContext(ctx, sqlStr, args...), cols)
	}
	changed, err := ch.queryKeyed(ctx, cols, sqlStr, args)
	if err != nil {
		return nil, err
	}
	if len(changed) == 0 {
		return nil, sql.ErrNoRows
	}
	for _, row := range changed {
		if !ch.ownRow(id, row, cols) {
			return nil, errNotFound
		}
	}
	return changed[0], nil
}

// execOwn runs a write naming id's record, built by build, and returns
// how many rows it changed. When keyHasSpellings every row the write
// reaches must be id's own, or it fails with errNotFound, which rolls it
// back (inTx). Postgres reads them back: build gets the key columns for a
// RETURNING clause, since a READ COMMITTED transaction sees rows other
// writes commit between two statements. SQLite cannot return rows from a
// virtual table (an FTS5 index), so there it reads the rows matching
// selects first, under the write's own WHERE (owner, tenant, live or
// trashed), which holds through the write: a SQLite transaction is
// serializable.
func (ch *CrudHandler) execOwn(ctx context.Context, id string, matching func(cols ...string) (string, []any), build func(returning []string) (string, []any)) (int64, error) {
	if !ch.keyHasSpellings() {
		sqlStr, args := build(nil)
		res, err := ch.conn(ctx).ExecContext(ctx, sqlStr, args...)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}
	if isPostgres(ctx, ch.DB) {
		sqlStr, args := build(ch.keyCols())
		changed, err := ch.queryKeyed(ctx, ch.keyCols(), sqlStr, args)
		if err != nil {
			return 0, err
		}
		for _, row := range changed {
			if !ch.ownRow(id, row, ch.keyCols()) {
				return 0, errNotFound
			}
		}
		return int64(len(changed)), nil
	}
	sel, selArgs := matching(ch.keyCols()...)
	matched, err := ch.queryKeyed(ctx, ch.keyCols(), sel, selArgs)
	if err != nil {
		return 0, err
	}
	for _, row := range matched {
		if !ch.ownRow(id, row, ch.keyCols()) {
			return 0, errNotFound
		}
	}
	sqlStr, args := build(nil)
	res, err := ch.conn(ctx).ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// isPostgres reports whether q's database answers SELECT version() with a
// PostgreSQL banner, query.IsPostgres over an executor: inside a
// transaction the probe runs on it. SQLite has no version(), and a failed
// statement leaves its transaction usable.
func isPostgres(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) bool {
	var v string
	if err := q.QueryRowContext(ctx, "SELECT version()").Scan(&v); err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(v), "postgresql")
}

// idVals splits a record id into its key values, one per key column,
// through entity.KeyValues: an integer key column takes its value only
// in canonical decimal and binds it as an integer, so no second spelling
// reaches a record that a Decider, asked about the id string, knows by
// the first.
func (ch *CrudHandler) idVals(id string) ([]any, bool) {
	var fields []schema.Field
	if ch.Entity != nil {
		fields = entity.ValueFields(ch.Registry, ch.Entity)
	}
	vals, err := entity.KeyValues(id, ch.keyCols(), fields)
	return vals, err == nil
}

// rowKeyVals reads the key values out of a row, under either the wire
// key or the column name. It reports false when any is missing or nil,
// and for a keyless table, whose rows have no name.
func (ch *CrudHandler) rowKeyVals(row map[string]any) ([]any, bool) {
	cols := ch.keyCols()
	if len(cols) == 0 {
		return nil, false
	}
	vals := make([]any, len(cols))
	for i, c := range cols {
		v, ok := row[ch.convertKey(c)]
		if !ok {
			v, ok = row[c]
		}
		if !ok || v == nil {
			return nil, false
		}
		vals[i] = v
	}
	return vals, true
}

// colKeyVals reads the key values out of a column-keyed body (a write
// after wire keys became columns). It returns nil when any is absent or
// nil, the way a body that names no row yet reads.
func (ch *CrudHandler) colKeyVals(body map[string]any) []any {
	cols := ch.keyCols()
	vals := make([]any, len(cols))
	for i, c := range cols {
		v, ok := body[c]
		if !ok || v == nil {
			return nil
		}
		vals[i] = v
	}
	return vals
}

// rowID is a row's record id as JoinKey renders it.
func (ch *CrudHandler) rowID(row map[string]any) (string, bool) {
	return entity.RowKey(row, ch.keyCols(), ch.convertKey)
}

// keyPayload is the event payload naming a record by its id: each key
// column under its wire key.
func (ch *CrudHandler) keyPayload(id string) map[string]any {
	cols := ch.keyCols()
	parts, err := entity.SplitKey(id, len(cols))
	if err != nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(cols))
	for i, c := range cols {
		out[ch.convertKey(c)] = parts[i]
	}
	return out
}

// afterRow runs the typ hooks on a written row with the row's record id
// on ctx (AuditRecordIDFor). It always sets the value, so a write a hook
// starts never answers with the id of the write that started it.
func (ch *CrudHandler) afterRow(ctx context.Context, typ hook.HookType, row map[string]any) error {
	if ch.Entity != nil {
		id, _ := ch.rowID(row)
		ctx = context.WithValue(ctx, auditRecordKey{}, auditRecord{entity: ch.Entity.GetName(), id: id})
	}
	return runHooks(ch.Hooks, ctx, typ, row)
}
