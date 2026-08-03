package crud

import (
	"database/sql"
	"encoding/json"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A schema.JSON field arrives from the wire as a decoded Go value — a
// map, a slice, a number — and database/sql binds only driver types, so
// handing it a map fails the statement outright ("unsupported type
// map[string]interface {}, a map"). Every write path therefore runs its
// values through bindJSONValue, and every read path parses the column
// back through decodeJSONFields, so a round trip returns what was sent
// rather than an opaque string the client has to parse a second time.
//
// The two directions are deliberately not symmetric about strings. On
// write, a string IS the JSON text and is stored verbatim: that is what
// the admin battery's textarea submits and what the image-variants
// writer produces, and re-encoding it would double-quote the document.
// On read, text that does not parse as JSON is returned unchanged, so a
// legacy TEXT column promoted to schema.JSON keeps serving its rows.
//
// Absent and null stay indistinguishable — both leave the column NULL
// and read back as JSON null. An empty object is distinct: it is stored
// as "{}" and reads back as {}.

// bindJSONValue prepares one column value for the driver. Values for
// non-JSON columns pass through untouched.
func (ch *CrudHandler) bindJSONValue(col string, v any) any {
	if v == nil || !ch.isJSONColumn(col) {
		return v
	}
	return marshalJSONColumn(v)
}

// marshalJSONColumn encodes a decoded JSON value as the text a driver can
// bind. Strings and byte slices are already JSON text and pass through —
// schema.ValidateAll has already rejected a string that is not valid JSON
// with a 400, so nothing invalid reaches a Postgres JSONB column here.
// A value that cannot be marshalled is returned as-is so the driver's own
// error surfaces rather than a silently substituted one; validation
// rejects those before this point too.
func marshalJSONColumn(v any) any {
	switch v.(type) {
	case nil, string, []byte, json.RawMessage:
		return v
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	return string(raw)
}

// ensureFieldCache rebuilds the derived field caches when they are absent
// or stale. CrudHandler is an exported struct, so a caller can construct
// one directly and never run NewCrudHandler's build — every lookup goes
// through here rather than reading the maps straight.
func (ch *CrudHandler) ensureFieldCache() {
	if ch.jsonColumns == nil || ch.jsonWireKeys == nil || ch.visibleFieldSig != ch.fieldCacheSignature() {
		ch.refreshFieldCache()
	}
}

// isJSONColumn reports whether the named DB column is declared schema.JSON.
func (ch *CrudHandler) isJSONColumn(col string) bool {
	ch.ensureFieldCache()
	_, ok := ch.jsonColumns[col]
	return ok
}

// decodeJSONFields parses every schema.JSON column of a scanned row back
// into a JSON value, in place. Keys are wire keys — the shape a row has
// after scanRow/scanRows applied convertKey.
func (ch *CrudHandler) decodeJSONFields(row map[string]any) {
	if row == nil {
		return
	}
	ch.ensureFieldCache()
	for key := range ch.jsonWireKeys {
		if v, ok := row[key]; ok {
			row[key] = decodeJSONColumn(v)
		}
	}
}

// decodeJSONRows applies decodeJSONFields to a slice of scanned rows.
func (ch *CrudHandler) decodeJSONRows(rows []map[string]any) {
	if len(rows) == 0 {
		return
	}
	ch.ensureFieldCache()
	if len(ch.jsonWireKeys) == 0 {
		return
	}
	for _, row := range rows {
		ch.decodeJSONFields(row)
	}
}

// scanOne and scanMany are the handler-bound scanners every read path
// uses. They wrap the package-level scanners so schema.JSON columns are
// parsed in exactly one place — a read that scanned directly would return
// the column as an opaque string and break the round trip.
//
// They pass the handler's entity down for the same reason: a driver that
// reports a boolean column as int64 (modernc.org/sqlite does) would
// otherwise serialize it as 0/1. Both normalizations belong on this seam,
// so no read path can pick up one and miss the other.
func (ch *CrudHandler) scanOne(row *sql.Row, cols []string) (map[string]any, error) {
	result, err := scanRowWithBoolColumns(row, cols, ch.convertKey, ch.boolColumns(cols))
	if err != nil {
		return nil, err
	}
	ch.decodeJSONFields(result)
	return result, nil
}

func (ch *CrudHandler) scanMany(rows *sql.Rows, cols []string) ([]map[string]any, error) {
	var ent *entity.Entity
	if ch != nil {
		ent = ch.Entity
	}
	results, err := scanRowsForEntity(rows, cols, ch.convertKey, ent)
	if err != nil {
		return nil, err
	}
	ch.decodeJSONRows(results)
	return results, nil
}

// decodeEntityJSONColumns parses the schema.JSON columns of rows keyed by
// RAW DB column name. Eager-loaded relation rows carry that shape (they
// are case-converted only at the very end of the include walk), and they
// belong to a different entity than the handler's, so they cannot go
// through decodeJSONFields.
func decodeEntityJSONColumns(ent *entity.Entity, rows []map[string]any) {
	if ent == nil || len(rows) == 0 {
		return
	}
	var cols []string
	for _, f := range ent.GetFields() {
		if f.Type == schema.JSON {
			cols = append(cols, f.Name)
		}
	}
	if len(cols) == 0 {
		return
	}
	for _, row := range rows {
		for _, c := range cols {
			if v, ok := row[c]; ok {
				row[c] = decodeJSONColumn(v)
			}
		}
	}
}

// decodeJSONColumn parses one stored column value. Anything that is not
// JSON text — a NULL, a number the driver already typed, a legacy string
// that never was JSON — comes back untouched.
func decodeJSONColumn(v any) any {
	var raw []byte
	switch t := v.(type) {
	case string:
		if t == "" {
			return v
		}
		raw = []byte(t)
	case []byte:
		if len(t) == 0 {
			return v
		}
		raw = t
	default:
		return v
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return v
	}
	return out
}
