package crud

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A schema.JSON field arrives from the wire as a decoded Go value, a
// map, a slice, a number, and database/sql binds only driver types, so
// handing it a map fails the statement outright ("unsupported type
// map[string]interface {}, a map"). Every write path therefore runs its
// values through bindJSONValue, and every read path parses the column
// back through decodeReadFields, so a round trip returns what was sent
// rather than an opaque string the client has to parse a second time.
//
// The two directions are deliberately not symmetric about strings. On
// write, a string IS the JSON text and is stored verbatim: that is what
// the admin battery's textarea submits and what the image-variants
// writer produces, and re-encoding it would double-quote the document.
// On read, text that does not parse as JSON is returned unchanged, so a
// legacy TEXT column promoted to schema.JSON keeps serving its rows, and
// a document that is one JSON string is returned as its JSON text, so the
// value read is the value the next write stores.
//
// Absent and null stay indistinguishable, both leave the column NULL
// and read back as JSON null. An empty object is distinct: it is stored
// as "{}" and reads back as {}.

// bindJSONValue prepares one column value for the driver. Values for
// non-JSON columns pass through untouched.
//
// A Timestamp column's value is stored in schema.TimestampLayout, the
// layout of the auto-managed timestamps, of a cursor token and of a bound
// Timestamp filter. SQLite compares the column as text, so a client's
// "...00Z" stored as sent sorted after the token "...00.000000Z" for the
// same instant and a keyset walk on the column repeated its first page.
//
// A Bool column binds every spelling validation accepts as the boolean. A
// body's value is already one (coerceNumberColumnValues); a declared
// Default is not, and SQLite kept a "true" default as text, which a
// boolean filter (bound as 1) never matched.
func (ch *CrudHandler) bindJSONValue(col string, v any) any {
	if v == nil {
		return v
	}
	snap := ch.snapshot()
	if _, ok := snap.timestampColumns[col]; ok {
		return storedTime(v)
	}
	if _, ok := snap.boolColumns[col]; ok {
		if b, ok := boolSpelling(v); ok {
			return b
		}
		return v
	}
	if !ch.isJSONColumn(col) {
		return v
	}
	return marshalJSONColumn(v)
}

// storedTime is v in schema.TimestampLayout when it is an instant (a
// time.Time, or text schema validation accepted as RFC 3339), v itself
// otherwise.
func storedTime(v any) any {
	switch t := v.(type) {
	case time.Time:
		return schema.FormatTimestamp(t)
	case *time.Time:
		if t != nil {
			return schema.FormatTimestamp(*t)
		}
	case string:
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			return schema.FormatTimestamp(parsed)
		}
	}
	return v
}

// marshalJSONColumn encodes a decoded JSON value as the text a driver can
// bind. Strings and byte slices are already JSON text and pass through,
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

// ensureFieldCache loads the derived field caches. CrudHandler is an
// exported struct, so a caller can construct one directly and never run
// NewCrudHandler's build; snapshot() fills lazily (once) for those. The
// returned snapshot is immutable, so unlike the old in-place refresh this
// cannot race concurrent readers.
func (ch *CrudHandler) ensureFieldCache() *fieldCacheData {
	return ch.snapshot()
}

// isJSONColumn reports whether the named DB column is declared schema.JSON.
func (ch *CrudHandler) isJSONColumn(col string) bool {
	_, ok := ch.snapshot().jsonColumns[col]
	return ok
}

// decodeReadFields parses every schema.JSON column of a scanned row back
// into a JSON value, turns every schema.Decimal column into its text and
// every schema.Date column into YYYY-MM-DD, in place. Keys are wire keys,
// the shape a row has after scanRow/scanRows applied convertKey.
func (ch *CrudHandler) decodeReadFields(row map[string]any) {
	if row == nil {
		return
	}
	snap := ch.snapshot()
	for key := range snap.jsonWireKeys {
		if v, ok := row[key]; ok {
			row[key] = decodeJSONColumn(v)
		}
	}
	for key := range snap.decimalWireKeys {
		if v, ok := row[key]; ok {
			row[key] = decimalText(v)
		}
	}
	for key := range snap.dateWireKeys {
		if v, ok := row[key]; ok {
			row[key] = dateText(v)
		}
	}
}

// decodeReadRows applies decodeReadFields to a slice of scanned rows.
func (ch *CrudHandler) decodeReadRows(rows []map[string]any) {
	if len(rows) == 0 {
		return
	}
	if snap := ch.snapshot(); len(snap.jsonWireKeys) == 0 && len(snap.decimalWireKeys) == 0 && len(snap.dateWireKeys) == 0 {
		return
	}
	for _, row := range rows {
		ch.decodeReadFields(row)
	}
}

