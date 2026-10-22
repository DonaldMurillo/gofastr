package crud

import (
	"context"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// keyCols returns the handler's primary-key columns in declared order. A
// handler built by hand with no PrimaryKey reads its entity's key.
func (ch *CrudHandler) keyCols() []string {
	if ch.PrimaryKey == "" {
		return ch.Entity.KeyColumns()
	}
	return []string{ch.PrimaryKey}
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
	if len(vals) != len(cols) {
		return "1 = 0", nil
	}
	conds := make([]string, len(cols))
	for i, c := range cols {
		conds[i] = c + " = $" + strconv.Itoa(i+1)
	}
	return strings.Join(conds, " AND "), vals
}

// keyWhere is keyCond for a record id as JoinKey renders it. An id that
// does not split into the key columns names no record: the condition
// matches nothing, so every caller reaches its own not-found answer.
func (ch *CrudHandler) keyWhere(id string) (string, []any) {
	vals, ok := ch.idVals(id)
	if !ok {
		return "1 = 0", nil
	}
	return ch.keyCond(vals)
}

// idVals splits a record id into its key values, one per key column.
func (ch *CrudHandler) idVals(id string) ([]any, bool) {
	parts, err := entity.SplitKey(id, len(ch.keyCols()))
	if err != nil {
		return nil, false
	}
	vals := make([]any, len(parts))
	for i, p := range parts {
		vals[i] = p
	}
	return vals, true
}

// rowKeyVals reads the key values out of a row, under either the wire
// key or the column name. It reports false when any is missing or nil.
func (ch *CrudHandler) rowKeyVals(row map[string]any) ([]any, bool) {
	cols := ch.keyCols()
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
