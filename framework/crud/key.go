package crud

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"weak"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func (ch *CrudHandler) keyCols() []string {
	if len(ch.PrimaryKey) == 0 {
		return ch.Entity.KeyColumns()
	}
	return ch.PrimaryKey
}

// refuseBlankKeyParts refuses "" in a key column: no route could name the row.
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

// refuseNullKey refuses an inserted row whose key reads back NULL, inside the
// write's transaction. SQLite stores NULL in most key columns.
func (ch *CrudHandler) refuseNullKey(row map[string]any) error {
	var fields map[string][]schema.Violation
	for _, c := range ch.keyCols() {
		v, ok := row[c]
		if !ok {
			v, ok = row[ch.convertKey(c)]
		}
		if !ok || v != nil {
			continue
		}
		if fields == nil {
			fields = map[string][]schema.Violation{}
		}
		fields[c] = []schema.Violation{{Reason: schema.ReasonRequired}}
	}
	if fields != nil {
		return &ValidationError{fields: fields}
	}
	return nil
}

// withKeyTiebreak appends every key column not already sorted on, so an
// offset page is over a total order; with no sort, a managed created_at leads.
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

// sortTerms is withKeyTiebreak with PinNulls set where pinsNulls says, so
// every list path orders nulls as Postgres does.
func (ch *CrudHandler) sortTerms(ctx context.Context, sorts []filter.ParsedSort) []filter.ParsedSort {
	out := ch.withKeyTiebreak(sorts)
	for i := range out {
		out[i].PinNulls = ch.pinsNulls(ctx, out[i].Field)
	}
	return out
}

// pinsNulls reports whether an ORDER BY on column must spell the null
// position: the column can hold NULL and the database is SQLite.
func (ch *CrudHandler) pinsNulls(ctx context.Context, column string) bool {
	if ch.Entity == nil {
		return false
	}
	keys := ch.keyCols()
	for _, f := range ch.Entity.GetFields() {
		if f.Name == column && !entity.CanHoldNull(f, keys) {
			return false
		}
	}
	return !isPostgres(ctx, ch.dialectHandle(), ch.conn(ctx))
}

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

func (ch *CrudHandler) isKeyCol(col string) bool {
	for _, c := range ch.keyCols() {
		if c == col {
			return true
		}
	}
	return false
}

// keyCond is "a = $1 AND b = $2" over the key columns; values that do not
// match the columns match nothing.
func (ch *CrudHandler) keyCond(vals []any) (string, []any) {
	cols := ch.keyCols()
	if len(cols) == 0 || len(vals) != len(cols) {
		return "1 = 0", nil
	}
	conds := make([]string, len(cols))
	for i, c := range cols {
		conds[i] = query.QuoteKeyword(c) + " = $" + strconv.Itoa(i+1)
	}
	return strings.Join(conds, " AND "), vals
}

// keyWhere matches id's record by key equality; an id that does not split
// into the key columns matches nothing. On an Unmanaged entity equality may
// match another spelling, so statements read the key back (keyedCols) and
// keep only the row whose own id is id (ownRow).
func (ch *CrudHandler) keyWhere(id string) (string, []any) {
	vals, ok := ch.idVals(id)
	if !ok {
		return "1 = 0", nil
	}
	return ch.keyCond(vals)
}

func (ch *CrudHandler) keyHasSpellings() bool {
	return ch.Entity != nil && ch.Entity.Config.Unmanaged
}

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

// ownRow reports whether row is id's own, and drops the key columns keyedCols added.
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

func (ch *CrudHandler) queryKeyed(ctx context.Context, cols []string, sqlStr string, args []any) ([]map[string]any, error) {
	rows, err := ch.conn(ctx).QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ch.scanMany(rows, ch.keyedCols(cols))
}

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

// writeOwn fails with errNotFound, rolling the write back, when the UPDATE
// changed a row that is not id's own.
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

// execOwn refuses, with errNotFound, a write that reaches a row not id's own.
// Postgres reads the rows back with RETURNING; SQLite, which cannot RETURN
// from a virtual table, selects them first inside its serializable transaction.
func (ch *CrudHandler) execOwn(ctx context.Context, id string, matching func(cols ...string) (string, []any), build func(returning []string) (string, []any)) (int64, error) {
	if !ch.keyHasSpellings() {
		sqlStr, args := build(nil)
		res, err := ch.conn(ctx).ExecContext(ctx, sqlStr, args...)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}
	if isPostgres(ctx, ch.dialectHandle(), ch.DB) {
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

// knownDialect caches isPostgres per *sql.DB, under a weak pointer.
var knownDialect sync.Map // weak.Pointer[sql.DB] -> bool

// dialectHandle is DB, or the database a transaction copy was begun on.
func (ch *CrudHandler) dialectHandle() *sql.DB {
	if db, ok := ch.DB.(*sql.DB); ok {
		return db
	}
	return ch.root
}

func rememberDialect(db *sql.DB, pg bool) {
	key := weak.Make(db)
	if _, had := knownDialect.Swap(key, pg); !had {
		runtime.AddCleanup(db, func(k weak.Pointer[sql.DB]) { knownDialect.Delete(k) }, key)
	}
}

// isPostgres asks q for SELECT version(). It caches the answer for a
// *sql.DB handle only when certain: a banner, or "no such function".
func isPostgres(ctx context.Context, handle any, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) bool {
	db, stable := handle.(*sql.DB)
	stable = stable && db != nil
	if stable {
		if known, ok := knownDialect.Load(weak.Make(db)); ok {
			return known.(bool)
		}
	}
	var v string
	if err := q.QueryRowContext(ctx, "SELECT version()").Scan(&v); err != nil {
		if stable && strings.Contains(strings.ToLower(err.Error()), "no such function") {
			rememberDialect(db, false)
		}
		return false
	}
	pg := strings.Contains(strings.ToLower(v), "postgresql")
	if stable {
		rememberDialect(db, pg)
	}
	return pg
}

// idVals splits id into bound key values (entity.KeyValues).
func (ch *CrudHandler) idVals(id string) ([]any, bool) {
	var fields []schema.Field
	if ch.Entity != nil {
		fields = entity.ValueFields(ch.Registry, ch.Entity)
	}
	vals, err := entity.KeyValues(id, ch.keyCols(), fields)
	return vals, err == nil
}

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

func (ch *CrudHandler) rowID(row map[string]any) (string, bool) {
	return entity.RowKey(row, ch.keyCols(), ch.convertKey)
}

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

// afterRow runs the typ hooks with the row's own id on ctx (AuditRecordIDFor).
func (ch *CrudHandler) afterRow(ctx context.Context, typ hook.HookType, row map[string]any) error {
	if ch.Entity != nil {
		id, _ := ch.rowID(row)
		ctx = context.WithValue(ctx, auditRecordKey{}, auditRecord{entity: ch.Entity.GetName(), id: id})
	}
	return runHooks(ch.Hooks, ctx, typ, row)
}