// scanOne and scanMany are the handler-bound scanners every read path
// uses. They wrap the package-level scanners so schema.JSON columns are
// parsed in exactly one place, a read that scanned directly would return
// the column as an opaque string and break the round trip.
//
// They pass the handler's entity down for the same reason: a driver that
// reports a boolean column as int64 (modernc.org/sqlite does) would
// otherwise serialize it as 0/1, and one that scans a DECIMAL column as a
// number would break its string wire contract. Every normalization
// belongs on this seam, so no read path can pick up one and miss another.
func (ch *CrudHandler) scanOne(row *sql.Row, cols []string) (map[string]any, error) {
	result, err := scanRowWithBoolColumns(row, cols, ch.convertKey, ch.boolColumns(cols))
	if err != nil {
		return nil, err
	}
	ch.decodeReadFields(result)
	return result, nil
}

func (ch *CrudHandler) scanMany(rows *sql.Rows, cols []string) ([]map[string]any, error) {
	var fields []schema.Field
	if ch != nil && ch.Entity != nil {
		fields = ch.snapshotFields()
	}
	results, err := scanRowsForEntity(rows, cols, ch.convertKey, fields)
	if err != nil {
		return nil, err
	}
	ch.decodeReadRows(results)
	return results, nil
}

// decodeEntityReadColumns does what decodeReadFields does for rows keyed
// by RAW DB column name. Eager-loaded relation rows carry that shape (they
// are case-converted only at the very end of the include walk), and they
// belong to a different entity than the handler's, so they cannot go
// through decodeReadFields.
func decodeEntityReadColumns(ent *entity.Entity, rows []map[string]any) {
	if ent == nil || len(rows) == 0 {
		return
	}
	var jsonCols, decimalCols, dateCols []string
	for _, f := range ent.GetFields() {
		switch f.Type {
		case schema.JSON:
			jsonCols = append(jsonCols, f.Name)
		case schema.Decimal:
			decimalCols = append(decimalCols, f.Name)
		case schema.Date:
			dateCols = append(dateCols, f.Name)
		default:
			// Every other type reads back as the driver scanned it.
		}
	}
	for _, row := range rows {
		for _, c := range jsonCols {
			if v, ok := row[c]; ok {
				row[c] = decodeJSONColumn(v)
			}
		}
		for _, c := range decimalCols {
			if v, ok := row[c]; ok {
				row[c] = decimalText(v)
			}
		}
		for _, c := range dateCols {
			if v, ok := row[c]; ok {
				row[c] = dateText(v)
			}
		}
	}
}

// dateText is a stored date as the YYYY-MM-DD the wire carries. Both
// drivers scan DATE into a time.Time, which marshals as a full timestamp
// ("2026-10-27T00:00:00Z") that Date validation and the Date filter
// refuse, so a client could not send back what it read. The date is the
// one the driver's value names, in its own location: a DATE has no zone.
func dateText(v any) any {
	switch t := v.(type) {
	case time.Time:
		return t.Format(time.DateOnly)
	case *time.Time:
		if t != nil {
			return t.Format(time.DateOnly)
		}
	}
	return v
}

// decimalText is a stored decimal as the string the wire carries. SQLite
// gives a DECIMAL column numeric affinity, so its driver scans the value
// back as an int64 or a float64; Postgres hands back the text already.
// The float is the stored value: SQLite kept no more digits than it has.
func decimalText(v any) any {
	switch t := v.(type) {
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []byte:
		return string(t)
	}
	return v
}

// decodeJSONColumn parses one stored column value. Anything that is not
// JSON text, a NULL, a number the driver already typed, a legacy string
// that never was JSON, comes back untouched.
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
	// UseNumber: a float64 decode read a stored 9007199254740993 back as
	// 9007199254740992.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return v
	}
	if _, err := dec.Token(); err != io.EOF {
		return v
	}
	// A document that is one JSON string reads back as its JSON text. The
	// write path stores a Go string verbatim as the document, so reading
	// "123" back as the string 123 made the next write store the number.
	if _, isString := out.(string); isString {
		return string(raw)
	}
	return storedJSONNumbers(out)
}

// storedJSONNumbers gives a decoded document the number types a float64
// decode gave it, float64, except for an integer float64 cannot hold
// exactly: past ±2^53 it reads as int64, and past int64 as the
// json.Number that keeps its digits.
func storedJSONNumbers(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, item := range x {
			x[k] = storedJSONNumbers(item)
		}
	case []any:
		for i, item := range x {
			x[i] = storedJSONNumbers(item)
		}
	case json.Number:
		if i, err := x.Int64(); err == nil {
			if i > 1<<53 || i < -(1<<53) {
				return i
			}
			return float64(i)
		}
		if strings.ContainsAny(x.String(), ".eE") {
			if f, err := x.Float64(); err == nil && !roundsInteger(f) {
				return f
			}
		}
		return x
	}
	return v
}
